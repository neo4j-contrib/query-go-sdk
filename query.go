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
	accessMode    bool
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
		reqPayload = queryRequest{Statement: qry, Parameters: qryParams, AccessMode: fmt.Sprintf("%v", q.accessMode)}
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
