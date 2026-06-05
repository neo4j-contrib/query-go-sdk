package query

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/neo4j-contrib/query-go-sdk/internal/api"
	"github.com/neo4j-contrib/query-go-sdk/internal/decode"
)

// ============================================================================
// Types
// ============================================================================

type queryService struct {
	api     api.RequestService
	timeout time.Duration
	logger  *slog.Logger
}

// queryRequest is the JSON body sent to the Query API.
type queryRequest struct {
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
	bodyMarshalled, err := json.Marshal(queryRequest{
		Statement:  qry,
		Parameters: qryParams,
	})
	if err != nil {
		return nil, fmt.Errorf("query: marshal request: %w", err)
	}

	body := string(bodyMarshalled)

	resp, err := q.api.Post(ctx, body)
	if err != nil {
		q.logger.ErrorContext(ctx, "failed to query", slog.String("error", err.Error()))
		return nil, err
	}

	result, err := decode.DecodeResponse(resp.Body)
	if err != nil {
		q.logger.ErrorContext(ctx, "failed to decode response", slog.String("error", err.Error()))
		return nil, err
	}

	return result, nil
}
