package ratelimit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	honker "github.com/russellromney/honker-go"
)

// requireExtension mirrors internal/jobs/honker_test.go: skip when the Honker
// extension cdylib is not built locally. honker-go expects the path WITHOUT
// the .so/.dylib suffix, but we look for the file with the suffix.
func requireExtension(t *testing.T) string {
	t.Helper()
	envPath := os.Getenv("WIRE_HONKER_EXTENSION_PATH")
	candidate := envPath
	if candidate == "" {
		candidate = "../../build/libhonker_ext"
	}
	if abs, err := filepath.Abs(candidate); err == nil {
		candidate = abs
	}
	for _, suffix := range []string{".so", ".dylib"} {
		if _, err := os.Stat(candidate + suffix); err == nil {
			return candidate
		}
	}
	t.Skipf("Honker extension not found near %s (run 'make extension'); skipping integration test", candidate)
	return ""
}

func TestBackoffSeconds_ZeroAttemptsReturnsBase(t *testing.T) {
	assert.Equal(t, 60, BackoffSeconds(0))
	assert.Equal(t, 60, BackoffSeconds(-5))
}

func TestBackoffSeconds_FirstAttemptInBaseRange(t *testing.T) {
	for i := 0; i < 100; i++ {
		got := BackoffSeconds(1)
		assert.GreaterOrEqual(t, got, 0)
		assert.LessOrEqual(t, got, 60)
	}
}

func TestBackoffSeconds_SecondAttemptInDoubleRange(t *testing.T) {
	for i := 0; i < 100; i++ {
		got := BackoffSeconds(2)
		assert.GreaterOrEqual(t, got, 0)
		assert.LessOrEqual(t, got, 120)
	}
}

func TestBackoffSeconds_CappedAtMax(t *testing.T) {
	// 60 * 2^19 vastly exceeds 86400; must clamp.
	for i := 0; i < 50; i++ {
		got := BackoffSeconds(20)
		assert.GreaterOrEqual(t, got, 0)
		assert.LessOrEqual(t, got, 86400)
	}
	// Sanity: an even larger attempt count still respects the cap.
	assert.LessOrEqual(t, BackoffSeconds(100), 86400)
}

func TestBackoffSeconds_JitterVaries(t *testing.T) {
	// With full jitter on a 60s window, 50 draws should produce >1 distinct value
	// with overwhelming probability. (Probability of all 50 being identical is
	// astronomically small for any sensible RNG.)
	seen := make(map[int]struct{})
	for i := 0; i < 50; i++ {
		seen[BackoffSeconds(1)] = struct{}{}
	}
	assert.Greater(t, len(seen), 1, "expected jitter to produce varied delays")
}

func TestAcquire_WindowAllowsThenBlocksThenResets(t *testing.T) {
	ext := requireExtension(t)
	dbPath := filepath.Join(t.TempDir(), "ratelimit.db")
	hdb, err := honker.Open(dbPath, ext)
	require.NoError(t, err)
	defer hdb.Close()

	db := hdb.Raw()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const host = "example.com"
	const limit = 2
	const windowSec = 1

	// First `limit` acquires must succeed.
	for i := 0; i < limit; i++ {
		ok, err := Acquire(ctx, db, host, limit, windowSec)
		require.NoError(t, err)
		assert.True(t, ok, "acquire #%d should succeed within window", i+1)
	}
	// (limit+1)-th must be denied.
	ok, err := Acquire(ctx, db, host, limit, windowSec)
	require.NoError(t, err)
	assert.False(t, ok, "acquire beyond limit should be denied")

	// After windowSec passes, a fresh acquire must succeed again.
	time.Sleep(time.Duration(windowSec+1) * time.Second)
	ok, err = Acquire(ctx, db, host, limit, windowSec)
	require.NoError(t, err)
	assert.True(t, ok, "acquire should succeed after window resets")
}
