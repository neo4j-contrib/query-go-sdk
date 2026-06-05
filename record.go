package query

import (
	"fmt"
	"strings"
	"time"

	"github.com/neo4j-contrib/query-go-sdk/internal/decode"
)

// Record represents a single row in a query result with named field access.
// It mirrors neo4j.Record in the Go driver for a consistent developer experience.
type Record struct {
	keys   []string
	values []any
}

func newRecord(keys []string, values []any) *Record {
	return &Record{keys: keys, values: values}
}

// Get returns the value for the named field and whether the field exists.
// A field that is present but Null returns (nil, true).
// The returned value is one of the types documented on decode.DecodeValue.
func (r *Record) Get(field string) (any, bool) {
	for i, k := range r.keys {
		if k == field {
			if i < len(r.values) {
				return r.values[i], true
			}
			return nil, false
		}
	}
	return nil, false
}

// AsMap returns all fields as a map. Mirrors neo4j.Record.AsMap().
func (r *Record) AsMap() map[string]any {
	m := make(map[string]any, len(r.keys))
	for i, k := range r.keys {
		if i < len(r.values) {
			m[k] = r.values[i]
		}
	}
	return m
}

// Keys returns the ordered list of field names.
func (r *Record) Keys() []string {
	out := make([]string, len(r.keys))
	copy(out, r.keys)
	return out
}

// Values returns the ordered list of decoded values.
func (r *Record) Values() []any {
	out := make([]any, len(r.values))
	copy(out, r.values)
	return out
}

// IsNull reports whether the named field exists and has a Null value.
// Use this to distinguish "field is null" from "field not in result".
func (r *Record) IsNull(field string) bool {
	v, ok := r.Get(field)
	return ok && v == nil
}

// String returns a human-readable representation, useful for debugging.
func (r *Record) String() string {
	var b strings.Builder
	b.WriteString("{")
	for i, k := range r.keys {
		if i > 0 {
			b.WriteString(", ")
		}
		var v any
		if i < len(r.values) {
			v = r.values[i]
		}
		fmt.Fprintf(&b, "%s: %s", k, formatValue(v))
	}
	b.WriteString("}")
	return b.String()
}

// ============================================================================
// Typed accessors
// ============================================================================

func (r *Record) GetString(field string) (string, bool) {
	v, ok := r.Get(field)
	if !ok || v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func (r *Record) GetInt64(field string) (int64, bool) {
	v, ok := r.Get(field)
	if !ok || v == nil {
		return 0, false
	}
	n, ok := v.(int64)
	return n, ok
}

func (r *Record) GetFloat64(field string) (float64, bool) {
	v, ok := r.Get(field)
	if !ok || v == nil {
		return 0, false
	}
	f, ok := v.(float64)
	return f, ok
}

func (r *Record) GetBool(field string) (bool, bool) {
	v, ok := r.Get(field)
	if !ok || v == nil {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

func (r *Record) GetBytes(field string) ([]byte, bool) {
	v, ok := r.Get(field)
	if !ok || v == nil {
		return nil, false
	}
	b, ok := v.([]byte)
	return b, ok
}

func (r *Record) GetTime(field string) (time.Time, bool) {
	v, ok := r.Get(field)
	if !ok || v == nil {
		return time.Time{}, false
	}
	t, ok := v.(time.Time)
	return t, ok
}

func (r *Record) GetDuration(field string) (decode.Duration, bool) {
	v, ok := r.Get(field)
	if !ok || v == nil {
		return decode.Duration{}, false
	}
	d, ok := v.(decode.Duration)
	return d, ok
}

func (r *Record) GetPoint(field string) (decode.Point, bool) {
	v, ok := r.Get(field)
	if !ok || v == nil {
		return decode.Point{}, false
	}
	p, ok := v.(decode.Point)
	return p, ok
}

func (r *Record) GetNode(field string) (*decode.Node, bool) {
	v, ok := r.Get(field)
	if !ok || v == nil {
		return nil, false
	}
	n, ok := v.(*decode.Node)
	return n, ok
}

func (r *Record) GetRelationship(field string) (*decode.Relationship, bool) {
	v, ok := r.Get(field)
	if !ok || v == nil {
		return nil, false
	}
	rel, ok := v.(*decode.Relationship)
	return rel, ok
}

func (r *Record) GetPath(field string) (decode.Path, bool) {
	v, ok := r.Get(field)
	if !ok || v == nil {
		return decode.Path{}, false
	}
	p, ok := v.(decode.Path)
	return p, ok
}

func (r *Record) GetList(field string) ([]any, bool) {
	v, ok := r.Get(field)
	if !ok || v == nil {
		return nil, false
	}
	l, ok := v.([]any)
	return l, ok
}

func (r *Record) GetMap(field string) (map[string]any, bool) {
	v, ok := r.Get(field)
	if !ok || v == nil {
		return nil, false
	}
	m, ok := v.(map[string]any)
	return m, ok
}

// ============================================================================
// List element helpers
// ============================================================================

// StringList converts a []any list to []string.
// Returns nil, false if any element is not a string.
func StringList(list []any) ([]string, bool) {
	out := make([]string, len(list))
	for i, v := range list {
		s, ok := v.(string)
		if !ok {
			return nil, false
		}
		out[i] = s
	}
	return out, true
}

// Int64List converts a []any list to []int64.
// Returns nil, false if any element is not an int64.
func Int64List(list []any) ([]int64, bool) {
	out := make([]int64, len(list))
	for i, v := range list {
		n, ok := v.(int64)
		if !ok {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}

// Float64List converts a []any list to []float64.
// Returns nil, false if any element is not a float64.
func Float64List(list []any) ([]float64, bool) {
	out := make([]float64, len(list))
	for i, v := range list {
		f, ok := v.(float64)
		if !ok {
			return nil, false
		}
		out[i] = f
	}
	return out, true
}

// ============================================================================
// formatValue — shared by Record.String() and EagerResult.String()
// ============================================================================

func formatValue(v any) string {
	if v == nil {
		return "<null>"
	}
	switch t := v.(type) {
	case *decode.Node:
		return fmt.Sprintf("Node{id:%s labels:%v props:%v}", t.ElementID, t.Labels, t.Properties)
	case *decode.Relationship:
		return fmt.Sprintf("Rel{id:%s type:%s props:%v}", t.ElementID, t.Type, t.Properties)
	case decode.Path:
		return fmt.Sprintf("Path{nodes:%d rels:%d}", len(t.Nodes), len(t.Relationships))
	case []any:
		parts := make([]string, len(t))
		for i, elem := range t {
			parts[i] = formatValue(elem)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		parts := make([]string, 0, len(t))
		for k, val := range t {
			parts = append(parts, k+"="+formatValue(val))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return fmt.Sprintf("%v", v)
	}
}
