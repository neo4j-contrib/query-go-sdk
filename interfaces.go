// Package query provides a Go client library for the Neo4j Query API.
package query

import (
	"context"

	"github.com/neo4j-contrib/query-go-sdk/internal/decode"
)

// QueryService defines operations for using the Query API
type QueryService interface {
	// Execute runs a Cypher statement and returns the raw decoded response.
	// Use WithTransformer to map the result to a typed value.
	Execute(ctx context.Context, qry string, qryParams map[string]any) (*Response, error)
	// ExecuteStream runs a Cypher statement and returns a StreamResult that
	// decodes records incrementally as they arrive over the wire, instead of
	// buffering the entire response. Requires the client to be constructed
	// with WithStreamingSupport(true).
	ExecuteStream(ctx context.Context, qry string, qryParams map[string]any) (*StreamResult, error)
}

// Compile-time interface compliance checks
var (
	_ QueryService = (*queryService)(nil)
)

// ============================================================================
// Re-exported types from internal/decode
//
// These aliases expose the full public surface of the SDK so callers never
// need to import internal packages directly. They are identical to their
// decode counterparts — no conversion is needed.
// ============================================================================

// Response is the decoded result of a Cypher query execution.
// Fields holds the ordered column names; Rows holds one []any per row where
// each element is a fully decoded Go value (see DecodeValue for the type mapping).
type Response = decode.Response

// Notification is an advisory message from Neo4j about the executed statement.
// Notifications are not errors — the query succeeded and rows are returned alongside them.
type Notification = decode.Notification

// PlanOperator is one node in an EXPLAIN or PROFILE query plan tree.
type PlanOperator = decode.PlanOperator

// Node represents a Neo4j NODE value returned by a Cypher query.
type Node = decode.Node

// Relationship represents a Neo4j RELATIONSHIP value.
type Relationship = decode.Relationship

// Path represents a Neo4j PATH value — an alternating sequence of nodes and relationships.
type Path = decode.Path

// Duration represents a Neo4j DURATION value. It cannot be represented as
// time.Duration because months and days are calendar-relative.
type Duration = decode.Duration

// Point represents a Neo4j POINT value with an SRID identifying the coordinate
// reference system. Common SRIDs: 4326 (WGS-84 2D), 4979 (WGS-84 3D),
// 7203 (Cartesian 2D), 9157 (Cartesian 3D).
type Point = decode.Point

// QueryErrors is returned when Neo4j responds with one or more query errors.
// Inspect with errors.As:
//
//	var qErr *query.QueryErrors
//	if errors.As(err, &qErr) {
//	    for _, e := range qErr.Errors {
//	        log.Printf("[%s] %s", e.Title(), e.Message)
//	    }
//	}
type QueryErrors = decode.QueryErrors

// QueryError is a single Neo4j error within a QueryErrors batch.
// Use Classification(), Category(), and Title() to branch on the error code.
type QueryError = decode.QueryError
