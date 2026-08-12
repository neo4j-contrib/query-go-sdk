package decode

import (
	"testing"
	"time"
)

// ============================================================================
// DecodeStreamEnvelope
// ============================================================================

func TestDecodeStreamEnvelope_Header(t *testing.T) {
	event, body, err := DecodeStreamEnvelope([]byte(`{"$event":"Header","_body":{"fields":["name","age"]}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != StreamEventHeader {
		t.Errorf("event = %q, want %q", event, StreamEventHeader)
	}
	fields, err := DecodeStreamHeader(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fields) != 2 || fields[0] != "name" || fields[1] != "age" {
		t.Errorf("fields = %v, want [name age]", fields)
	}
}

func TestDecodeStreamEnvelope_MissingEvent(t *testing.T) {
	_, _, err := DecodeStreamEnvelope([]byte(`{"_body":{}}`))
	if err == nil {
		t.Fatal("expected error for missing $event, got nil")
	}
}

func TestDecodeStreamEnvelope_InvalidJSON(t *testing.T) {
	_, _, err := DecodeStreamEnvelope([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// ============================================================================
// DecodeStreamRecord
// ============================================================================

func TestDecodeStreamRecord_PlainValues(t *testing.T) {
	row, err := DecodeStreamRecord([]byte(`["Alice", 32]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(row) != 2 || row[0] != "Alice" || row[1] != float64(32) {
		t.Errorf("row = %v", row)
	}
}

func TestDecodeStreamRecord_TypedValues(t *testing.T) {
	row, err := DecodeStreamRecord([]byte(`[{"$type":"String","_value":"Antonio"},{"$type":"Integer","_value":"39"}]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(row) != 2 {
		t.Fatalf("row = %v, want 2 elements", row)
	}
	if row[0] != "Antonio" {
		t.Errorf("row[0] = %v, want Antonio", row[0])
	}
	if row[1] != int64(39) {
		t.Errorf("row[1] = %v, want 39", row[1])
	}
}

// ============================================================================
// DecodeStreamSummary
// ============================================================================

func TestDecodeStreamSummary_Full(t *testing.T) {
	body := []byte(`{
		"bookmarks": ["FB:kcwQ/wTfJf8rS1WY+GiIKXsCXgmQ"],
		"notifications": [{"code":"Neo.ClientNotification.Statement.UnknownLabelWarning","severity":"WARNING"}],
		"queryType": "r",
		"resultAvailableAfter": 10,
		"resultConsumedAfter": 25
	}`)

	summary, err := DecodeStreamSummary(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summary.Bookmarks) != 1 || summary.Bookmarks[0] != "FB:kcwQ/wTfJf8rS1WY+GiIKXsCXgmQ" {
		t.Errorf("bookmarks = %v", summary.Bookmarks)
	}
	if summary.QueryType != "r" {
		t.Errorf("queryType = %q, want r", summary.QueryType)
	}
	if summary.ResultAvailableAfter != 10*time.Millisecond {
		t.Errorf("resultAvailableAfter = %v, want 10ms", summary.ResultAvailableAfter)
	}
	if summary.ResultConsumedAfter != 25*time.Millisecond {
		t.Errorf("resultConsumedAfter = %v, want 25ms", summary.ResultConsumedAfter)
	}
	if !summary.HasWarnings() {
		t.Error("expected HasWarnings() to be true")
	}
	if len(summary.Warnings()) != 1 {
		t.Errorf("warnings = %d, want 1", len(summary.Warnings()))
	}
}

func TestDecodeStreamSummary_QueryPlan(t *testing.T) {
	body := []byte(`{
		"queryPlan": {
			"operatorType": "ProduceResults",
			"arguments": {},
			"identifiers": ["n"],
			"children": []
		}
	}`)

	summary, err := DecodeStreamSummary(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.QueryPlan == nil {
		t.Fatal("expected QueryPlan to be non-nil")
	}
	if summary.QueryPlan.OperatorType != "ProduceResults" {
		t.Errorf("operatorType = %q", summary.QueryPlan.OperatorType)
	}
}

// ============================================================================
// DecodeStreamError
// ============================================================================

func TestDecodeStreamError(t *testing.T) {
	body := []byte(`[{"code":"Neo.ClientError.Statement.SyntaxError","message":"Invalid input 'RETURN'"}]`)

	queryErrs, err := DecodeStreamError(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(queryErrs.Errors) != 1 {
		t.Fatalf("errors = %d, want 1", len(queryErrs.Errors))
	}
	if queryErrs.Errors[0].Code != "Neo.ClientError.Statement.SyntaxError" {
		t.Errorf("code = %q", queryErrs.Errors[0].Code)
	}
	if queryErrs.Errors[0].Message != "Invalid input 'RETURN'" {
		t.Errorf("message = %q", queryErrs.Errors[0].Message)
	}
}
