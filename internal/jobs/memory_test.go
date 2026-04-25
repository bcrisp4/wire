package jobs

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryQueue_RoundTrip(t *testing.T) {
	q := NewMemoryQueue()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	id, err := q.Enqueue(ctx, "feed.poll", json.RawMessage(`{"id":1}`))
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	got, err := q.Claim(ctx, "feed.poll", "w1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "feed.poll", got.Queue)
	assert.Equal(t, `{"id":1}`, string(got.Payload))

	require.NoError(t, got.Ack(ctx))
}

func TestMemoryQueue_ClaimEmptyReturnsErrNoJob(t *testing.T) {
	q := NewMemoryQueue()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := q.Claim(ctx, "feed.poll", "w1")
	assert.ErrorIs(t, err, ErrNoJob)
}

func TestMemoryQueue_FIFO(t *testing.T) {
	q := NewMemoryQueue()
	ctx := context.Background()
	_, err := q.Enqueue(ctx, "x", json.RawMessage(`"a"`))
	require.NoError(t, err)
	_, err = q.Enqueue(ctx, "x", json.RawMessage(`"b"`))
	require.NoError(t, err)

	first, err := q.Claim(ctx, "x", "w1")
	require.NoError(t, err)
	assert.Equal(t, `"a"`, string(first.Payload))

	second, err := q.Claim(ctx, "x", "w1")
	require.NoError(t, err)
	assert.Equal(t, `"b"`, string(second.Payload))
}

func TestMemoryQueue_PayloadIsolation(t *testing.T) {
	// Mutating the source payload after Enqueue must not change the queued copy.
	q := NewMemoryQueue()
	ctx := context.Background()
	src := []byte(`{"v":1}`)
	_, err := q.Enqueue(ctx, "q", src)
	require.NoError(t, err)
	src[5] = '9'
	got, err := q.Claim(ctx, "q", "w")
	require.NoError(t, err)
	assert.Equal(t, `{"v":1}`, string(got.Payload))
}
