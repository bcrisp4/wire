// Package ratelimit provides per-host rate limiting and exponential backoff
// helpers used by Wire's poll/extract workers.
//
// Per-host rate limiting is a thin wrapper over Honker's fixed-window
// honker_rate_limit_try SQL function. Backoff uses exponential growth with
// full jitter so the worker loop can compute Job.Fail(retryAfterSec=...) from
// Job.Attempts.
package ratelimit

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
)

const (
	backoffBaseSeconds = 60    // 1 minute
	backoffMaxSeconds  = 86400 // 24 hours
)

// Acquire attempts to take a slot for `host` with up to `limit` requests per
// `windowSec` seconds. Returns true if the caller may proceed.
//
// It is a thin wrapper over Honker's honker_rate_limit_try SQL function,
// keying by "feed-host:" + host. There is no release/refund — the underlying
// counter is a fixed window.
func Acquire(ctx context.Context, db *sql.DB, host string, limit, windowSec int) (bool, error) {
	var n int64
	err := db.QueryRowContext(ctx,
		"SELECT honker_rate_limit_try(?, ?, ?)",
		"feed-host:"+host, limit, windowSec,
	).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("ratelimit: %w", err)
	}
	return n == 1, nil
}

// BackoffSeconds returns the delay (in seconds) for retrying a failed job
// after `attempts` previous failures (1-indexed: first failure -> attempts=1).
//
// Schedule: base * 2^(attempts-1) with full jitter, capped at maxSeconds.
//
//	base = 60 (1 minute), max = 86400 (24 hours).
//
// attempts <= 0 returns base.
func BackoffSeconds(attempts int) int {
	if attempts <= 0 {
		return backoffBaseSeconds
	}
	// base * 2^(attempts-1) with overflow-safe saturation at the cap.
	window := backoffBaseSeconds
	for i := 1; i < attempts; i++ {
		window <<= 1
		if window >= backoffMaxSeconds || window <= 0 {
			window = backoffMaxSeconds
			break
		}
	}
	return rand.IntN(window + 1)
}
