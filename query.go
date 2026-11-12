package query

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/goccy/go-json"

	"github.com/neo4j-contrib/query-go-sdk/internal/api"
	"github.com/neo4j-contrib/query-go-sdk/internal/decode"
	"github.com/neo4j-contrib/query-go-sdk/internal/httpclient"
)

// initialStreamScanBuffer is the starting buffer size for the bufio.Scanner
// used to read streamed responses line by line. It grows up to
// queryService.maxResponseSize as needed for larger individual event lines
// (e.g. a Record containing a Node with many/large properties).
const initialStreamScanBuffer = 64 * 1024

// ============================================================================
// Types
// ============================================================================

type queryService struct {
	api               api.RequestService
	timeout           time.Duration
	logger            *slog.Logger
	useLegacyHTTP     bool
	accessMode        AccessMode
	streamingEnabled  bool
	maxResponseSize   int
	maxTransientRetry int           // default retries after a retryable Neo.TransientError.*; 0 disables
	maxNetworkRetry   int           // default retries after a network-level failure; 0 disables
	retryBackoff      time.Duration // base delay between transient retries; defaults to defaultTransientBackoff
}

// queryRequest is the JSON body sent to the Query API v2.
type queryRequest struct {
	Statement  string         `json:"statement"`
	Parameters map[string]any `json:"parameters,omitempty"`
	AccessMode string         `json:"accessMode,omitempty"`

	IncludeCounters     bool                 `json:"includeCounters,omitempty"`
	TxMetadata          map[string]any       `json:"txMetadata,omitempty"`
	MaxExecutionTime    int                  `json:"maxExecutionTime,omitempty"` // seconds
	Bookmarks           []string             `json:"bookmarks,omitempty"`
	NotificationsFilter *NotificationsFilter `json:"notificationsFilter,omitempty"`
}

// legacyQueryRequest is the JSON body sent to the legacy HTTP Transaction API.
type legacyQueryRequest struct {
	Statements []legacyStatement `json:"statements"`
}

type legacyStatement struct {
	Statement  string         `json:"statement"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

// accessModeValue is the Query API v2 body-field spelling of AccessMode:
// "Read" or "Write", capitalized per https://neo4j.com/docs/query-api/current/routing/.
// AccessModeUnset returns "", which the queryRequest's omitempty tag drops
// from the body entirely.
func accessModeValue(mode AccessMode) string {
	switch mode {
	case AccessModeRead:
		return "Read"
	case AccessModeWrite:
		return "Write"
	default:
		return ""
	}
}

// ============================================================================
// Service
// ============================================================================

// callPlan is the resolved, per-call execution plan: the effective timeout and
// retry budgets, plus the request body, marshalled once and reused for every
// attempt.
type callPlan struct {
	timeout      time.Duration
	transientMax int
	networkMax   int
	accessMode   AccessMode
	body         []byte
}

// prepare resolves per-call options against the client defaults and builds the
// request body.
func (q *queryService) prepare(qry string, qryParams map[string]any, opts []CallOption) (*callPlan, error) {
	o, err := applyCallOptions(opts)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	if q.useLegacyHTTP {
		if name := o.requiresV2(); name != "" {
			return nil, fmt.Errorf("query: %s is not supported with FlavorLegacyHTTP", name)
		}
	}

	plan := &callPlan{
		timeout:      q.timeout,
		transientMax: q.maxTransientRetry,
		networkMax:   q.maxNetworkRetry,
		accessMode:   q.accessMode,
	}
	if o.timeout > 0 {
		plan.timeout = o.timeout
	}
	if o.accessMode != nil {
		plan.accessMode = *o.accessMode
	}
	switch {
	case o.retry != nil:
		plan.transientMax, plan.networkMax = o.retry.Transient, o.retry.Network
	case usesInTransactions(qry):
		// CALL { } IN TRANSACTIONS commits batches as it goes, so repeating the
		// statement after a failure repeats work that already committed.
		// Callers who know better can opt back in with WithRetry.
		plan.transientMax, plan.networkMax = 0, 0
	}

	var reqPayload any
	if q.useLegacyHTTP {
		reqPayload = legacyQueryRequest{
			Statements: []legacyStatement{{Statement: qry, Parameters: qryParams}},
		}
	} else {
		req := queryRequest{
			Statement:       qry,
			Parameters:      qryParams,
			AccessMode:      accessModeValue(plan.accessMode),
			IncludeCounters: o.includeCounters,
			TxMetadata:      o.txMetadata,
			Bookmarks:       o.bookmarks,
		}
		if o.maxExecutionTime > 0 {
			req.MaxExecutionTime = int((o.maxExecutionTime + time.Second - 1) / time.Second)
		}
		req.NotificationsFilter = o.notifications
		reqPayload = req
	}

	plan.body, err = json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("query: marshal request: %w", err)
	}
	return plan, nil
}

// transportCtx attaches the per-call transport settings to ctx.
func (q *queryService) transportCtx(ctx context.Context, plan *callPlan) context.Context {
	ctx = httpclient.WithMaxNetworkRetries(ctx, plan.networkMax)
	if q.useLegacyHTTP {
		ctx = api.WithAccessMode(ctx, apiAccessMode(plan.accessMode))
	}
	return ctx
}

func apiAccessMode(m AccessMode) api.AccessMode {
	switch m {
	case AccessModeRead:
		return api.AccessModeRead
	case AccessModeWrite:
		return api.AccessModeWrite
	default:
		return api.AccessModeUnset
	}
}

// logFailure logs err for a failed call. Errors the caller receives anyway
// and that describe a rejected statement (Neo4j query errors, HTTP API
// errors) are expected outcomes, so they are logged at Debug; anything else
// (transport failures, decode failures) is unexpected and logged at Error.
func (q *queryService) logFailure(ctx context.Context, msg string, err error) {
	var qErrs *decode.QueryErrors
	var apiErr *api.Error
	if errors.As(err, &qErrs) || errors.As(err, &apiErr) {
		q.logger.DebugContext(ctx, msg, slog.String("error", err.Error()))
		return
	}
	q.logger.ErrorContext(ctx, msg, slog.String("error", err.Error()))
}

// Execute runs a Cypher statement and returns the decoded response.
//
// CallOptions customise this call: timeout, retry policy, access mode,
// counters, transaction metadata, max execution time, bookmarks and
// notification filters.
//
// If the server rejects the statement with a retryable Neo.TransientError.*
// (for example a deadlock), the statement is re-run up to the transient retry
// count (see RetryPolicy and WithMaxTransientRetry) with exponential backoff.
// All attempts share the single timeout deadline.
func (q *queryService) Execute(ctx context.Context, qry string, qryParams map[string]any, opts ...CallOption) (*Response, error) {
	plan, err := q.prepare(qry, qryParams, opts)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, plan.timeout)
	defer cancel()
	ctx = q.transportCtx(ctx, plan)

	q.logger.DebugContext(ctx, "running query")

	for attempt := 0; ; attempt++ {
		result, err := q.executeOnce(ctx, plan)
		if err == nil || !q.shouldRetry(ctx, plan, err, attempt) {
			return result, err
		}
		if waitErr := q.backoff(ctx, plan, err, attempt); waitErr != nil {
			return nil, err
		}
	}
}

func (q *queryService) executeOnce(ctx context.Context, plan *callPlan) (*Response, error) {
	resp, err := q.api.Post(ctx, plan.body)
	if err != nil {
		q.logFailure(ctx, "failed to query", err)
		return nil, err
	}

	var result *decode.Response
	if q.useLegacyHTTP {
		result, err = decode.DecodeLegacyResponse(resp.Body)
	} else {
		result, err = decode.DecodeResponse(resp.Body)
	}
	if err != nil {
		q.logFailure(ctx, "failed to decode response", err)
		return nil, err
	}

	return result, nil
}

// ExecuteStream runs a Cypher statement and returns a StreamResult that
// decodes records incrementally as they arrive, instead of buffering the
// entire response. Requires the client to be constructed with
// WithStreamingSupport(true).
//
// Unlike Execute, the context timeout applied here (WithTimeout or
// WithCallTimeout) is not released when ExecuteStream returns — it stays live
// for as long as the caller is draining the returned StreamResult, since the
// HTTP body is read lazily during iteration. It is released by
// StreamResult.Close (called automatically once Records() finishes, or
// explicitly by the caller). The timeout therefore bounds the entire lifetime
// of a stream, from request start through full consumption — callers
// streaming very large or slowly-consumed results should raise it
// accordingly.
//
// Retryable Neo.TransientError.* failures reported before the first Header
// event (i.e. before any record has been delivered) are retried like Execute.
// Errors that arrive mid-stream are never retried.
func (q *queryService) ExecuteStream(ctx context.Context, qry string, qryParams map[string]any, opts ...CallOption) (*StreamResult, error) {
	if !q.streamingEnabled {
		return nil, errors.New("query: ExecuteStream requires the client to be constructed with WithStreamingSupport(true)")
	}

	plan, err := q.prepare(qry, qryParams, opts)
	if err != nil {
		return nil, err
	}

	q.logger.DebugContext(ctx, "running streaming query")

	for attempt := 0; ; attempt++ {
		res, err := q.executeStreamOnce(ctx, plan)
		if err == nil || !q.shouldRetry(ctx, plan, err, attempt) {
			return res, err
		}
		if waitErr := q.backoff(ctx, plan, err, attempt); waitErr != nil {
			return nil, err
		}
	}
}

func (q *queryService) executeStreamOnce(ctx context.Context, plan *callPlan) (*StreamResult, error) {
	ctx, cancel := context.WithTimeout(ctx, plan.timeout)
	ctx = q.transportCtx(ctx, plan)

	resp, err := q.api.PostStream(ctx, plan.body)
	if err != nil {
		cancel()
		q.logFailure(ctx, "failed to query", err)
		return nil, err
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, initialStreamScanBuffer), q.maxResponseSize)

	if !scanner.Scan() {
		defer cancel()
		defer func() { _ = resp.Body.Close() }()
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("query: stream: read: %w", err)
		}
		return nil, fmt.Errorf("query: stream: empty response")
	}

	eventType, body, err := decode.DecodeStreamEnvelope(scanner.Bytes())
	if err != nil {
		cancel()
		_ = resp.Body.Close()
		return nil, err
	}

	switch eventType {
	case decode.StreamEventHeader:
		fields, err := decode.DecodeStreamHeader(body)
		if err != nil {
			cancel()
			_ = resp.Body.Close()
			return nil, err
		}
		return &StreamResult{fields: fields, body: resp.Body, scanner: scanner, cancel: cancel}, nil

	case decode.StreamEventError:
		cancel()
		_ = resp.Body.Close()
		queryErrs, err := decode.DecodeStreamError(body)
		if err != nil {
			return nil, err
		}
		return nil, queryErrs

	default:
		cancel()
		_ = resp.Body.Close()
		return nil, fmt.Errorf("query: stream: expected Header or Error as first event, got %q", eventType)
	}
}
