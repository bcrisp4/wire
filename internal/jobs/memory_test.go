package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryQueue_RoundTrip(t *testing.T) {
	q := NewMemoryQueue()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.NoError(t, q.Enqueue(ctx, Job{Queue: "feed.poll", Payload: []byte(`{"id":1}`)}))

	got, err := q.Claim(ctx, "feed.poll")
	require.NoError(t, err)
	assert.Equal(t, "feed.poll", got.Queue)
	assert.Equal(t, `{"id":1}`, string(got.Payload))

	require.NoError(t, q.Ack(ctx, got.ID))
}

func TestMemoryQueue_ClaimEmptyReturnsErrNoJob(t *testing.T) {
	q := NewMemoryQueue()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := q.Claim(ctx, "feed.poll")
	assert.ErrorIs(t, err, ErrNoJob)
}

func TestMemoryQueue_FIFO(t *testing.T) {
	q := NewMemoryQueue()
	ctx := context.Background()
	require.NoError(t, q.Enqueue(ctx, Job{Queue: "x", Payload: []byte("a")}))
	require.NoError(t, q.Enqueue(ctx, Job{Queue: "x", Payload: []byte("b")}))

	first, err := q.Claim(ctx, "x")
	require.NoError(t, err)
	assert.Equal(t, "a", string(first.Payload))

	second, err := q.Claim(ctx, "x")
	require.NoError(t, err)
	assert.Equal(t, "b", string(second.Payload))
}
