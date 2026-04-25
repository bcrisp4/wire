// Package jobs defines the queue/scheduler abstraction used by Wire.
//
// Production wires the Honker-backed implementation in honker.go.
// Unit tests use the in-process MemoryQueue in memory.go.
// Both satisfy the same Queue and Scheduler interfaces, allowing the
// production backend to be swapped if upstream blockers ever force it.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
)

// ErrNoJob is returned by Queue.Claim when no job is available.
var ErrNoJob = errors.New("jobs: no job available")

// Queue and scheduled-task names. New names should be added here rather than
// inlined as string literals at registration sites.
const (
	QueueHeartbeat = "wire.heartbeat"
	// Unit 4: feed polling.
	QueueFeedPoll = "feed.poll"
)

// Queue is the durable job queue contract.
type Queue interface {
	Enqueue(ctx context.Context, queue string, payload json.RawMessage) (int64, error)
	Claim(ctx context.Context, queue, workerID string) (*Job, error) // returns ErrNoJob on empty
	Close() error
}

// Scheduler is the cron contract.
type Scheduler interface {
	Schedule(task ScheduledTask) error
	Run(ctx context.Context) error // blocks until ctx canceled
}

// ScheduledTask is a recurring enqueue.
type ScheduledTask struct {
	Name    string          // unique identifier
	Cron    string          // 5-field cron expression
	Queue   string          // queue to enqueue into when fired
	Payload json.RawMessage // payload attached to enqueued jobs
}

// Job is a unit of work returned by Queue.Claim.
//
// Ack and Fail delegate to backend-specific closures bound at claim time.
// On the in-process MemoryQueue they are no-ops; on the Honker backend they
// call honker.Job.Ack / Retry on the underlying claimed row.
type Job struct {
	ID       int64
	Queue    string
	Payload  json.RawMessage
	Attempts int64

	ack  func(context.Context) error
	fail func(context.Context, string, int) error
}

// Ack acknowledges the job as successfully processed.
func (j *Job) Ack(ctx context.Context) error {
	if j == nil || j.ack == nil {
		return nil
	}
	return j.ack(ctx)
}

// Fail marks the job as failed and schedules a retry after retryAfterSec
// (or moves it to the dead-letter queue if it has hit MaxAttempts).
func (j *Job) Fail(ctx context.Context, errMsg string, retryAfterSec int) error {
	if j == nil || j.fail == nil {
		return nil
	}
	return j.fail(ctx, errMsg, retryAfterSec)
}
