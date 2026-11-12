package query

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// RetryPolicy says how many times a failed call is retried, per failure kind.
// The two kinds have different safety properties, so they are set separately:
//
//   - Transient is the number of retries after a Neo.TransientError.* (for
//     example a deadlock). The server rolled the request back, so repeating a
//     plain statement is safe. It is NOT safe for CALL { } IN TRANSACTIONS,
//     where batches before the failure have already committed.
//   - Network is the number of retries after a network-level failure with no
//     response (connection refused or reset, DNS). The outcome is ambiguous —
//     the server may have committed — so repeating a non-idempotent write such
//     as CREATE can duplicate it.
//
// The zero value disables all retries.
type RetryPolicy struct {
	Transient int
	Network   int
}

// NotificationsFilter restricts which server notifications are raised for a
// statement. Zero-valued fields are omitted. Requires Neo4j 2026.08+.
type NotificationsFilter struct {
	// MinimumSeverityLevel is "WARNING", "INFORMATION" or "OFF".
	MinimumSeverityLevel string `json:"minimumSeverityLevel,omitempty"`
	// DisabledCategories lists notification categories to suppress, e.g.
	// "HINT", "PERFORMANCE", "DEPRECATION".
	DisabledCategories []string `json:"disabledCategories,omitempty"`
}

// callOptions holds the per-call settings collected from CallOptions.
type callOptions struct {
	timeout          time.Duration // 0 = client default
	retry            *RetryPolicy  // nil = client default (adjusted for IN TRANSACTIONS)
	accessMode       *AccessMode   // nil = client default
	includeCounters  bool
	txMetadata       map[string]any
	maxExecutionTime time.Duration
	bookmarks        []string
	notifications    *NotificationsFilter
	err              error
}

// CallOption customises a single Execute or ExecuteStream call.
type CallOption func(*callOptions)

// WithCallTimeout overrides the client-wide WithTimeout for this call. For
// ExecuteStream the timeout bounds the whole stream lifetime.
func WithCallTimeout(d time.Duration) CallOption {
	return func(o *callOptions) {
		if d <= 0 {
			o.err = errors.New("call timeout must be greater than zero")
			return
		}
		o.timeout = d
	}
}

// WithRetry overrides the client's retry settings for this call; see
// RetryPolicy for the safety trade-offs. Passing it explicitly also overrides
// the default of disabling retries for statements containing IN TRANSACTIONS.
// Negative counts are rejected.
func WithRetry(p RetryPolicy) CallOption {
	return func(o *callOptions) {
		if p.Transient < 0 || p.Network < 0 {
			o.err = errors.New("retry counts must not be negative")
			return
		}
		o.retry = &p
	}
}

// WithCallAccessMode overrides the client-wide WithAccessMode for this call.
func WithCallAccessMode(m AccessMode) CallOption {
	return func(o *callOptions) {
		if m != AccessModeUnset && m != AccessModeRead && m != AccessModeWrite {
			o.err = fmt.Errorf("invalid access mode: %d", m)
			return
		}
		o.accessMode = &m
	}
}

// WithIncludeCounters asks the server to report how the statement changed the
// database; the result is exposed as Response.Counters, EagerResult.Counters
// and StreamSummary.Counters.
func WithIncludeCounters() CallOption {
	return func(o *callOptions) { o.includeCounters = true }
}

// WithTxMetadata attaches metadata to the transaction. It is logged in the
// server's query.log and shown by SHOW TRANSACTIONS. Requires Neo4j 2026.04+.
func WithTxMetadata(m map[string]any) CallOption {
	return func(o *callOptions) { o.txMetadata = m }
}

// WithMaxExecutionTime sets a server-side transaction timeout; the server
// terminates transactions that run longer. The wire unit is whole seconds, so
// d is rounded up to the next second. Requires Neo4j 2026.04+. This is
// independent of WithCallTimeout, which bounds the client's wait.
func WithMaxExecutionTime(d time.Duration) CallOption {
	return func(o *callOptions) {
		if d <= 0 {
			o.err = errors.New("max execution time must be greater than zero")
			return
		}
		o.maxExecutionTime = d
	}
}

// WithBookmarks makes the server wait until it has caught up with the given
// bookmarks (as returned in Response.Bookmarks) before running the statement.
func WithBookmarks(bookmarks ...string) CallOption {
	return func(o *callOptions) { o.bookmarks = bookmarks }
}

// WithNotificationsFilter restricts the notifications the server raises for
// this statement. Requires Neo4j 2026.08+.
func WithNotificationsFilter(f NotificationsFilter) CallOption {
	return func(o *callOptions) { o.notifications = &f }
}

func applyCallOptions(opts []CallOption) (callOptions, error) {
	var o callOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o, o.err
}

// requiresV2 reports a name for the first option that only exists in the
// Query API v2 body, or "" if none was set. The legacy HTTP Transaction API
// has no equivalent field.
func (o *callOptions) requiresV2() string {
	switch {
	case o.includeCounters:
		return "WithIncludeCounters"
	case o.txMetadata != nil:
		return "WithTxMetadata"
	case o.maxExecutionTime > 0:
		return "WithMaxExecutionTime"
	case len(o.bookmarks) > 0:
		return "WithBookmarks"
	case o.notifications != nil:
		return "WithNotificationsFilter"
	}
	return ""
}

// inTransactionsRE matches CALL { ... } IN [n] [CONCURRENT] TRANSACTIONS.
// It is deliberately loose (it also matches inside comments or strings): a
// false positive only turns automatic retries off, which is the safe side.
var inTransactionsRE = regexp.MustCompile(`(?i)\bIN\s+(?:\S+\s+)?(?:CONCURRENT\s+)?TRANSACTIONS\b`)

func usesInTransactions(stmt string) bool { return inTransactionsRE.MatchString(stmt) }
