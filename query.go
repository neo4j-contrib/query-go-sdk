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
	api              api.RequestService
	timeout          time.Duration
	logger           *slog.Logger
	useLegacyHTTP    bool
	accessMode       AccessMode
	streamingEnabled bool
	maxResponseSize  int
}

// queryRequest is the JSON body sent to the Query API v2.
type queryRequest struct {
	Statement  string         `json:"statement"`
	Parameters map[string]any `json:"parameters,omitempty"`
	AccessMode string         `json:"accessMode,omitempty"`
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

// Execute runs a Cypher statement and returns the decoded response.
func (q *queryService) Execute(ctx context.Context, qry string, qryParams map[string]any) (*Response, error) {
	ctx, cancel := context.WithTimeout(ctx, q.timeout)
	defer cancel()

	q.logger.DebugContext(ctx, "running query")

	// Build request body.
	var reqPayload any
	if q.useLegacyHTTP {
		reqPayload = legacyQueryRequest{
			Statements: []legacyStatement{{Statement: qry, Parameters: qryParams}},
		}
	} else {
		reqPayload = queryRequest{Statement: qry, Parameters: qryParams, AccessMode: accessModeValue(q.accessMode)}
	}

	bodyMarshalled, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("query: marshal request: %w", err)
	}

	resp, err := q.api.Post(ctx, string(bodyMarshalled))
	if err != nil {
		q.logger.ErrorContext(ctx, "failed to query", slog.String("error", err.Error()))
		return nil, err
	}

	var result *decode.Response
	if q.useLegacyHTTP {
		result, err = decode.DecodeLegacyResponse(resp.Body)
	} else {
		result, err = decode.DecodeResponse(resp.Body)
	}
	if err != nil {
		q.logger.ErrorContext(ctx, "failed to decode response", slog.String("error", err.Error()))
		return nil, err
	}

	return result, nil
}

// ExecuteStream runs a Cypher statement and returns a StreamResult that
// decodes records incrementally as they arrive, instead of buffering the
// entire response. Requires the client to be constructed with
// WithStreamingSupport(true).
//
// Unlike Execute, the context timeout applied here (q.timeout) is not
// released when ExecuteStream returns — it stays live for as long as the
// caller is draining the returned StreamResult, since the HTTP body is read
// lazily during iteration. It is released by StreamResult.Close (called
// automatically once Records() finishes, or explicitly by the caller). This
// means the same WithTimeout value that bounds Execute also bounds the
// entire lifetime of a stream, from request start through full consumption —
// callers streaming very large or slowly-consumed results should raise
// WithTimeout accordingly.
func (q *queryService) ExecuteStream(ctx context.Context, qry string, qryParams map[string]any) (*StreamResult, error) {
	if !q.streamingEnabled {
		return nil, errors.New("query: ExecuteStream requires the client to be constructed with WithStreamingSupport(true)")
	}

	q.logger.DebugContext(ctx, "running streaming query")

	ctx, cancel := context.WithTimeout(ctx, q.timeout)

	reqPayload := queryRequest{Statement: qry, Parameters: qryParams, AccessMode: accessModeValue(q.accessMode)}
	bodyMarshalled, err := json.Marshal(reqPayload)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("query: marshal request: %w", err)
	}

	resp, err := q.api.PostStream(ctx, string(bodyMarshalled))
	if err != nil {
		cancel()
		q.logger.ErrorContext(ctx, "failed to query", slog.String("error", err.Error()))
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
