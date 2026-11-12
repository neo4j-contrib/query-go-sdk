package query

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/neo4j-contrib/query-go-sdk/internal/api"
	"github.com/neo4j-contrib/query-go-sdk/internal/decode"
)

const (
	transientCodePrefix = "Neo.TransientError."

	// defaultTransientBackoff is the base delay before the first transient
	// retry; it doubles on each subsequent attempt up to maxTransientBackoff.
	defaultTransientBackoff = 200 * time.Millisecond
	maxTransientBackoff     = 5 * time.Second
)

// isRetryableCode reports whether a Neo4j status code is a transient error
// that is safe to retry. Neo.TransientError.Transaction.Terminated and
// Neo.TransientError.Transaction.LockClientStopped are excluded: they mean
// the transaction was deliberately stopped, so retrying would defeat that
// (this matches the official Neo4j drivers).
func isRetryableCode(code string) bool {
	if !strings.HasPrefix(code, transientCodePrefix) {
		return false
	}
	switch code {
	case "Neo.TransientError.Transaction.Terminated",
		"Neo.TransientError.Transaction.LockClientStopped":
		return false
	}
	return true
}

// isRetryableTransient reports whether err carries a retryable
// Neo.TransientError.* code, whether it was delivered in a 2xx body, a
// streamed Error event, or an HTTP error response.
func isRetryableTransient(err error) bool {
	var qErrs *decode.QueryErrors
	if errors.As(err, &qErrs) {
		for _, e := range qErrs.Errors {
			if isRetryableCode(e.Code) {
				return true
			}
		}
	}
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		for _, d := range apiErr.Details {
			if isRetryableCode(d.Code) {
				return true
			}
		}
	}
	return false
}

// shouldRetry reports whether attempt (0-based) that failed with err should be
// followed by another attempt.
func (q *queryService) shouldRetry(ctx context.Context, plan *callPlan, err error, attempt int) bool {
	return attempt < plan.transientMax && ctx.Err() == nil && isRetryableTransient(err)
}

// backoff logs and sleeps before the next attempt using exponential backoff
// with jitter. It returns the context error if ctx ends while waiting.
func (q *queryService) backoff(ctx context.Context, plan *callPlan, err error, attempt int) error {
	base := q.retryBackoff
	if base <= 0 {
		base = defaultTransientBackoff
	}
	d := base << attempt
	if d > maxTransientBackoff || d <= 0 {
		d = maxTransientBackoff
	}
	// Jitter in [d/2, d] so concurrent deadlock victims don't retry in lockstep.
	d = d/2 + time.Duration(rand.Int64N(int64(d/2)+1))

	q.logger.WarnContext(ctx, "retrying after transient error",
		slog.Int("attempt", attempt+1),
		slog.Int("max_retry", plan.transientMax),
		slog.Duration("wait", d),
		slog.String("error", err.Error()),
	)

	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
