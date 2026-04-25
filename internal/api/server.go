package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/bcrisp4/wire/internal/jobs"
	"github.com/bcrisp4/wire/internal/store"
)

// Options configures a Server. Logger is required; Store and Queue are required
// in production wiring (cmd/wire/serve.go) but accepted as nil in tests so
// route-only checks (health, SPA, middleware) need not stand up a database.
// Phase 1 REST units add their handlers behind these dependencies.
type Options struct {
	Listen string
	Logger *slog.Logger
	SPA    http.Handler // optional: served on non-/api routes
	Store  store.Store  // optional in tests; required by REST handlers
	Queue  jobs.Queue   // optional in tests; required by handlers that enqueue work
}

type Server struct {
	opts  Options
	http  *http.Server
	mu    sync.Mutex
	ln    net.Listener
	ready chan struct{}
}

func NewServer(opts Options) (*Server, error) {
	if opts.Logger == nil {
		return nil, errors.New("api: Options.Logger is required")
	}
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/health", healthHandler())

	// Unit 7: categories
	if opts.Store != nil {
		registerCategoryRoutes(mux, opts.Store.Categories(), opts.Logger)
	}
	// End Unit 7
	// Unit 8: entries
	if opts.Store != nil {
		if entries, ok := opts.Store.Entries().(store.EntriesAPI); ok {
			registerEntryRoutes(mux, entries)
		}
	}
	// end Unit 8: entries

	if opts.SPA != nil {
		mux.Handle("/", opts.SPA)
	}

	return &Server{
		opts: opts,
		http: &http.Server{
			Handler:           requestLogger(opts.Logger)(panicRecover(opts.Logger)(mux)),
			ReadHeaderTimeout: 10 * time.Second,
		},
		ready: make(chan struct{}),
	}, nil
}

// Handler returns the fully-chained HTTP handler. Useful for httptest.NewServer
// in unit tests so they exercise the same middleware stack as production.
func (s *Server) Handler() http.Handler { return s.http.Handler }

// Run blocks until ctx is canceled or the server fails.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.opts.Listen)
	if err != nil {
		return fmt.Errorf("api: listen: %w", err)
	}
	s.mu.Lock()
	s.ln = ln
	close(s.ready)
	s.mu.Unlock()
	s.opts.Logger.Info("listening", "addr", ln.Addr().String())

	errCh := make(chan error, 1)
	go func() {
		err := s.http.Serve(ln)
		if !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return s.http.Shutdown(shutCtx)
	case err := <-errCh:
		return err
	}
}

// Shutdown gracefully terminates the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

// Ready returns a channel that closes once the listener is bound.
func (s *Server) Ready() <-chan struct{} { return s.ready }

// Addr returns the bound listener's address. Empty until Ready is closed.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}
