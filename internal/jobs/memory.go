package jobs

import (
	"context"
	"sync"
	"sync/atomic"
)

// MemoryQueue is an in-process FIFO queue used in unit tests.
// It is NOT durable and NOT cross-process. Production uses the Honker backend.
type MemoryQueue struct {
	mu     sync.Mutex
	queues map[string][]Job
	nextID atomic.Int64
}

func NewMemoryQueue() *MemoryQueue {
	return &MemoryQueue{queues: make(map[string][]Job)}
}

func (m *MemoryQueue) Enqueue(_ context.Context, j Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j.ID = m.nextID.Add(1)
	m.queues[j.Queue] = append(m.queues[j.Queue], j)
	return nil
}

func (m *MemoryQueue) Claim(_ context.Context, queue string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q := m.queues[queue]
	if len(q) == 0 {
		return Job{}, ErrNoJob
	}
	j := q[0]
	m.queues[queue] = q[1:]
	return j, nil
}

func (*MemoryQueue) Ack(context.Context, int64) error               { return nil }
func (*MemoryQueue) Fail(context.Context, int64, string, int) error { return nil }
func (*MemoryQueue) Close() error                                   { return nil }
