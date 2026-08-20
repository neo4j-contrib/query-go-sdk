package decode

import (
	"errors"
	"fmt"
	"github.com/goccy/go-json"
	"math"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// DecodeResponse
// ============================================================================

func TestDecodeResponse_Success(t *testing.T) {
	body := []byte(`{
		"data": {
			"fields": ["name", "age"],
			"values": [
				[{"$type": "String", "_value": "Alice"}, {"$type": "Integer", "_value": "30"}],
				[{"$type": "String", "_value": "Bob"}, {"$type": "Integer", "_value": "25"}]
			]
		},
		"bookmarks": ["neo4j:bookmark:v1:tx42"],
		"notifications": [],
		"errors": []
	}`)

	resp, err := DecodeResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := resp.Fields; len(got) != 2 || got[0] != "name" || got[1] != "age" {
		t.Errorf("fields = %v, want [name age]", got)
	}
	if len(resp.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(resp.Rows))
	}
	if resp.Rows[0][0] != "Alice" {
		t.Errorf("row0col0 = %v, want Alice", resp.Rows[0][0])
	}
	if resp.Rows[0][1] != int64(30) {
		t.Errorf("row0col1 = %v, want 30", resp.Rows[0][1])
	}
	if resp.Rows[1][0] != "Bob" {
		t.Errorf("row1col0 = %v, want Bob", resp.Rows[1][0])
	}
	if len(resp.Bookmarks) != 1 || resp.Bookmarks[0] != "neo4j:bookmark:v1:tx42" {
		t.Errorf("bookmarks = %v", resp.Bookmarks)
	}
}

func TestDecodeResponse_QueryTypeAndTimings(t *testing.T) {
	// Values mirror the Query API docs' non-streaming typed JSON example response.
	body := []byte(`{
		"data": {"fields": [], "values": []},
		"bookmarks": ["FB:kcwQ/wTfJf8rS1WY+GiIKXsCXg6Q"],
		"queryType": "rw",
		"resultAvailableAfter": 10,
		"resultConsumedAfter": 25
	}`)

	resp, err := DecodeResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.QueryType != "rw" {
		t.Errorf("queryType = %q, want rw", resp.QueryType)
	}
	if resp.ResultAvailableAfter != 10*time.Millisecond {
		t.Errorf("resultAvailableAfter = %v, want 10ms", resp.ResultAvailableAfter)
	}
	if resp.ResultConsumedAfter != 25*time.Millisecond {
		t.Errorf("resultConsumedAfter = %v, want 25ms", resp.ResultConsumedAfter)
	}
}

func TestDecodeResponse_SingleError(t *testing.T) {
	body := []byte(`{
		"errors": [{"code": "Neo.ClientError.Statement.SyntaxError", "message": "Invalid syntax"}]
	}`)

	resp, err := DecodeResponse(body)
	if resp != nil {
		t.Errorf("resp = %v, want nil", resp)
	}
	var qErr *QueryErrors
	if !errors.As(err, &qErr) {
		t.Fatalf("error = %v, want *QueryErrors", err)
	}
	if len(qErr.Errors) != 1 {
		t.Fatalf("errors = %d, want 1", len(qErr.Errors))
	}
	if qErr.Errors[0].Code != "Neo.ClientError.Statement.SyntaxError" {
		t.Errorf("code = %q", qErr.Errors[0].Code)
	}
	if qErr.Errors[0].Message != "Invalid syntax" {
		t.Errorf("message = %q", qErr.Errors[0].Message)
	}
}

func TestDecodeResponse_MultipleErrors(t *testing.T) {
	body := []byte(`{
		"errors": [
			{"code": "Neo.ClientError.Statement.SyntaxError", "message": "first"},
			{"code": "Neo.ClientError.Schema.ConstraintValidationFailed", "message": "second"}
		]
	}`)

	_, err := DecodeResponse(body)
	var qErr *QueryErrors
	if !errors.As(err, &qErr) {
		t.Fatalf("error = %v, want *QueryErrors", err)
	}
	if len(qErr.Errors) != 2 {
		t.Fatalf("errors = %d, want 2", len(qErr.Errors))
	}
}

func TestDecodeResponse_EmptyResultSet(t *testing.T) {
	body := []byte(`{
		"data": {"fields": ["n"], "values": []},
		"bookmarks": []
	}`)

	resp, err := DecodeResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Rows) != 0 {
		t.Errorf("rows = %d, want 0", len(resp.Rows))
	}
	if len(resp.Fields) != 1 || resp.Fields[0] != "n" {
		t.Errorf("fields = %v", resp.Fields)
	}
}

func TestDecodeResponse_WithNotifications(t *testing.T) {
	body := []byte(`{
		"data": {"fields": [], "values": []},
		"bookmarks": [],
		"notifications": [
			{"code": "Neo.ClientNotification.Statement.UnknownLabelWarning",
			 "severity": "WARNING", "title": "label warning",
			 "description": "desc", "category": "UNRECOGNIZED",
			 "position": {"offset": 1, "line": 2, "column": 3}}
		]
	}`)

	resp, err := DecodeResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Notifications) != 1 {
		t.Fatalf("notifications = %d, want 1", len(resp.Notifications))
	}
	n := resp.Notifications[0]
	if n.Severity != SeverityWarning {
		t.Errorf("severity = %q, want WARNING", n.Severity)
	}
	if n.Position.Line != 2 {
		t.Errorf("position.line = %d, want 2", n.Position.Line)
	}
}

func TestDecodeResponse_DecodeValueError(t *testing.T) {
	body := []byte(`{
		"data": {"fields": ["x"], "values": [[{"$type": "Bogus", "_value": 1}]]},
		"bookmarks": []
	}`)
	_, err := DecodeResponse(body)
	if err == nil {
		t.Fatal("expected error for unknown $type in row")
	}
}

func TestDecodeResponse_InvalidJSON(t *testing.T) {
	_, err := DecodeResponse([]byte(`{not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestResponse_Warnings(t *testing.T) {
	r := &Response{Notifications: []Notification{
		{Severity: SeverityWarning, Code: "W1"},
		{Severity: SeverityInformation, Code: "I1"},
		{Severity: SeverityWarning, Code: "W2"},
	}}
	w := r.Warnings()
	if len(w) != 2 {
		t.Fatalf("warnings = %d, want 2", len(w))
	}
	if w[0].Code != "W1" || w[1].Code != "W2" {
		t.Errorf("warnings = %v", w)
	}
}

func TestResponse_HasWarnings(t *testing.T) {
	with := &Response{Notifications: []Notification{
		{Severity: SeverityInformation}, {Severity: SeverityWarning},
	}}
	if !with.HasWarnings() {
		t.Error("HasWarnings() = false, want true")
	}
	without := &Response{Notifications: []Notification{
		{Severity: SeverityInformation},
	}}
	if without.HasWarnings() {
		t.Error("HasWarnings() = true, want false")
	}
	empty := &Response{}
	if empty.HasWarnings() {
		t.Error("HasWarnings() = true on empty, want false")
	}
}

// ============================================================================
// DecodeValue — one test per type
// ============================================================================

// decodeOne is a helper that decodes a single typed envelope literal.
func decodeOne(t *testing.T, raw string) any {
	t.Helper()
	v, err := DecodeValue(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("DecodeValue(%s) error: %v", raw, err)
	}
	return v
}

func TestDecodeValue_Null(t *testing.T) {
	v := decodeOne(t, `{"$type": "Null", "_value": null}`)
	if v != nil {
		t.Errorf("got %v, want nil", v)
	}
}

func TestDecodeValue_BareNull(t *testing.T) {
	v := decodeOne(t, `null`)
	if v != nil {
		t.Errorf("got %v, want nil", v)
	}
}

func TestDecodeValue_Boolean(t *testing.T) {
	if v := decodeOne(t, `{"$type": "Boolean", "_value": true}`); v != true {
		t.Errorf("got %v, want true", v)
	}
	if v := decodeOne(t, `{"$type": "Boolean", "_value": false}`); v != false {
		t.Errorf("got %v, want false", v)
	}
}

func TestDecodeValue_Integer(t *testing.T) {
	cases := map[string]int64{
		`{"$type": "Integer", "_value": "42"}`: 42,
		`{"$type": "Integer", "_value": "-7"}`: -7,
		`{"$type": "Integer", "_value": "0"}`:  0,
	}
	for raw, want := range cases {
		if v := decodeOne(t, raw); v != want {
			t.Errorf("decode %s = %v, want %d", raw, v, want)
		}
	}
}

func TestDecodeValue_Float(t *testing.T) {
	if v := decodeOne(t, `{"$type": "Float", "_value": "3.14"}`); v != 3.14 {
		t.Errorf("got %v, want 3.14", v)
	}
	if v := decodeOne(t, `{"$type": "Float", "_value": "NaN"}`); !math.IsNaN(v.(float64)) {
		t.Errorf("got %v, want NaN", v)
	}
	if v := decodeOne(t, `{"$type": "Float", "_value": "Infinity"}`); !math.IsInf(v.(float64), 1) {
		t.Errorf("got %v, want +Inf", v)
	}
	if v := decodeOne(t, `{"$type": "Float", "_value": "-Infinity"}`); !math.IsInf(v.(float64), -1) {
		t.Errorf("got %v, want -Inf", v)
	}
}

func TestDecodeValue_String(t *testing.T) {
	if v := decodeOne(t, `{"$type": "String", "_value": "hello"}`); v != "hello" {
		t.Errorf("got %v, want hello", v)
	}
}

func TestDecodeValue_Base64(t *testing.T) {
	v := decodeOne(t, `{"$type": "Base64", "_value": "aGVsbG8="}`)
	b, ok := v.([]byte)
	if !ok {
		t.Fatalf("got %T, want []byte", v)
	}
	if string(b) != "hello" {
		t.Errorf("got %q, want hello", b)
	}
}

func TestDecodeValue_ListFlat(t *testing.T) {
	v := decodeOne(t, `{"$type": "List", "_value": [{"$type": "Integer", "_value": "1"}, {"$type": "Integer", "_value": "2"}]}`)
	l, ok := v.([]any)
	if !ok {
		t.Fatalf("got %T, want []any", v)
	}
	if len(l) != 2 || l[0] != int64(1) || l[1] != int64(2) {
		t.Errorf("got %v", l)
	}
}

func TestDecodeValue_ListNested(t *testing.T) {
	v := decodeOne(t, `{"$type": "List", "_value": [{"$type": "List", "_value": [{"$type": "String", "_value": "x"}]}]}`)
	l := v.([]any)
	inner, ok := l[0].([]any)
	if !ok {
		t.Fatalf("inner got %T, want []any", l[0])
	}
	if inner[0] != "x" {
		t.Errorf("inner[0] = %v, want x", inner[0])
	}
}

func TestDecodeValue_Map(t *testing.T) {
	v := decodeOne(t, `{"$type": "Map", "_value": {"k": {"$type": "String", "_value": "v"}}}`)
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("got %T, want map", v)
	}
	if m["k"] != "v" {
		t.Errorf("m[k] = %v, want v", m["k"])
	}
}

func TestDecodeValue_Date(t *testing.T) {
	v := decodeOne(t, `{"$type": "Date", "_value": "2024-01-15"}`)
	tm := v.(time.Time)
	if tm.Year() != 2024 || tm.Month() != 1 || tm.Day() != 15 {
		t.Errorf("got %v", tm)
	}
}

func TestDecodeValue_Time(t *testing.T) {
	v := decodeOne(t, `{"$type": "Time", "_value": "14:30:00.000000000+01:00"}`)
	tm := v.(time.Time)
	if tm.Hour() != 14 || tm.Minute() != 30 {
		t.Errorf("got %v", tm)
	}
	_, offset := tm.Zone()
	if offset != 3600 {
		t.Errorf("offset = %d, want 3600", offset)
	}
}

func TestDecodeValue_LocalTime(t *testing.T) {
	v := decodeOne(t, `{"$type": "LocalTime", "_value": "14:30:00.000000000"}`)
	tm := v.(time.Time)
	if tm.Hour() != 14 || tm.Minute() != 30 {
		t.Errorf("got %v", tm)
	}
}

func TestDecodeValue_OffsetDateTime(t *testing.T) {
	v := decodeOne(t, `{"$type": "OffsetDateTime", "_value": "2024-01-15T14:30:00.000000000Z"}`)
	tm := v.(time.Time)
	if tm.Year() != 2024 || tm.Hour() != 14 {
		t.Errorf("got %v", tm)
	}
}

func TestDecodeValue_LocalDateTime(t *testing.T) {
	v := decodeOne(t, `{"$type": "LocalDateTime", "_value": "2024-01-15T14:30:00.000000000"}`)
	tm := v.(time.Time)
	if tm.Year() != 2024 || tm.Hour() != 14 {
		t.Errorf("got %v", tm)
	}
}

func TestDecodeValue_Duration(t *testing.T) {
	v := decodeOne(t, `{"$type": "Duration", "_value": "P14DT16H12M"}`)
	d := v.(Duration)
	if d.Days != 14 {
		t.Errorf("days = %d, want 14", d.Days)
	}
	if d.Seconds != 16*3600+12*60 {
		t.Errorf("seconds = %d", d.Seconds)
	}
}

func TestDecodeValue_Point2D(t *testing.T) {
	v := decodeOne(t, `{"$type": "Point", "_value": "SRID=4326;POINT (12.3 45.6)"}`)
	p := v.(Point)
	if p.SRID != 4326 || p.X != 12.3 || p.Y != 45.6 || p.Is3D {
		t.Errorf("got %+v", p)
	}
}

func TestDecodeValue_Point3D(t *testing.T) {
	v := decodeOne(t, `{"$type": "Point", "_value": "SRID=9157;POINT Z (1.0 2.0 3.0)"}`)
	p := v.(Point)
	if p.SRID != 9157 || p.X != 1.0 || p.Y != 2.0 || p.Z != 3.0 || !p.Is3D {
		t.Errorf("got %+v", p)
	}
}

func TestDecodeValue_Node(t *testing.T) {
	v := decodeOne(t, `{"$type": "Node", "_value": {"_element_id": "4:abc:0", "_labels": ["Person"], "_properties": {"name": {"$type": "String", "_value": "Alice"}}}}`)
	n, ok := v.(*Node)
	if !ok {
		t.Fatalf("got %T, want *Node", v)
	}
	if n.ElementID != "4:abc:0" || len(n.Labels) != 1 || n.Labels[0] != "Person" {
		t.Errorf("got %+v", n)
	}
	if n.Properties["name"] != "Alice" {
		t.Errorf("props = %v", n.Properties)
	}
}

func TestDecodeValue_Relationship(t *testing.T) {
	v := decodeOne(t, `{"$type": "Relationship", "_value": {"_element_id": "5:abc:0", "_start_node_element_id": "4:abc:0", "_end_node_element_id": "4:abc:1", "_type": "KNOWS", "_properties": {"since": {"$type": "Integer", "_value": "2020"}}}}`)
	rel, ok := v.(*Relationship)
	if !ok {
		t.Fatalf("got %T, want *Relationship", v)
	}
	if rel.Type != "KNOWS" || rel.StartNodeElementID != "4:abc:0" || rel.EndNodeElementID != "4:abc:1" {
		t.Errorf("got %+v", rel)
	}
	if rel.Properties["since"] != int64(2020) {
		t.Errorf("props = %v", rel.Properties)
	}
}

func TestDecodeValue_Path(t *testing.T) {
	v := decodeOne(t, `{"$type": "Path", "_value": [
		{"$type": "Node", "_value": {"_element_id": "4:abc:0", "_labels": ["A"], "_properties": {}}},
		{"$type": "Relationship", "_value": {"_element_id": "5:abc:0", "_start_node_element_id": "4:abc:0", "_end_node_element_id": "4:abc:1", "_type": "R", "_properties": {}}},
		{"$type": "Node", "_value": {"_element_id": "4:abc:1", "_labels": ["B"], "_properties": {}}}
	]}`)
	p, ok := v.(Path)
	if !ok {
		t.Fatalf("got %T, want Path", v)
	}
	if len(p.Nodes) != 2 || len(p.Relationships) != 1 {
		t.Errorf("nodes=%d rels=%d", len(p.Nodes), len(p.Relationships))
	}
	if p.Nodes[0].ElementID != "4:abc:0" || p.Relationships[0].Type != "R" {
		t.Errorf("got %+v", p)
	}
}

func TestDecodeValue_Vector(t *testing.T) {
	v := decodeOne(t, `{"$type": "Vector", "_value": {"coordinatesType": "FLOAT64", "coordinates": ["1.0", "2.0"]}}`)
	vec, ok := v.(Vector)
	if !ok {
		t.Fatalf("got %T, want Vector", v)
	}
	if vec.CoordinatesType != CoordFloat64 {
		t.Errorf("type = %q", vec.CoordinatesType)
	}
	if len(vec.Coordinates) != 2 || vec.Coordinates[0] != "1.0" {
		t.Errorf("coords = %v", vec.Coordinates)
	}
}

func TestDecodeValue_Unsupported(t *testing.T) {
	v := decodeOne(t, `{"$type": "Unsupported", "_value": "some message"}`)
	u, ok := v.(Unsupported)
	if !ok {
		t.Fatalf("got %T, want Unsupported", v)
	}
	if u.Message != "some message" {
		t.Errorf("message = %q", u.Message)
	}
}

func TestDecodeValue_UnknownType(t *testing.T) {
	_, err := DecodeValue(json.RawMessage(`{"$type": "Bogus", "_value": 1}`))
	if err == nil {
		t.Fatal("expected error for unknown $type")
	}
}

func TestDecodeValue_PlainJSON(t *testing.T) {
	// Not a typed envelope — falls back to plain JSON scalar.
	v, err := DecodeValue(json.RawMessage(`"plain string"`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "plain string" {
		t.Errorf("got %v, want plain string", v)
	}
	// Plain JSON number falls back too.
	v2, err := DecodeValue(json.RawMessage(`123`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v2 != float64(123) {
		t.Errorf("got %v (%T), want 123", v2, v2)
	}
}

func TestDecodeValue_InvalidJSON(t *testing.T) {
	_, err := DecodeValue(json.RawMessage(`{bad`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ============================================================================
// Benchmarks
// ============================================================================

// benchmarkPayload builds a wireResponse JSON payload with the given number of
// rows and columns. Every cell is a typed String envelope:
//
//	{"$type":"String","_value":"hello"}
func benchmarkPayload(rows, cols int) []byte {
	cell := `{"$type":"String","_value":"hello"}`

	// Build the fields array: ["col0","col1",...]
	fields := make([]string, cols)
	for i := range fields {
		fields[i] = fmt.Sprintf(`"col%d"`, i)
	}

	// Build one row: [cell, cell, ...]
	rowCells := make([]string, cols)
	for i := range rowCells {
		rowCells[i] = cell
	}
	oneRow := "[" + strings.Join(rowCells, ",") + "]"

	// Build all rows.
	allRows := make([]string, rows)
	for i := range allRows {
		allRows[i] = oneRow
	}

	payload := fmt.Sprintf(
		`{"data":{"fields":[%s],"values":[%s]},"bookmarks":[],"notifications":[],"errors":[]}`,
		strings.Join(fields, ","),
		strings.Join(allRows, ","),
	)
	return []byte(payload)
}

// BenchmarkDecodeResponse measures DecodeResponse on a 1000-row × 10-column
// wireResponse payload where every cell is a typed String envelope.
// Run with -benchmem to capture B/op and allocs/op alongside ns/op.
func BenchmarkDecodeResponse(b *testing.B) {
	payload := benchmarkPayload(1000, 10)
	b.ReportAllocs()
	for b.Loop() {
		_, err := DecodeResponse(payload)
		if err != nil {
			b.Fatalf("DecodeResponse error: %v", err)
		}
	}
}

// ============================================================================
// parseDuration
// ============================================================================

func TestParseDuration(t *testing.T) {
	tests := []struct {
		in           string
		months, days int64
		seconds      int64
		nanos        int32
	}{
		{"P14DT16H12M", 0, 14, 16*3600 + 12*60, 0},
		{"P1Y2M3DT4H5M6.789S", 14, 3, 4*3600 + 5*60 + 6, 789000000},
		{"P2W", 0, 14, 0, 0},
	}
	for _, tt := range tests {
		d, err := parseDuration(tt.in)
		if err != nil {
			t.Errorf("parseDuration(%q) error: %v", tt.in, err)
			continue
		}
		if d.Months != tt.months || d.Days != tt.days || d.Seconds != tt.seconds || d.Nanos != tt.nanos {
			t.Errorf("parseDuration(%q) = %+v, want months=%d days=%d seconds=%d nanos=%d",
				tt.in, d, tt.months, tt.days, tt.seconds, tt.nanos)
		}
	}
}

func TestParseDuration_ZeroViaString(t *testing.T) {
	// Zero Duration renders as "PT0S" — a valid ISO 8601 zero duration.
	var zero Duration
	if got := zero.String(); got != "PT0S" {
		t.Errorf("zero.String() = %q, want PT0S", got)
	}
	// Verify the roundtrip: PT0S must parse back to a zero Duration.
	parsed, err := parseDuration("PT0S")
	if err != nil {
		t.Fatalf("parseDuration(PT0S) unexpected error: %v", err)
	}
	if parsed != zero {
		t.Errorf("parseDuration(PT0S) = %+v, want zero Duration", parsed)
	}
}

func TestParseDuration_Invalid(t *testing.T) {
	cases := []string{
		"14DT16H", // missing P prefix
		"PXY",     // non-numeric
		"",        // empty
		"P12",     // number with no unit at end
	}
	for _, c := range cases {
		if _, err := parseDuration(c); err == nil {
			t.Errorf("parseDuration(%q) expected error, got nil", c)
		}
	}
}

// ============================================================================
// parsePoint
// ============================================================================

func TestParsePoint_2D(t *testing.T) {
	p, err := parsePoint("SRID=4326;POINT (12.3 45.6)")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if p.SRID != 4326 || p.X != 12.3 || p.Y != 45.6 || p.Is3D {
		t.Errorf("got %+v", p)
	}
}

func TestParsePoint_3D(t *testing.T) {
	p, err := parsePoint("SRID=9157;POINT Z (1.0 2.0 3.0)")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if p.SRID != 9157 || p.X != 1.0 || p.Y != 2.0 || p.Z != 3.0 || !p.Is3D {
		t.Errorf("got %+v", p)
	}
}

func TestParsePoint_Invalid(t *testing.T) {
	cases := []string{
		"POINT (1 2)",           // no SRID prefix / no semicolon
		"SRID=abc;POINT (1 2)",  // bad SRID
		"SRID=4326;POINT (1)",   // too few coords
		"SRID=4326;POINT (a b)", // non-numeric coords
	}
	for _, c := range cases {
		if _, err := parsePoint(c); err == nil {
			t.Errorf("parsePoint(%q) expected error, got nil", c)
		}
	}
}
