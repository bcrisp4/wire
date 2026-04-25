package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bcrisp4/wire/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runTestServer starts the server in a goroutine and returns its address.
// The server stops when ctx is canceled.
func runTestServer(t *testing.T, opts Options) string {
	t.Helper()
	if opts.Listen == "" {
		opts.Listen = "127.0.0.1:0"
	}
	if opts.Logger == nil {
		opts.Logger = slogDiscard()
	}
	srv, err := NewServer(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Run(ctx) }()
	select {
	case <-srv.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("server failed to bind within 2s")
	}
	return srv.Addr()
}

func TestServer_HealthThroughRouter(t *testing.T) {
	addr := runTestServer(t, Options{})
	resp, err := http.Get("http://" + addr + "/api/v1/health")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)
	b, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(b), `"ok"`)
}

func TestServer_ShutdownIsGraceful(t *testing.T) {
	srv, err := NewServer(Options{Listen: "127.0.0.1:0", Logger: slogDiscard()})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Run(ctx) }()
	<-srv.Ready()
	cancel()
	shutCtx, sc := context.WithTimeout(context.Background(), time.Second)
	defer sc()
	assert.NoError(t, srv.Shutdown(shutCtx))
}

func TestServer_RejectsMissingLogger(t *testing.T) {
	_, err := NewServer(Options{Listen: "127.0.0.1:0"})
	assert.Error(t, err)
}

func TestServer_AcceptsStore(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "wire.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, store.Migrate(context.Background(), db))

	addr := runTestServer(t, Options{Store: store.New(db)})
	resp, err := http.Get("http://" + addr + "/api/v1/health")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)
}

func TestMiddleware_RecoversPanic(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/boom", func(http.ResponseWriter, *http.Request) { panic("nope") })
	h := panicRecover(slogDiscard())(mux)

	r := httptest.NewRequest("GET", "/boom", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	assert.Equal(t, 500, w.Code)
	assert.Contains(t, strings.ToLower(w.Body.String()), "internal error")
}

func slogDiscard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
