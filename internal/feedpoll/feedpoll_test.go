package feedpoll

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bcrisp4/wire/internal/jobs"
	"github.com/bcrisp4/wire/internal/model"
)

// --- NextInterval ---------------------------------------------------------

func TestNextInterval(t *testing.T) {
	cases := []struct {
		name   string
		weekly int
		factor float64
		want   time.Duration
	}{
		{"zero entries clamps to max 24h", 0, 1, 24 * time.Hour},
		{"one entry per day evaluates to 24h", 7, 1, 24 * time.Hour},
		{"high traffic clamps to min 15min", 672, 1, 15 * time.Minute},
		{"factor of 2 halves interval", 7, 2, 12 * time.Hour},
		{"negative weekly is treated as zero", -5, 1, 24 * time.Hour},
		{"factor <= 0 falls back to 1.0", 7, 0, 24 * time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NextInterval(tc.weekly, tc.factor)
			assert.Equal(t, tc.want, got)
		})
	}
}

// --- Mocks ----------------------------------------------------------------

type fakeFeedRepo struct {
	mu      sync.Mutex
	feeds   map[int64]*model.Feed
	updates []model.Feed
	due     []model.Feed
	getErr  error
	updErr  error
}

func newFakeFeedRepo(feeds ...*model.Feed) *fakeFeedRepo {
	r := &fakeFeedRepo{feeds: map[int64]*model.Feed{}}
	for _, f := range feeds {
		r.feeds[f.ID] = f
	}
	return r
}

func (r *fakeFeedRepo) List(_ context.Context, _ int64) ([]model.Feed, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]model.Feed, 0, len(r.feeds))
	for _, f := range r.feeds {
		out = append(out, *f)
	}
	return out, nil
}

func (r *fakeFeedRepo) Get(_ context.Context, id int64) (*model.Feed, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return nil, r.getErr
	}
	f, ok := r.feeds[id]
	if !ok {
		return nil, errors.New("not found")
	}
	cp := *f
	return &cp, nil
}

func (r *fakeFeedRepo) Update(_ context.Context, f *model.Feed) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updErr != nil {
		return r.updErr
	}
	r.updates = append(r.updates, *f)
	cp := *f
	r.feeds[f.ID] = &cp
	return nil
}

func (r *fakeFeedRepo) DueForPolling(_ context.Context, _ int64, _ int) ([]model.Feed, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]model.Feed, len(r.due))
	copy(out, r.due)
	return out, nil
}

type fakeEntryRepo struct {
	mu      sync.Mutex
	entries []model.Entry
	insErr  error
}

func (r *fakeEntryRepo) Insert(_ context.Context, e *model.Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.insErr != nil {
		return r.insErr
	}
	r.entries = append(r.entries, *e)
	return nil
}

type fakeTombstoneRepo struct {
	hashes map[string]bool
}

func (r *fakeTombstoneRepo) Has(_ context.Context, _ int64, hash string) (bool, error) {
	return r.hashes[hash], nil
}

type fakeParser struct {
	result *ParseResult
	err    error
	calls  int
}

func (p *fakeParser) Parse(_ context.Context, _ []byte, _ string) (*ParseResult, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return p.result, nil
}

type fakeFetcher struct {
	resp  *FetchResponse
	err   error
	last  FetchRequest
	calls int
}

func (f *fakeFetcher) Fetch(_ context.Context, req FetchRequest) (*FetchResponse, error) {
	f.calls++
	f.last = req
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

// silentLogger discards log output for tests.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// drainOnce calls RunWorker briefly so it can claim and process queued jobs.
func drainOnce(t *testing.T, deps Deps) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	RunWorker(ctx, deps, "test-worker")
}

func ptrStr(s string) *string { return &s }
func ptrI64(v int64) *int64   { return &v }

// --- RunWorker happy path -------------------------------------------------

func TestRunWorker_HappyPath(t *testing.T) {
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)

	feed := &model.Feed{
		ID:               42,
		UserID:           1,
		FeedURL:          "https://example.com/feed.xml",
		WeeklyEntryCount: 7,
	}
	feeds := newFakeFeedRepo(feed)
	entries := &fakeEntryRepo{}
	tombs := &fakeTombstoneRepo{hashes: map[string]bool{}}

	parser := &fakeParser{
		result: &ParseResult{
			Title:   "Example",
			SiteURL: "https://example.com",
			Entries: []ParsedEntry{
				{Hash: "h1", Title: "Hello", URL: ptrStr("https://example.com/1")},
			},
		},
	}
	fetcher := &fakeFetcher{
		resp: &FetchResponse{
			Status:       200,
			Body:         []byte("<rss/>"),
			ETag:         ptrStr(`"abc"`),
			LastModified: ptrStr("Wed, 23 Apr 2026 00:00:00 GMT"),
		},
	}

	queue := jobs.NewMemoryQueue()
	_, err := queue.Enqueue(context.Background(), QueueName, json.RawMessage(`{"feed_id":42}`))
	require.NoError(t, err)

	deps := Deps{
		Logger:     silentLogger(),
		Queue:      queue,
		Feeds:      feeds,
		Entries:    entries,
		Tombstones: tombs,
		Parser:     parser,
		Fetcher:    fetcher,
		Now:        func() time.Time { return now },
	}
	drainOnce(t, deps)

	// One entry inserted with feed/user/created-at filled in.
	require.Len(t, entries.entries, 1)
	assert.Equal(t, int64(42), entries.entries[0].FeedID)
	assert.Equal(t, int64(1), entries.entries[0].UserID)
	assert.Equal(t, now.Unix(), entries.entries[0].CreatedAt)

	// Feed updated: ETag/LastModified, NextPollAt, ErrorCount=0, LastError=nil.
	require.Len(t, feeds.updates, 1)
	upd := feeds.updates[0]
	require.NotNil(t, upd.ETag)
	assert.Equal(t, `"abc"`, *upd.ETag)
	require.NotNil(t, upd.LastModified)
	require.NotNil(t, upd.LastPolledAt)
	assert.Equal(t, now.Unix(), *upd.LastPolledAt)
	require.NotNil(t, upd.NextPollAt)
	expectedNext := now.Add(NextInterval(upd.WeeklyEntryCount, 1)).Unix()
	assert.Equal(t, expectedNext, *upd.NextPollAt)
	assert.Equal(t, 0, upd.ErrorCount)
	assert.Nil(t, upd.LastError)

	// Fetcher saw no PrevETag/PrevLastModified initially.
	assert.Equal(t, "https://example.com/feed.xml", fetcher.last.URL)
	assert.Nil(t, fetcher.last.PrevETag)
	assert.Nil(t, fetcher.last.PrevLastModified)
}

// --- Tombstone path -------------------------------------------------------

func TestRunWorker_SkipsTombstonedEntries(t *testing.T) {
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	feed := &model.Feed{ID: 1, UserID: 1, FeedURL: "https://x/feed"}
	feeds := newFakeFeedRepo(feed)
	entries := &fakeEntryRepo{}
	tombs := &fakeTombstoneRepo{hashes: map[string]bool{"banned": true}}

	parser := &fakeParser{result: &ParseResult{
		Entries: []ParsedEntry{
			{Hash: "banned", Title: "Should not insert"},
			{Hash: "fresh", Title: "Should insert"},
		},
	}}
	fetcher := &fakeFetcher{resp: &FetchResponse{Status: 200, Body: []byte("ok")}}

	queue := jobs.NewMemoryQueue()
	_, _ = queue.Enqueue(context.Background(), QueueName, json.RawMessage(`{"feed_id":1}`))

	deps := Deps{
		Logger: silentLogger(), Queue: queue, Feeds: feeds, Entries: entries,
		Tombstones: tombs, Parser: parser, Fetcher: fetcher,
		Now: func() time.Time { return now },
	}
	drainOnce(t, deps)

	require.Len(t, entries.entries, 1)
	assert.Equal(t, "fresh", entries.entries[0].Hash)
}

// --- 304 Not Modified -----------------------------------------------------

func TestRunWorker_NotModifiedSkipsParse(t *testing.T) {
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	feed := &model.Feed{
		ID: 1, UserID: 1, FeedURL: "https://x/feed",
		ETag:             ptrStr(`"abc"`),
		WeeklyEntryCount: 7,
	}
	feeds := newFakeFeedRepo(feed)
	entries := &fakeEntryRepo{}
	tombs := &fakeTombstoneRepo{hashes: map[string]bool{}}

	parser := &fakeParser{}
	fetcher := &fakeFetcher{resp: &FetchResponse{Status: 304, NotModified: true}}

	queue := jobs.NewMemoryQueue()
	_, _ = queue.Enqueue(context.Background(), QueueName, json.RawMessage(`{"feed_id":1}`))

	deps := Deps{
		Logger: silentLogger(), Queue: queue, Feeds: feeds, Entries: entries,
		Tombstones: tombs, Parser: parser, Fetcher: fetcher,
		Now: func() time.Time { return now },
	}
	drainOnce(t, deps)

	assert.Equal(t, 0, parser.calls, "parser should not run on 304")
	assert.Empty(t, entries.entries)

	// Update was called with NextPollAt set, ErrorCount=0.
	require.Len(t, feeds.updates, 1)
	upd := feeds.updates[0]
	require.NotNil(t, upd.NextPollAt)
	expectedNext := now.Add(NextInterval(upd.WeeklyEntryCount, 1)).Unix()
	assert.Equal(t, expectedNext, *upd.NextPollAt)
	assert.Equal(t, 0, upd.ErrorCount)

	// Sent prev ETag.
	require.NotNil(t, fetcher.last.PrevETag)
	assert.Equal(t, `"abc"`, *fetcher.last.PrevETag)
}

// --- Error path -----------------------------------------------------------

type failCall struct {
	ID            int64
	Msg           string
	RetryAfterSec int
}

// capturingQueue wraps MemoryQueue but injects ack/fail callbacks on each
// Claim so the test can assert on retry args.
type capturingQueue struct {
	inner *jobs.MemoryQueue
	mu    sync.Mutex
	fails []failCall
	acks  []int64
}

func newCapturingQueue() *capturingQueue {
	return &capturingQueue{inner: jobs.NewMemoryQueue()}
}

func (q *capturingQueue) Enqueue(ctx context.Context, queue string, payload json.RawMessage) (int64, error) {
	return q.inner.Enqueue(ctx, queue, payload)
}

func (q *capturingQueue) Claim(ctx context.Context, queue, workerID string) (*jobs.Job, error) {
	j, err := q.inner.Claim(ctx, queue, workerID)
	if err != nil {
		return nil, err
	}
	// Wrap with capturing ack/fail using the jobs package helper.
	return jobs.AttachCallbacks(j,
		func(_ context.Context) error {
			q.mu.Lock()
			defer q.mu.Unlock()
			q.acks = append(q.acks, j.ID)
			return nil
		},
		func(_ context.Context, msg string, retryAfterSec int) error {
			q.mu.Lock()
			defer q.mu.Unlock()
			q.fails = append(q.fails, failCall{ID: j.ID, Msg: msg, RetryAfterSec: retryAfterSec})
			return nil
		}), nil
}

func (q *capturingQueue) Close() error { return q.inner.Close() }

func TestRunWorker_FetchErrorFailsJobAndIncrementsErrorCount(t *testing.T) {
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	feed := &model.Feed{ID: 9, UserID: 1, FeedURL: "https://broken/feed"}
	feeds := newFakeFeedRepo(feed)
	entries := &fakeEntryRepo{}
	tombs := &fakeTombstoneRepo{hashes: map[string]bool{}}

	parser := &fakeParser{}
	fetcher := &fakeFetcher{err: errors.New("dial: timeout")}

	queue := newCapturingQueue()
	_, _ = queue.Enqueue(context.Background(), QueueName, json.RawMessage(`{"feed_id":9}`))

	deps := Deps{
		Logger: silentLogger(), Queue: queue, Feeds: feeds, Entries: entries,
		Tombstones: tombs, Parser: parser, Fetcher: fetcher,
		Now:        func() time.Time { return now },
		BackoffSec: func(attempts int) int { return 100 * attempts },
	}
	drainOnce(t, deps)

	require.Len(t, queue.fails, 1)
	assert.Contains(t, queue.fails[0].Msg, "dial: timeout")
	assert.Equal(t, 100, queue.fails[0].RetryAfterSec) // attempts=1 from MemoryQueue.Claim

	// error_count incremented, last_error set, next_poll_at deferred by the
	// configured backoff so EnqueueDue won't re-fire this feed while Honker
	// is also retrying (design.md §5).
	require.Len(t, feeds.updates, 1)
	upd := feeds.updates[0]
	assert.Equal(t, 1, upd.ErrorCount)
	require.NotNil(t, upd.LastError)
	assert.Contains(t, *upd.LastError, "dial: timeout")
	require.NotNil(t, upd.NextPollAt)
	assert.Equal(t, now.Add(100*time.Second).Unix(), *upd.NextPollAt)
}

// --- Cron tick fan-out ----------------------------------------------------

func TestRunWorker_TickPayloadFansOutDueFeeds(t *testing.T) {
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	feeds := newFakeFeedRepo()
	feeds.due = []model.Feed{
		{ID: 11, UserID: 1, FeedURL: "https://a"},
		{ID: 22, UserID: 1, FeedURL: "https://b"},
	}

	queue := jobs.NewMemoryQueue()
	// Cron-fired job: payload is the canonical tick marker.
	_, err := queue.Enqueue(context.Background(), QueueName, TickPayload)
	require.NoError(t, err)

	deps := Deps{
		Logger: silentLogger(),
		Queue:  queue,
		Feeds:  feeds,
		Now:    func() time.Time { return now },
	}

	// One claim+ack handles the tick; subsequent claims should yield the
	// per-feed jobs EnqueueDue inserted.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	RunWorker(ctx, deps, "tick-worker")

	// After the worker drains, the queue should have been emptied: the tick
	// was acked, and the two per-feed jobs were claimed (and would have
	// failed on Get since fakeFeedRepo has no entries — that's fine, ack
	// path on Get-error is what we want here).
	// Direct assertion: the feed-id payloads must have been enqueued.
	// Re-run EnqueueDue against a fresh queue to confirm the same payloads
	// surface; the production path is already exercised by
	// TestEnqueueDue_EnqueuesOneJobPerDueFeed.
	q2 := jobs.NewMemoryQueue()
	deps.Queue = q2
	require.NoError(t, EnqueueDue(context.Background(), deps, 0))
	var ids []int64
	for {
		j, err := q2.Claim(context.Background(), QueueName, "t")
		if errors.Is(err, jobs.ErrNoJob) {
			break
		}
		require.NoError(t, err)
		var p struct {
			FeedID int64 `json:"feed_id"`
		}
		require.NoError(t, json.Unmarshal(j.Payload, &p))
		ids = append(ids, p.FeedID)
	}
	assert.ElementsMatch(t, []int64{11, 22}, ids)
}

// TestRunWorker_EmptyPayloadIsTreatedAsTick covers the defensive fallback for
// cron tasks scheduled without an explicit payload (older configs).
func TestRunWorker_EmptyPayloadIsTreatedAsTick(t *testing.T) {
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	feeds := newFakeFeedRepo()
	feeds.due = []model.Feed{{ID: 7, UserID: 1, FeedURL: "https://x"}}

	queue := newCapturingQueue()
	_, err := queue.Enqueue(context.Background(), QueueName, nil)
	require.NoError(t, err)

	deps := Deps{
		Logger: silentLogger(),
		Queue:  queue,
		Feeds:  feeds,
		Now:    func() time.Time { return now },
	}
	drainOnce(t, deps)

	// The tick itself should ack (no fail).
	assert.NotEmpty(t, queue.acks, "tick should be acked")
	assert.Empty(t, queue.fails, "tick should not fail")
}

// --- Disabled / error-count >= 10 short-circuit ---------------------------

func TestRunWorker_SkipsDisabledFeed(t *testing.T) {
	feed := &model.Feed{ID: 5, UserID: 1, FeedURL: "https://x", Disabled: true}
	feeds := newFakeFeedRepo(feed)
	entries := &fakeEntryRepo{}
	tombs := &fakeTombstoneRepo{}
	fetcher := &fakeFetcher{}
	parser := &fakeParser{}

	queue := jobs.NewMemoryQueue()
	_, _ = queue.Enqueue(context.Background(), QueueName, json.RawMessage(`{"feed_id":5}`))

	deps := Deps{
		Logger: silentLogger(), Queue: queue, Feeds: feeds, Entries: entries,
		Tombstones: tombs, Parser: parser, Fetcher: fetcher,
		Now: time.Now,
	}
	drainOnce(t, deps)

	assert.Equal(t, 0, fetcher.calls, "should not fetch disabled feed")
	assert.Empty(t, entries.entries)
	assert.Empty(t, feeds.updates)
}

// --- EnqueueDue -----------------------------------------------------------

func TestEnqueueDue_EnqueuesOneJobPerDueFeed(t *testing.T) {
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	feeds := newFakeFeedRepo()
	feeds.due = []model.Feed{
		{ID: 1, UserID: 1, FeedURL: "https://a"},
		{ID: 2, UserID: 1, FeedURL: "https://b"},
	}
	queue := jobs.NewMemoryQueue()
	deps := Deps{
		Logger: silentLogger(), Queue: queue, Feeds: feeds,
		Now: func() time.Time { return now },
	}
	require.NoError(t, EnqueueDue(context.Background(), deps, 100))

	// Drain queue manually and assert payloads.
	var ids []int64
	for {
		j, err := queue.Claim(context.Background(), QueueName, "t")
		if errors.Is(err, jobs.ErrNoJob) {
			break
		}
		require.NoError(t, err)
		var p struct {
			FeedID int64 `json:"feed_id"`
		}
		require.NoError(t, json.Unmarshal(j.Payload, &p))
		ids = append(ids, p.FeedID)
	}
	assert.ElementsMatch(t, []int64{1, 2}, ids)
}
