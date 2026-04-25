package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runTestServer starts the server in a goroutine and returns its address. The server stops when ctx is canceled.
func runTestServer(t *testing.T, opts Options) string {
	t.Helper()
	if opts.Listen == "" {
		opts.Listen = "127.0.0.1:0"
	}
	srv, err := NewServer(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	addrCh := make(chan string, 1)
	go func() {
		// Poll briefly for the listener to bind so we can return the actual address.
		go func() {
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				if a := srv.Addr(); a != "" {
					addrCh <- a
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
			addrCh <- ""
		}()
		_ = srv.Run(ctx)
	}()
	addr := <-addrCh
	require.NotEmpty(t, addr, "server failed to bind")
	return addr
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
	srv, err := NewServer(Options{Listen: "127.0.0.1:0"})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Run(ctx) }()
	// Wait for bind.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && srv.Addr() == "" {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	// Shutdown via second call is also fine.
	shutCtx, sc := context.WithTimeout(context.Background(), time.Second)
	defer sc()
	assert.NoError(t, srv.Shutdown(shutCtx))
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
