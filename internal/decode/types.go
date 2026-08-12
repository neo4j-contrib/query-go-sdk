// Package decode handles decoding of Neo4j Query API typed JSON responses.
// It is transport-agnostic and can be used by any internal package that
// receives Query API response bodies: the query service, streaming endpoints,
// future transaction endpoints, etc.
package decode

import (
	"fmt"
	"strings"
	"time"
)

// ============================================================================
// Response types
// ============================================================================

// Response is the fully decoded top-level Query API response.
// QueryPlan is non-nil only when the Cypher statement was prefixed with
// EXPLAIN or PROFILE. Notifications is non-nil when Neo4j has advisory
// warnings or hints about the executed statement.
type Response struct {
	Fields        []string
	Rows          [][]any
	QueryPlan     *PlanOperator
	Notifications []Notification
	Bookmarks     []string
}

// Warnings returns only the notifications with severity "WARNING".
func (r *Response) Warnings() []Notification {
	out := make([]Notification, 0, len(r.Notifications))
	for _, n := range r.Notifications {
		if n.Severity == SeverityWarning {
			out = append(out, n)
		}
	}
	return out
}

// HasWarnings returns true if any WARNING severity notifications are present.
func (r *Response) HasWarnings() bool {
	for _, n := range r.Notifications {
		if n.Severity == SeverityWarning {
			return true
		}
	}
	return false
}

// ============================================================================
// Streaming response types
// ============================================================================

// StreamSummary is the execution metadata delivered in the Summary event that
// terminates a successful streamed response. QueryPlan is populated for
// EXPLAIN statements, ProfiledQueryPlan for PROFILE statements — unlike the
// buffered Response type, the streaming Summary event can carry either or
// both.
type StreamSummary struct {
	Bookmarks            []string
	Notifications        []Notification
	QueryPlan            *PlanOperator
	ProfiledQueryPlan    *PlanOperator
	QueryType            string
	ResultAvailableAfter time.Duration
	ResultConsumedAfter  time.Duration
}

// Warnings returns only the notifications with severity "WARNING".
func (s *StreamSummary) Warnings() []Notification {
	out := make([]Notification, 0, len(s.Notifications))
	for _, n := range s.Notifications {
		if n.Severity == SeverityWarning {
			out = append(out, n)
		}
	}
	return out
}

// HasWarnings returns true if any WARNING severity notifications are present.
func (s *StreamSummary) HasWarnings() bool {
	for _, n := range s.Notifications {
		if n.Severity == SeverityWarning {
			return true
		}
	}
	return false
}

// ============================================================================
// Notification types
// ============================================================================

// Severity levels returned in notifications.
const (
	SeverityWarning     = "WARNING"
	SeverityInformation = "INFORMATION"
)

// Notification categories.
const (
	CategoryUnrecognized = "UNRECOGNIZED"
	CategoryUnsupported  = "UNSUPPORTED"
	CategoryPerformance  = "PERFORMANCE"
	CategoryDeprecation  = "DEPRECATION"
	CategoryGeneric      = "GENERIC"
)

// Notification carries advisory information from Neo4j about the executed
// statement. Notifications are not errors — the query succeeded and rows
// are still returned alongside them.
type Notification struct {
	Code        string               `json:"code"`
	Description string               `json:"description"`
	Severity    string               `json:"severity"`
	Title       string               `json:"title"`
	Category    string               `json:"category"`
	Position    NotificationPosition `json:"position"`
}

// NotificationPosition is the location in the Cypher statement that
// triggered the notification, reported as offset, line, and column.
type NotificationPosition struct {
	Offset int `json:"offset"`
	Line   int `json:"line"`
	Column int `json:"column"`
}

// ============================================================================
// Query plan types
// ============================================================================

// PlanOperator is one node in the EXPLAIN or PROFILE query plan tree.
// Children form a recursive tree rooted at the outermost operator.
// Arguments values are fully decoded Neo4j typed values (string, int64,
// float64, etc.) — not raw JSON.
type PlanOperator struct {
	OperatorType string
	Arguments    map[string]any
	Identifiers  []string
	Children     []*PlanOperator
}

// ============================================================================
// Error types
// ============================================================================

// QueryError represents a single error returned by Neo4j. It implements
// the error interface so it can be used directly or wrapped.
type QueryError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error implements the error interface.
func (e QueryError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Classification returns the second segment of the Neo4j error code,
// e.g. "ClientError", "DatabaseError", "TransientError".
// TransientError codes indicate the request is safe to retry as-is.
// ClientError codes indicate the request must be fixed before retrying.
func (e QueryError) Classification() string {
	return e.codeSegment(1)
}

// Category returns the third segment of the Neo4j error code,
// e.g. "Statement", "Schema", "Procedure".
func (e QueryError) Category() string {
	return e.codeSegment(2)
}

// Title returns the fourth segment of the Neo4j error code,
// e.g. "SyntaxError", "EntityNotFound", "ConstraintValidationFailed".
func (e QueryError) Title() string {
	return e.codeSegment(3)
}

func (e QueryError) codeSegment(i int) string {
	parts := strings.SplitN(e.Code, ".", 4)
	if i >= len(parts) {
		return ""
	}
	return parts[i]
}

// QueryErrors is returned when Neo4j responds with one or more errors.
// It implements the error interface so the whole batch can be returned
// as a single Go error and inspected with errors.As.
type QueryErrors struct {
	Errors []QueryError
}

// Error implements the error interface.
func (e *QueryErrors) Error() string {
	if len(e.Errors) == 1 {
		return e.Errors[0].Error()
	}
	msgs := make([]string, len(e.Errors))
	for i, err := range e.Errors {
		msgs[i] = err.Error()
	}
	return fmt.Sprintf("%d query errors: %s", len(e.Errors), strings.Join(msgs, "; "))
}

// ============================================================================
// Graph entity types
// ============================================================================

// Node represents a Neo4j NODE value. Properties values are fully decoded
// Go types (string, int64, float64, time.Time, etc.).
type Node struct {
	ElementID  string
	Labels     []string
	Properties map[string]any
}

// Relationship represents a Neo4j RELATIONSHIP value.
type Relationship struct {
	ElementID          string
	StartNodeElementID string
	EndNodeElementID   string
	Type               string
	Properties         map[string]any
}

// Path represents a Neo4j PATH value — an alternating sequence of nodes
// and relationships. Nodes[0] is always the start node. The direction of
// each relationship is encoded in its StartNodeElementID/EndNodeElementID,
// not by position in the slice.
type Path struct {
	Nodes         []*Node
	Relationships []*Relationship
}

// ============================================================================
// Temporal types
// ============================================================================

// The following Neo4j temporal types map to time.Time:
//   Date         → time.Time (date only, no time component)
//   Time         → time.Time (zoned time)
//   LocalTime    → time.Time (no zone)
//   OffsetDateTime → time.Time (full zoned datetime, RFC3339Nano)
//   LocalDateTime  → time.Time (no zone)

// Duration represents a Neo4j DURATION value. It cannot be represented as
// time.Duration because months and days are calendar-relative and cannot
// be collapsed to a fixed nanosecond count without an anchor date.
type Duration struct {
	Months  int64
	Days    int64
	Seconds int64
	Nanos   int32
}

// String returns the ISO 8601 duration representation.
func (d Duration) String() string {
	var b strings.Builder
	b.WriteByte('P')
	if d.Months != 0 {
		months := d.Months % 12
		years := d.Months / 12
		if years != 0 {
			fmt.Fprintf(&b, "%dY", years)
		}
		if months != 0 {
			fmt.Fprintf(&b, "%dM", months)
		}
	}
	if d.Days != 0 {
		fmt.Fprintf(&b, "%dD", d.Days)
	}
	if d.Seconds != 0 || d.Nanos != 0 {
		b.WriteByte('T')
		h := d.Seconds / 3600
		m := (d.Seconds % 3600) / 60
		s := d.Seconds % 60
		if h != 0 {
			fmt.Fprintf(&b, "%dH", h)
		}
		if m != 0 {
			fmt.Fprintf(&b, "%dM", m)
		}
		if s != 0 || d.Nanos != 0 {
			if d.Nanos != 0 {
				fmt.Fprintf(&b, "%d.%09dS", s, d.Nanos)
			} else {
				fmt.Fprintf(&b, "%dS", s)
			}
		}
	}
	if b.Len() == 1 {
		b.WriteString("T0S") // PT0S — valid ISO 8601 zero duration
	}
	return b.String()
}

// ============================================================================
// Spatial types
// ============================================================================

// Point represents a Neo4j POINT value with an SRID identifying the
// coordinate reference system. Common SRIDs:
//
//	4326  WGS-84 geographic 2D  (longitude, latitude)
//	4979  WGS-84 geographic 3D  (longitude, latitude, height)
//	7203  Cartesian 2D
//	9157  Cartesian 3D
type Point struct {
	SRID int
	X    float64
	Y    float64
	Z    float64 // Only populated for 3D points; check Is3D.
	Is3D bool
}

// ============================================================================
// Vector type (Enterprise, Neo4j 2025.11+)
// ============================================================================

// CoordinateType identifies the numeric type of vector coordinates.
type CoordinateType string

const (
	CoordFloat64 CoordinateType = "FLOAT64"
	CoordFloat32 CoordinateType = "FLOAT32"
	CoordInt64   CoordinateType = "INT64"
	CoordInt32   CoordinateType = "INT32"
	CoordInt16   CoordinateType = "INT16"
	CoordInt8    CoordinateType = "INT8"
)

// Vector represents a Neo4j VECTOR value. Coordinates are kept as strings
// to preserve precision across all CoordinateType variants. Callers should
// convert based on CoordinatesType.
type Vector struct {
	CoordinatesType CoordinateType
	Coordinates     []string
}

// ============================================================================
// Fallback type
// ============================================================================

// Unsupported is returned for Neo4j types that cannot be represented in the
// Query API typed JSON format. Message contains the explanation from Neo4j.
type Unsupported struct {
	Message string
}

// ============================================================================
// Time layout constants (unexported — used only by decoder)
// ============================================================================

const (
	layoutDate          = "2006-01-02"
	layoutTime          = "15:04:05.999999999Z07:00"
	layoutLocalTime     = "15:04:05.999999999"
	layoutLocalDateTime = "2006-01-02T15:04:05.999999999"
)

// Ensure time package is used (layouts reference it indirectly via parse).
var _ = time.Time{}
