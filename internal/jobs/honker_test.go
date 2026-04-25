package jobs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireExtension skips the test if WIRE_HONKER_EXTENSION_PATH points to a missing file.
// This keeps the suite runnable on machines where the Rust extension hasn't been built.
func requireExtension(t *testing.T) string {
	t.Helper()
	path := os.Getenv("WIRE_HONKER_EXTENSION_PATH")
	if path == "" {
		// Look for the dev-build path relative to the package.
		path = "../../build/libhonker_ext.so"
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("Honker extension not found at %s (run 'make extension'); skipping integration test", path)
	}
	return path
}

func TestHonker_RoundTrip(t *testing.T) {
	ext := requireExtension(t)
	dbPath := filepath.Join(t.TempDir(), "honker.db")

	hb, err := NewHonker(HonkerOptions{DBPath: dbPath, ExtensionPath: ext})
	require.NoError(t, err)
	defer hb.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	id, err := hb.Queue().Enqueue(ctx, "test.q", json.RawMessage(`{"k":"v"}`))
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	job, err := hb.Queue().Claim(ctx, "test.q", "w1")
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, "test.q", job.Queue)
	assert.Equal(t, `{"k":"v"}`, string(job.Payload))
	assert.Equal(t, int64(1), job.Attempts)

	require.NoError(t, job.Ack(ctx))
}

func TestHonker_ClaimEmpty(t *testing.T) {
	ext := requireExtension(t)
	dbPath := filepath.Join(t.TempDir(), "honker.db")
	hb, err := NewHonker(HonkerOptions{DBPath: dbPath, ExtensionPath: ext})
	require.NoError(t, err)
	defer hb.Close()

	_, err = hb.Queue().Claim(context.Background(), "nothing.here", "w1")
	assert.ErrorIs(t, err, ErrNoJob)
}

func TestHonker_RawDB_AppSQLCoexists(t *testing.T) {
	ext := requireExtension(t)
	dbPath := filepath.Join(t.TempDir(), "honker.db")
	hb, err := NewHonker(HonkerOptions{DBPath: dbPath, ExtensionPath: ext})
	require.NoError(t, err)
	defer hb.Close()

	db := hb.RawDB()
	_, err = db.Exec(`CREATE TABLE app_t(x INT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO app_t VALUES (42)`)
	require.NoError(t, err)
	var x int
	require.NoError(t, db.QueryRow(`SELECT x FROM app_t`).Scan(&x))
	assert.Equal(t, 42, x)

	// Honker tables also present.
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name LIKE '_honker_%'`).Scan(&n))
	assert.Greater(t, n, 0)
}

func TestHonker_ScheduleAndRun(t *testing.T) {
	ext := requireExtension(t)
	dbPath := filepath.Join(t.TempDir(), "honker.db")
	hb, err := NewHonker(HonkerOptions{DBPath: dbPath, ExtensionPath: ext})
	require.NoError(t, err)
	defer hb.Close()

	require.NoError(t, hb.Scheduler().Schedule(ScheduledTask{
		Name:  "wire.test.heartbeat",
		Cron:  "* * * * *",
		Queue: "wire.test.heartbeat",
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	// Run should exit cleanly when ctx is canceled.
	err = hb.Scheduler().Run(ctx)
	assert.True(t, err == nil || ctxErr(err), "expected nil or context error, got %v", err)
}

func ctxErr(err error) bool {
	return err == context.Canceled || err == context.DeadlineExceeded
}
