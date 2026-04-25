package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	honker "github.com/russellromney/honker-go"
)

// HonkerOptions configures the Honker-backed queue/scheduler.
type HonkerOptions struct {
	DBPath        string
	ExtensionPath string
	// SchedulerOwner identifies this process for cron leader election. Defaults to "wire".
	SchedulerOwner string
}

// HonkerBackend is the production Queue+Scheduler implementation backed by
// honker-go. It owns the SQLite connection (Honker's design); application code
// reads/writes through the *sql.DB returned by RawDB().
type HonkerBackend struct {
	db        *honker.Database
	queue     *honkerQueue
	scheduler *honkerScheduler
}

// NewHonker opens (or initialises) the Honker-managed SQLite file at opts.DBPath
// loading the extension at opts.ExtensionPath.
func NewHonker(opts HonkerOptions) (*HonkerBackend, error) {
	if opts.DBPath == "" {
		return nil, fmt.Errorf("honker: empty DB path")
	}
	if opts.ExtensionPath == "" {
		return nil, fmt.Errorf("honker: empty extension path")
	}
	owner := opts.SchedulerOwner
	if owner == "" {
		owner = "wire"
	}
	db, err := honker.Open(opts.DBPath, opts.ExtensionPath)
	if err != nil {
		return nil, fmt.Errorf("honker: open: %w", err)
	}
	return &HonkerBackend{
		db:        db,
		queue:     &honkerQueue{db: db},
		scheduler: &honkerScheduler{db: db, owner: owner},
	}, nil
}

// RawDB returns the underlying *sql.DB so application code can run app-side
// queries (migrations, store ops). Honker tables (_honker_*) coexist on the
// same connection.
func (h *HonkerBackend) RawDB() *sql.DB { return h.db.Raw() }

// Queue returns the Queue interface implementation.
func (h *HonkerBackend) Queue() Queue { return h.queue }

// Scheduler returns the Scheduler interface implementation.
func (h *HonkerBackend) Scheduler() Scheduler { return h.scheduler }

// Close shuts down the underlying Honker DB.
func (h *HonkerBackend) Close() error { return h.db.Close() }

// --- Queue impl ---

type honkerQueue struct{ db *honker.Database }

func (q *honkerQueue) Enqueue(_ context.Context, queue string, payload json.RawMessage) (int64, error) {
	hq := q.db.Queue(queue, honker.QueueOptions{})
	// Honker JSON-marshals payload internally. json.RawMessage marshals to its raw bytes,
	// preserving caller-prepared JSON verbatim.
	id, err := hq.Enqueue(payload, honker.EnqueueOptions{})
	if err != nil {
		return 0, fmt.Errorf("honker enqueue: %w", err)
	}
	return id, nil
}

func (q *honkerQueue) Claim(_ context.Context, queue, workerID string) (*Job, error) {
	hq := q.db.Queue(queue, honker.QueueOptions{})
	hj, err := hq.ClaimOne(workerID)
	if err != nil {
		return nil, fmt.Errorf("honker claim: %w", err)
	}
	if hj == nil {
		return nil, ErrNoJob
	}
	return wrapHonkerJob(hj), nil
}

func (q *honkerQueue) Close() error { return nil } // owned by HonkerBackend

func wrapHonkerJob(hj *honker.Job) *Job {
	return &Job{
		ID:       hj.ID,
		Queue:    hj.Queue,
		Payload:  json.RawMessage(hj.Payload),
		Attempts: hj.Attempts,
		ack: func(_ context.Context) error {
			ok, err := hj.Ack()
			if err != nil {
				return fmt.Errorf("honker ack: %w", err)
			}
			if !ok {
				return fmt.Errorf("honker ack: job %d not in claimed state", hj.ID)
			}
			return nil
		},
		fail: func(_ context.Context, errMsg string, retryAfterSec int) error {
			ok, err := hj.Retry(int64(retryAfterSec), errMsg)
			if err != nil {
				return fmt.Errorf("honker retry: %w", err)
			}
			if !ok {
				return fmt.Errorf("honker retry: job %d not in claimed state", hj.ID)
			}
			return nil
		},
	}
}

// --- Scheduler impl ---

type honkerScheduler struct {
	db    *honker.Database
	owner string
}

func (s *honkerScheduler) Schedule(t ScheduledTask) error {
	return s.db.Scheduler().Add(honker.ScheduledTask{
		Name:    t.Name,
		Queue:   t.Queue,
		Cron:    t.Cron,
		Payload: t.Payload,
	})
}

func (s *honkerScheduler) Run(ctx context.Context) error {
	return s.db.Scheduler().Run(ctx, s.owner)
}
