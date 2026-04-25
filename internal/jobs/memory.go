package jobs

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
)

// MemoryQueue is an in-process FIFO queue used in unit tests.
// It is NOT durable and NOT cross-process. Production uses the Honker backend.
type MemoryQueue struct {
	mu     sync.Mutex
	queues map[string][]*Job
	nextID atomic.Int64
}

func NewMemoryQueue() *MemoryQueue {
	return &MemoryQueue{queues: make(map[string][]*Job)}
}

func (m *MemoryQueue) Enqueue(_ context.Context, queue string, payload json.RawMessage) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.nextID.Add(1)
	// Copy payload so callers can mutate the source slice safely.
	p := make(json.RawMessage, len(payload))
	copy(p, payload)
	m.queues[queue] = append(m.queues[queue], &Job{
		ID:      id,
		Queue:   queue,
		Payload: p,
	})
	return id, nil
}

func (m *MemoryQueue) Claim(_ context.Context, queue, _ string) (*Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q := m.queues[queue]
	if len(q) == 0 {
		return nil, ErrNoJob
	}
	j := q[0]
	m.queues[queue] = q[1:]
	j.Attempts++
	// MemoryQueue Ack/Fail are no-ops; leave ack/fail nil.
	return j, nil
}

func (*MemoryQueue) Close() error { return nil }
