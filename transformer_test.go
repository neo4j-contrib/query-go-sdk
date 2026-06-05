package query

import (
	"errors"
	"strings"
	"testing"

	"github.com/neo4j-contrib/query-go-sdk/internal/decode"
)

// ============================================================================
// Single transformer
// ============================================================================

func TestSingle_OneRow(t *testing.T) {
	resp := &decode.Response{
		Fields: []string{"name"},
		Rows:   [][]any{{"Alice"}},
	}
	transform := Single(func(rec *Record) (string, error) {
		s, _ := rec.GetString("name")
		return s, nil
	})
	got, err := transform(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Alice" {
		t.Errorf("got %q, want Alice", got)
	}
}

func TestSingle_ZeroRows(t *testing.T) {
	resp := &decode.Response{Fields: []string{"name"}, Rows: [][]any{}}
	transform := Single(func(rec *Record) (string, error) { return "", nil })
	_, err := transform(resp)
	if err == nil {
		t.Fatal("expected error for 0 rows")
	}
	if !strings.Contains(err.Error(), "0") {
		t.Errorf("error %q should mention 0", err.Error())
	}
}

func TestSingle_MultipleRows(t *testing.T) {
	resp := &decode.Response{
		Fields: []string{"name"},
		Rows:   [][]any{{"Alice"}, {"Bob"}, {"Carol"}},
	}
	transform := Single(func(rec *Record) (string, error) { return "", nil })
	_, err := transform(resp)
	if err == nil {
		t.Fatal("expected error for multiple rows")
	}
	if !strings.Contains(err.Error(), "3") {
		t.Errorf("error %q should mention count 3", err.Error())
	}
}

func TestSingle_FnError(t *testing.T) {
	resp := &decode.Response{Fields: []string{"name"}, Rows: [][]any{{"Alice"}}}
	want := errors.New("boom")
	transform := Single(func(rec *Record) (string, error) { return "", want })
	_, err := transform(resp)
	if !errors.Is(err, want) {
		t.Errorf("error = %v, want %v", err, want)
	}
}

// ============================================================================
// EagerResultTransformer
// ============================================================================

func TestEagerResultTransformer_MultipleRows(t *testing.T) {
	resp := &decode.Response{
		Fields:    []string{"name"},
		Rows:      [][]any{{"Alice"}, {"Bob"}},
		Bookmarks: []string{"bm1"},
	}
	res, err := EagerResultTransformer(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Records) != 2 {
		t.Fatalf("records = %d, want 2", len(res.Records))
	}
	if v, _ := res.Records[0].GetString("name"); v != "Alice" {
		t.Errorf("record0 name = %q", v)
	}
	if v, _ := res.Records[1].GetString("name"); v != "Bob" {
		t.Errorf("record1 name = %q", v)
	}
	if len(res.Keys) != 1 || res.Keys[0] != "name" {
		t.Errorf("keys = %v", res.Keys)
	}
	if len(res.Bookmarks) != 1 || res.Bookmarks[0] != "bm1" {
		t.Errorf("bookmarks = %v", res.Bookmarks)
	}
}

func TestEagerResultTransformer_Empty(t *testing.T) {
	resp := &decode.Response{Fields: []string{"name"}, Rows: [][]any{}}
	res, err := EagerResultTransformer(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Records) != 0 {
		t.Errorf("records = %d, want 0", len(res.Records))
	}
}

// ============================================================================
// HasWarnings / Warnings on EagerResult
// ============================================================================

func TestEagerResult_NoNotifications(t *testing.T) {
	e := &EagerResult{}
	if e.HasWarnings() {
		t.Error("HasWarnings() = true, want false")
	}
	if len(e.Warnings()) != 0 {
		t.Error("Warnings() not empty")
	}
}

func TestEagerResult_WarningNotification(t *testing.T) {
	e := &EagerResult{Notifications: []decode.Notification{
		{Severity: decode.SeverityWarning, Code: "W1"},
	}}
	if !e.HasWarnings() {
		t.Error("HasWarnings() = false, want true")
	}
	w := e.Warnings()
	if len(w) != 1 || w[0].Code != "W1" {
		t.Errorf("Warnings() = %v", w)
	}
}

func TestEagerResult_InformationOnly(t *testing.T) {
	e := &EagerResult{Notifications: []decode.Notification{
		{Severity: decode.SeverityInformation, Code: "I1"},
	}}
	if e.HasWarnings() {
		t.Error("HasWarnings() = true, want false")
	}
	if len(e.Warnings()) != 0 {
		t.Error("Warnings() not empty")
	}
}

func TestEagerResult_MixedNotifications(t *testing.T) {
	e := &EagerResult{Notifications: []decode.Notification{
		{Severity: decode.SeverityInformation, Code: "I1"},
		{Severity: decode.SeverityWarning, Code: "W1"},
		{Severity: decode.SeverityInformation, Code: "I2"},
		{Severity: decode.SeverityWarning, Code: "W2"},
	}}
	if !e.HasWarnings() {
		t.Error("HasWarnings() = false, want true")
	}
	w := e.Warnings()
	if len(w) != 2 || w[0].Code != "W1" || w[1].Code != "W2" {
		t.Errorf("Warnings() = %v, want only W1, W2", w)
	}
}

// ============================================================================
// EagerResult.String
// ============================================================================

func TestEagerResult_String(t *testing.T) {
	resp := &decode.Response{
		Fields: []string{"name"},
		Rows:   [][]any{{"Alice"}, {"Bob"}},
		Notifications: []decode.Notification{
			{Severity: decode.SeverityWarning, Code: "W1", Title: "warn"},
		},
		QueryPlan: &decode.PlanOperator{OperatorType: "ProduceResults"},
		Bookmarks: []string{"bm1"},
	}
	res, _ := EagerResultTransformer(resp)
	s := res.String()
	if s == "" {
		t.Fatal("String() returned empty")
	}
	if !strings.Contains(s, "name") {
		t.Errorf("String() missing keys: %q", s)
	}
	if !strings.Contains(s, "Records: 2") {
		t.Errorf("String() missing record count: %q", s)
	}
}

// ============================================================================
// Collect
// ============================================================================

func TestCollect_Success(t *testing.T) {
	type person struct {
		Name string
		Age  int64
	}
	resp := &decode.Response{
		Fields: []string{"name", "age"},
		Rows: [][]any{
			{"Alice", int64(30)},
			{"Bob", int64(25)},
		},
	}
	transform := Collect(func(rec *Record) (person, error) {
		name, _ := rec.GetString("name")
		age, _ := rec.GetInt64("age")
		return person{Name: name, Age: age}, nil
	})
	got, err := transform(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0] != (person{"Alice", 30}) || got[1] != (person{"Bob", 25}) {
		t.Errorf("got %+v", got)
	}
}

func TestCollect_FnErrorPropagates(t *testing.T) {
	resp := &decode.Response{
		Fields: []string{"name"},
		Rows:   [][]any{{"Alice"}, {"Bob"}},
	}
	transform := Collect(func(rec *Record) (string, error) {
		name, _ := rec.GetString("name")
		if name == "Bob" {
			return "", errors.New("bad bob")
		}
		return name, nil
	})
	_, err := transform(resp)
	if err == nil {
		t.Fatal("expected error")
	}
	// Error from row index 1.
	if !strings.Contains(err.Error(), "collect row 1") {
		t.Errorf("error %q should contain 'collect row 1'", err.Error())
	}
	if !strings.Contains(err.Error(), "bad bob") {
		t.Errorf("error %q should wrap original 'bad bob'", err.Error())
	}
}
