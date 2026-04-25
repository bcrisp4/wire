package jobs

import "context"

// AttachCallbacks returns j with the given ack/fail callbacks bound. It is a
// test helper for downstream packages: the production Honker backend wires its
// own callbacks at claim time, but in-process MemoryQueue jobs have nil
// callbacks (Ack/Fail are no-ops). Tests that want to assert on retry behaviour
// can wrap a MemoryQueue Claim result with this helper.
//
// Returning the same *Job rather than a copy keeps Job.ID stable so callers can
// correlate fails against the originally-enqueued job.
func AttachCallbacks(
	j *Job,
	ack func(context.Context) error,
	fail func(context.Context, string, int) error,
) *Job {
	if j == nil {
		return nil
	}
	j.ack = ack
	j.fail = fail
	return j
}
