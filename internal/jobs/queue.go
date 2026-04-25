// Package jobs defines the queue/scheduler abstraction used by Wire.
//
// Production wires the Honker-backed implementation in honker.go.
// Unit tests use the in-process MemoryQueue in memory.go.
// Both satisfy the same Queue and Scheduler interfaces, allowing the
// production backend to be swapped if upstream blockers ever force it.
package jobs

import (
	"context"
	"errors"
)

// ErrNoJob is returned by Queue.Claim when no job is available.
var ErrNoJob = errors.New("jobs: no job available")

type Job struct {
	ID      int64
	Queue   string
	Payload []byte
}

// Queue is the durable job queue contract.
type Queue interface {
	Enqueue(ctx context.Context, j Job) error
	Claim(ctx context.Context, queue string) (Job, error) // returns ErrNoJob on empty
	Ack(ctx context.Context, jobID int64) error
	Fail(ctx context.Context, jobID int64, errMsg string, retryAfterSec int) error
	Close() error
}

// Scheduler is the cron contract.
type Scheduler interface {
	Schedule(task ScheduledTask) error
	Run(ctx context.Context) error // blocks until ctx canceled
}

// ScheduledTask defines a recurring enqueue.
type ScheduledTask struct {
	Name    string // unique identifier
	Cron    string // 5-field cron expression
	Queue   string // queue to enqueue into when fired
	Payload []byte
}
