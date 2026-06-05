package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/neo4j-contrib/query-go-sdk/internal/decode"
)

// ============================================================================
// EagerResult
// ============================================================================

// EagerResult holds the fully materialised result of a query.
// Mirrors neo4j.EagerResult in the Go driver.
type EagerResult struct {
	Keys          []string
	Records       []*Record
	Notifications []decode.Notification
	QueryPlan     *decode.PlanOperator
	Bookmarks     []string
}

// HasWarnings returns true if any WARNING severity notifications are present.
func (e *EagerResult) HasWarnings() bool {
	for _, n := range e.Notifications {
		if n.Severity == decode.SeverityWarning {
			return true
		}
	}
	return false
}

// Warnings returns only the notifications with severity "WARNING".
func (e *EagerResult) Warnings() []decode.Notification {
	out := make([]decode.Notification, 0, len(e.Notifications))
	for _, n := range e.Notifications {
		if n.Severity == decode.SeverityWarning {
			out = append(out, n)
		}
	}
	return out
}

// String returns a human-readable summary of the result, useful for debugging.
func (e *EagerResult) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Keys: %v\n", e.Keys)
	fmt.Fprintf(&b, "Records: %d\n", len(e.Records))
	for i, rec := range e.Records {
		fmt.Fprintf(&b, "  [%d] %s\n", i, rec.String())
	}
	if len(e.Notifications) > 0 {
		fmt.Fprintf(&b, "Notifications: %d\n", len(e.Notifications))
		for _, n := range e.Notifications {
			fmt.Fprintf(&b, "  [%s] %s: %s\n", n.Severity, n.Code, n.Title)
		}
	}
	if e.QueryPlan != nil {
		fmt.Fprintf(&b, "QueryPlan: %s\n", e.QueryPlan.OperatorType)
	}
	fmt.Fprintf(&b, "Bookmarks: %v\n", e.Bookmarks)
	return b.String()
}

// ============================================================================
// ResultTransformer
// ============================================================================

// ResultTransformer converts a raw *decode.Response into a typed result T.
// Mirrors the transformer pattern in the Neo4j Go driver:
//
//	neo4j.ExecuteQuery(ctx, driver, cypher, params, neo4j.EagerResultTransformer, ...)
//
// Usage with this SDK:
//
//	result, err := query.WithTransformer(svc, ctx, cypher, params, query.EagerResultTransformer)
type ResultTransformer[T any] func(*decode.Response) (T, error)

// EagerResultTransformer collects all rows into an EagerResult.
// Use for typical queries where the full result set fits comfortably in memory.
//
//	result, err := query.WithTransformer(svc, ctx, cypher, params, query.EagerResultTransformer)
//	for _, rec := range result.Records {
//	    name, _ := rec.GetString("p.name")
//	}
var EagerResultTransformer ResultTransformer[*EagerResult] = func(resp *decode.Response) (*EagerResult, error) {
	records := make([]*Record, len(resp.Rows))
	for i, row := range resp.Rows {
		records[i] = newRecord(resp.Fields, row)
	}
	return &EagerResult{
		Keys:          resp.Fields,
		Records:       records,
		Notifications: resp.Notifications,
		QueryPlan:     resp.QueryPlan,
		Bookmarks:     resp.Bookmarks,
	}, nil
}

// Collect returns a ResultTransformer that maps each Record to a value of
// type T using fn, collecting the results into []T.
//
//	type Actor struct {
//	    Name  string
//	    Roles []string
//	}
//
//	actors, err := query.WithTransformer(svc, ctx, cypher, params,
//	    query.Collect(func(rec *query.Record) (Actor, error) {
//	        name, _  := rec.GetString("p.name")
//	        raw, _   := rec.GetList("a.roles")
//	        roles, _ := query.StringList(raw)
//	        return Actor{Name: name, Roles: roles}, nil
//	    }),
//	)
func Collect[T any](fn func(*Record) (T, error)) ResultTransformer[[]T] {
	return func(resp *decode.Response) ([]T, error) {
		out := make([]T, 0, len(resp.Rows))
		for i, row := range resp.Rows {
			rec := newRecord(resp.Fields, row)
			v, err := fn(rec)
			if err != nil {
				return nil, fmt.Errorf("collect row %d: %w", i, err)
			}
			out = append(out, v)
		}
		return out, nil
	}
}

// Single returns a ResultTransformer that expects exactly one row and maps
// it to T using fn. Returns an error if the result has zero or more than one row.
//
//	movie, err := query.WithTransformer(svc, ctx, cypher, params,
//	    query.Single(func(rec *query.Record) (Movie, error) {
//	        title, _    := rec.GetString("m.title")
//	        released, _ := rec.GetInt64("m.released")
//	        return Movie{Title: title, Released: int(released)}, nil
//	    }),
//	)
func Single[T any](fn func(*Record) (T, error)) ResultTransformer[T] {
	return func(resp *decode.Response) (T, error) {
		var zero T
		switch len(resp.Rows) {
		case 0:
			return zero, fmt.Errorf("single: expected 1 row, got 0")
		case 1:
			return fn(newRecord(resp.Fields, resp.Rows[0]))
		default:
			return zero, fmt.Errorf("single: expected 1 row, got %d", len(resp.Rows))
		}
	}
}

// ============================================================================
// WithTransformer — package-level generic function
// ============================================================================

// WithTransformer executes a Cypher statement via svc and applies transformer
// to the result. It is a package-level generic function rather than a method
// on queryService because Go interfaces cannot have generic methods.
//
// This mirrors the pattern used by the Neo4j Go driver, where neo4j.ExecuteQuery
// is a package-level generic function that accepts the Driver interface:
//
//	// Driver pattern:
//	result, err := neo4j.ExecuteQuery(ctx, driver, cypher, params,
//	    neo4j.EagerResultTransformer, ...)
//
//	// This SDK — same shape:
//	result, err := query.WithTransformer(svc, ctx, cypher, params,
//	    query.EagerResultTransformer)
//
// Because it accepts QueryService (the interface), it works against any
// implementation including test doubles.
func WithTransformer[T any](svc QueryService, ctx context.Context, cypher string, params map[string]any, transformer ResultTransformer[T]) (T, error) {
	var zero T

	resp, err := svc.Execute(ctx, cypher, params)
	if err != nil {
		return zero, err
	}

	result, err := transformer(resp)
	if err != nil {
		return zero, fmt.Errorf("query: transform result: %w", err)
	}

	return result, nil
}
