package query

import (
	"testing"
	"time"

	"github.com/neo4j-contrib/query-go-sdk/internal/decode"
)

// ============================================================================
// Typed accessors
// ============================================================================

func TestRecord_GetString(t *testing.T) {
	r := newRecord([]string{"s", "n", "null"}, []any{"hello", int64(1), nil})
	if v, ok := r.GetString("s"); !ok || v != "hello" {
		t.Errorf("present: got (%q, %v), want (hello, true)", v, ok)
	}
	if v, ok := r.GetString("missing"); ok || v != "" {
		t.Errorf("missing: got (%q, %v), want (\"\", false)", v, ok)
	}
	if v, ok := r.GetString("null"); ok || v != "" {
		t.Errorf("null: got (%q, %v), want (\"\", false)", v, ok)
	}
	if v, ok := r.GetString("n"); ok || v != "" {
		t.Errorf("wrong type: got (%q, %v), want (\"\", false)", v, ok)
	}
}

func TestRecord_GetInt64(t *testing.T) {
	r := newRecord([]string{"n", "s", "null"}, []any{int64(42), "x", nil})
	if v, ok := r.GetInt64("n"); !ok || v != 42 {
		t.Errorf("present: got (%d, %v)", v, ok)
	}
	if v, ok := r.GetInt64("missing"); ok || v != 0 {
		t.Errorf("missing: got (%d, %v)", v, ok)
	}
	if v, ok := r.GetInt64("null"); ok || v != 0 {
		t.Errorf("null: got (%d, %v)", v, ok)
	}
	if v, ok := r.GetInt64("s"); ok || v != 0 {
		t.Errorf("wrong type: got (%d, %v)", v, ok)
	}
}

func TestRecord_GetFloat64(t *testing.T) {
	r := newRecord([]string{"f", "s", "null"}, []any{3.14, "x", nil})
	if v, ok := r.GetFloat64("f"); !ok || v != 3.14 {
		t.Errorf("present: got (%v, %v)", v, ok)
	}
	if v, ok := r.GetFloat64("missing"); ok || v != 0 {
		t.Errorf("missing: got (%v, %v)", v, ok)
	}
	if v, ok := r.GetFloat64("null"); ok || v != 0 {
		t.Errorf("null: got (%v, %v)", v, ok)
	}
	if v, ok := r.GetFloat64("s"); ok || v != 0 {
		t.Errorf("wrong type: got (%v, %v)", v, ok)
	}
}

func TestRecord_GetBool(t *testing.T) {
	r := newRecord([]string{"b", "s", "null"}, []any{true, "x", nil})
	if v, ok := r.GetBool("b"); !ok || v != true {
		t.Errorf("present: got (%v, %v)", v, ok)
	}
	if v, ok := r.GetBool("missing"); ok || v != false {
		t.Errorf("missing: got (%v, %v)", v, ok)
	}
	if v, ok := r.GetBool("null"); ok || v != false {
		t.Errorf("null: got (%v, %v)", v, ok)
	}
	if v, ok := r.GetBool("s"); ok || v != false {
		t.Errorf("wrong type: got (%v, %v)", v, ok)
	}
}

func TestRecord_GetBytes(t *testing.T) {
	r := newRecord([]string{"b", "s", "null"}, []any{[]byte("hi"), "x", nil})
	if v, ok := r.GetBytes("b"); !ok || string(v) != "hi" {
		t.Errorf("present: got (%q, %v)", v, ok)
	}
	if v, ok := r.GetBytes("missing"); ok || v != nil {
		t.Errorf("missing: got (%q, %v)", v, ok)
	}
	if v, ok := r.GetBytes("null"); ok || v != nil {
		t.Errorf("null: got (%q, %v)", v, ok)
	}
	if v, ok := r.GetBytes("s"); ok || v != nil {
		t.Errorf("wrong type: got (%q, %v)", v, ok)
	}
}

func TestRecord_GetTime(t *testing.T) {
	now := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	r := newRecord([]string{"t", "s", "null"}, []any{now, "x", nil})
	if v, ok := r.GetTime("t"); !ok || !v.Equal(now) {
		t.Errorf("present: got (%v, %v)", v, ok)
	}
	if v, ok := r.GetTime("missing"); ok || !v.IsZero() {
		t.Errorf("missing: got (%v, %v)", v, ok)
	}
	if v, ok := r.GetTime("null"); ok || !v.IsZero() {
		t.Errorf("null: got (%v, %v)", v, ok)
	}
	if v, ok := r.GetTime("s"); ok || !v.IsZero() {
		t.Errorf("wrong type: got (%v, %v)", v, ok)
	}
}

func TestRecord_GetDuration(t *testing.T) {
	d := decode.Duration{Days: 5}
	r := newRecord([]string{"d", "s", "null"}, []any{d, "x", nil})
	if v, ok := r.GetDuration("d"); !ok || v.Days != 5 {
		t.Errorf("present: got (%+v, %v)", v, ok)
	}
	if v, ok := r.GetDuration("missing"); ok || v != (decode.Duration{}) {
		t.Errorf("missing: got (%+v, %v)", v, ok)
	}
	if v, ok := r.GetDuration("null"); ok || v != (decode.Duration{}) {
		t.Errorf("null: got (%+v, %v)", v, ok)
	}
	if v, ok := r.GetDuration("s"); ok || v != (decode.Duration{}) {
		t.Errorf("wrong type: got (%+v, %v)", v, ok)
	}
}

func TestRecord_GetPoint(t *testing.T) {
	p := decode.Point{SRID: 4326, X: 1, Y: 2}
	r := newRecord([]string{"p", "s", "null"}, []any{p, "x", nil})
	if v, ok := r.GetPoint("p"); !ok || v.SRID != 4326 {
		t.Errorf("present: got (%+v, %v)", v, ok)
	}
	if v, ok := r.GetPoint("missing"); ok || v != (decode.Point{}) {
		t.Errorf("missing: got (%+v, %v)", v, ok)
	}
	if v, ok := r.GetPoint("null"); ok || v != (decode.Point{}) {
		t.Errorf("null: got (%+v, %v)", v, ok)
	}
	if v, ok := r.GetPoint("s"); ok || v != (decode.Point{}) {
		t.Errorf("wrong type: got (%+v, %v)", v, ok)
	}
}

func TestRecord_GetNode(t *testing.T) {
	n := &decode.Node{ElementID: "4:x:0"}
	r := newRecord([]string{"n", "s", "null"}, []any{n, "x", nil})
	if v, ok := r.GetNode("n"); !ok || v.ElementID != "4:x:0" {
		t.Errorf("present: got (%+v, %v)", v, ok)
	}
	if v, ok := r.GetNode("missing"); ok || v != nil {
		t.Errorf("missing: got (%+v, %v)", v, ok)
	}
	if v, ok := r.GetNode("null"); ok || v != nil {
		t.Errorf("null: got (%+v, %v)", v, ok)
	}
	if v, ok := r.GetNode("s"); ok || v != nil {
		t.Errorf("wrong type: got (%+v, %v)", v, ok)
	}
}

func TestRecord_GetRelationship(t *testing.T) {
	rel := &decode.Relationship{ElementID: "5:x:0", Type: "KNOWS"}
	r := newRecord([]string{"r", "s", "null"}, []any{rel, "x", nil})
	if v, ok := r.GetRelationship("r"); !ok || v.Type != "KNOWS" {
		t.Errorf("present: got (%+v, %v)", v, ok)
	}
	if v, ok := r.GetRelationship("missing"); ok || v != nil {
		t.Errorf("missing: got (%+v, %v)", v, ok)
	}
	if v, ok := r.GetRelationship("null"); ok || v != nil {
		t.Errorf("null: got (%+v, %v)", v, ok)
	}
	if v, ok := r.GetRelationship("s"); ok || v != nil {
		t.Errorf("wrong type: got (%+v, %v)", v, ok)
	}
}

func TestRecord_GetPath(t *testing.T) {
	p := decode.Path{Nodes: []*decode.Node{{ElementID: "4:x:0"}}}
	r := newRecord([]string{"p", "s", "null"}, []any{p, "x", nil})
	if v, ok := r.GetPath("p"); !ok || len(v.Nodes) != 1 {
		t.Errorf("present: got (%+v, %v)", v, ok)
	}
	if v, ok := r.GetPath("missing"); ok || v.Nodes != nil {
		t.Errorf("missing: got (%+v, %v)", v, ok)
	}
	if v, ok := r.GetPath("null"); ok || v.Nodes != nil {
		t.Errorf("null: got (%+v, %v)", v, ok)
	}
	if v, ok := r.GetPath("s"); ok || v.Nodes != nil {
		t.Errorf("wrong type: got (%+v, %v)", v, ok)
	}
}

func TestRecord_GetList(t *testing.T) {
	r := newRecord([]string{"l", "s", "null"}, []any{[]any{int64(1)}, "x", nil})
	if v, ok := r.GetList("l"); !ok || len(v) != 1 {
		t.Errorf("present: got (%v, %v)", v, ok)
	}
	if v, ok := r.GetList("missing"); ok || v != nil {
		t.Errorf("missing: got (%v, %v)", v, ok)
	}
	if v, ok := r.GetList("null"); ok || v != nil {
		t.Errorf("null: got (%v, %v)", v, ok)
	}
	if v, ok := r.GetList("s"); ok || v != nil {
		t.Errorf("wrong type: got (%v, %v)", v, ok)
	}
}

func TestRecord_GetMap(t *testing.T) {
	r := newRecord([]string{"m", "s", "null"}, []any{map[string]any{"k": "v"}, "x", nil})
	if v, ok := r.GetMap("m"); !ok || v["k"] != "v" {
		t.Errorf("present: got (%v, %v)", v, ok)
	}
	if v, ok := r.GetMap("missing"); ok || v != nil {
		t.Errorf("missing: got (%v, %v)", v, ok)
	}
	if v, ok := r.GetMap("null"); ok || v != nil {
		t.Errorf("null: got (%v, %v)", v, ok)
	}
	if v, ok := r.GetMap("s"); ok || v != nil {
		t.Errorf("wrong type: got (%v, %v)", v, ok)
	}
}

// ============================================================================
// Other Record methods
// ============================================================================

func TestRecord_Get(t *testing.T) {
	r := newRecord([]string{"a", "b"}, []any{int64(1), nil})
	if v, ok := r.Get("a"); !ok || v != int64(1) {
		t.Errorf("present: got (%v, %v)", v, ok)
	}
	if v, ok := r.Get("missing"); ok || v != nil {
		t.Errorf("absent: got (%v, %v)", v, ok)
	}
	// Present-but-null returns (nil, true).
	if v, ok := r.Get("b"); !ok || v != nil {
		t.Errorf("null: got (%v, %v), want (nil, true)", v, ok)
	}
}

func TestRecord_AsMap(t *testing.T) {
	r := newRecord([]string{"a", "b"}, []any{int64(1), "two"})
	m := r.AsMap()
	if len(m) != 2 || m["a"] != int64(1) || m["b"] != "two" {
		t.Errorf("AsMap() = %v", m)
	}
}

func TestRecord_Keys_IsCopy(t *testing.T) {
	r := newRecord([]string{"a", "b"}, []any{1, 2})
	k := r.Keys()
	if len(k) != 2 || k[0] != "a" || k[1] != "b" {
		t.Fatalf("Keys() = %v", k)
	}
	k[0] = "mutated"
	if r.keys[0] != "a" {
		t.Errorf("Keys() did not return a copy; internal keys mutated to %v", r.keys)
	}
}

func TestRecord_Values_IsCopy(t *testing.T) {
	r := newRecord([]string{"a", "b"}, []any{int64(1), int64(2)})
	v := r.Values()
	if len(v) != 2 || v[0] != int64(1) || v[1] != int64(2) {
		t.Fatalf("Values() = %v", v)
	}
	v[0] = "mutated"
	if r.values[0] != int64(1) {
		t.Errorf("Values() did not return a copy; internal values mutated to %v", r.values)
	}
}

func TestRecord_IsNull(t *testing.T) {
	r := newRecord([]string{"null", "set"}, []any{nil, "x"})
	if !r.IsNull("null") {
		t.Error("IsNull(null) = false, want true")
	}
	if r.IsNull("missing") {
		t.Error("IsNull(missing) = true, want false")
	}
	if r.IsNull("set") {
		t.Error("IsNull(set) = true, want false")
	}
}

func TestRecord_String(t *testing.T) {
	r := newRecord([]string{"a"}, []any{int64(1)})
	s := r.String()
	if s == "" {
		t.Error("String() returned empty")
	}
}

// ============================================================================
// List helpers
// ============================================================================

func TestStringList(t *testing.T) {
	out, ok := StringList([]any{"a", "b"})
	if !ok || len(out) != 2 || out[0] != "a" || out[1] != "b" {
		t.Errorf("success: got (%v, %v)", out, ok)
	}
	if out, ok := StringList([]any{"a", int64(1)}); ok || out != nil {
		t.Errorf("non-string element: got (%v, %v)", out, ok)
	}
}

func TestInt64List(t *testing.T) {
	out, ok := Int64List([]any{int64(1), int64(2)})
	if !ok || len(out) != 2 || out[0] != 1 || out[1] != 2 {
		t.Errorf("success: got (%v, %v)", out, ok)
	}
	if out, ok := Int64List([]any{int64(1), "x"}); ok || out != nil {
		t.Errorf("non-int64 element: got (%v, %v)", out, ok)
	}
}

func TestFloat64List(t *testing.T) {
	out, ok := Float64List([]any{1.0, 2.0})
	if !ok || len(out) != 2 || out[0] != 1.0 || out[1] != 2.0 {
		t.Errorf("success: got (%v, %v)", out, ok)
	}
	if out, ok := Float64List([]any{1.0, "x"}); ok || out != nil {
		t.Errorf("non-float64 element: got (%v, %v)", out, ok)
	}
}
