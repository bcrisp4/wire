package extract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/bcrisp4/wire/internal/extract"
	"github.com/bcrisp4/wire/internal/jobs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const workerArticleHTML = `<!doctype html>
<html><body>
<header><nav>menu</nav></header>
<article>
<h1>Worker Test Article</h1>
<p>This article body has more than enough words for Readability to commit to
it as the document's main candidate. We are deliberately writing several
sentences of substantive prose, padding the paragraph until it crosses the
threshold the heuristic uses for adoption.</p>
<p>Second paragraph, more text, plenty of words to keep Readability happy.
The reading-time computation should yield at least one minute, since even
short articles round up to a single minute by design.</p>
</article>
<footer>fin</footer>
</body></html>`

// runWorkerForOneJob spins up a worker, blocks until it acks a single job,
// then cancels the context and returns. It mirrors how serve.go would run
// the worker except that it returns once the queue is drained.
func runWorkerForOneJob(t *testing.T, deps extract.Deps, jobID int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	doneCh := make(chan struct{})
	deps.OnJobDone = func(id int64) {
		if id == jobID {
			close(doneCh)
		}
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		extract.RunWorker(ctx, deps, "test-worker")
	}()

	select {
	case <-doneCh:
	case <-ctx.Done():
		t.Fatalf("worker did not process job in time: %v", ctx.Err())
	}
	cancel()
	wg.Wait()
}

func TestRunWorker_ExtractsAndUpdatesEntry(t *testing.T) {
	// HTTP server that serves the article HTML.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, workerArticleHTML)
	}))
	defer srv.Close()

	// In-memory state.
	store := newMemEntryStore()
	entryID := store.insert(srv.URL+"/post", "")

	q := jobs.NewMemoryQueue()
	payload, _ := json.Marshal(struct {
		EntryID int64 `json:"entry_id"`
	}{EntryID: entryID})
	jobID, err := q.Enqueue(context.Background(), jobs.QueueEntryExtract, payload)
	require.NoError(t, err)

	deps := extract.Deps{
		Queue:      q,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		HTTPClient: srv.Client(),
		EntryFetcher: func(ctx context.Context, id int64) (string, string, error) {
			url, rules, ok := store.get(id)
			if !ok {
				return "", "", fmt.Errorf("entry %d not found", id)
			}
			return url, rules, nil
		},
		EntryUpdater: func(ctx context.Context, id int64, content string, readingTime int) error {
			store.update(id, content, readingTime)
			return nil
		},
	}

	runWorkerForOneJob(t, deps, jobID)

	got := store.snapshot(entryID)
	assert.NotEmpty(t, got.content, "content should be set after extraction")
	assert.Contains(t, got.content, "Worker Test Article")
	assert.GreaterOrEqual(t, got.readingTime, 1)
}

func TestRunWorker_FetchErrorIsAcked(t *testing.T) {
	store := newMemEntryStore()
	entryID := store.insert("http://127.0.0.1:1/never", "")

	q := jobs.NewMemoryQueue()
	payload, _ := json.Marshal(struct {
		EntryID int64 `json:"entry_id"`
	}{EntryID: entryID})
	jobID, err := q.Enqueue(context.Background(), jobs.QueueEntryExtract, payload)
	require.NoError(t, err)

	deps := extract.Deps{
		Queue:  q,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		HTTPClient: &http.Client{Timeout: 100 * time.Millisecond},
		EntryFetcher: func(ctx context.Context, id int64) (string, string, error) {
			url, rules, _ := store.get(id)
			return url, rules, nil
		},
		EntryUpdater: func(ctx context.Context, id int64, content string, readingTime int) error {
			store.update(id, content, readingTime)
			return nil
		},
	}

	runWorkerForOneJob(t, deps, jobID)

	got := store.snapshot(entryID)
	assert.Empty(t, got.content, "content stays empty when fetch fails")
}

// --- helpers ---

type memEntryStore struct {
	mu      sync.Mutex
	nextID  int64
	entries map[int64]*memEntry
}

type memEntry struct {
	url         string
	rules       string
	content     string
	readingTime int
}

func newMemEntryStore() *memEntryStore {
	return &memEntryStore{entries: map[int64]*memEntry{}}
}

func (s *memEntryStore) insert(url, rules string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	id := s.nextID
	s.entries[id] = &memEntry{url: url, rules: rules}
	return id
}

func (s *memEntryStore) get(id int64) (url, rules string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return "", "", false
	}
	return e.url, e.rules, true
}

func (s *memEntryStore) update(id int64, content string, readingTime int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[id]; ok {
		e.content = content
		e.readingTime = readingTime
	}
}

func (s *memEntryStore) snapshot(id int64) memEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[id]; ok {
		return *e
	}
	return memEntry{}
}

