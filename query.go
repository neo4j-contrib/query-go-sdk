package query

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/goccy/go-json"

	"github.com/neo4j-contrib/query-go-sdk/internal/api"
	"github.com/neo4j-contrib/query-go-sdk/internal/decode"
)

// ============================================================================
// Types
// ============================================================================

type queryService struct {
	api           api.RequestService
	timeout       time.Duration
	logger        *slog.Logger
	useLegacyHTTP bool
	accessMode    AccessMode
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
