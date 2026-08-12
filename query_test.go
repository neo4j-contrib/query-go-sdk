package query

import (
	"context"
	"errors"

	"github.com/goccy/go-json"
	"strings"
	"testing"
	"time"

	"github.com/neo4j-contrib/query-go-sdk/internal/api"
	"github.com/neo4j-contrib/query-go-sdk/internal/decode"
)

func createTestQueryService(mock api.RequestService) *queryService {
	return &queryService{
		api:     mock,
		timeout: 30 * time.Second,
		logger:  testLogger(),
	}
}

func validQueryResponseJSON(fields []string, values [][]json.RawMessage) []byte {
	body, _ := json.Marshal(map[string]any{
		"data":      map[string]any{"fields": fields, "values": values},
		"bookmarks": []string{},
	})
	return body
}

func TestQueryService_Execute_Success(t *testing.T) {
	nameVal, _ := json.Marshal(map[string]any{"$type": "String", "_value": "Alice"})
	body := validQueryResponseJSON(
		[]string{"name"},
		[][]json.RawMessage{{nameVal}},
	)

	mock := &mockRequestService{
		response: &api.Response{StatusCode: 200, Body: body},
	}
	svc := createTestQueryService(mock)

	result, err := svc.Execute(context.Background(), "RETURN 'Alice' AS name", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Fields) != 1 || result.Fields[0] != "name" {
		t.Errorf("expected fields ['name'], got %v", result.Fields)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	if result.Rows[0][0] != "Alice" {
		t.Errorf("expected 'Alice', got %v", result.Rows[0][0])
	}
}

func TestQueryService_Execute_WithParameters(t *testing.T) {
	body := validQueryResponseJSON([]string{"n"}, nil)
	mock := &mockRequestService{
		response: &api.Response{StatusCode: 200, Body: body},
	}
	svc := createTestQueryService(mock)

	params := map[string]any{"name": "Alice"}
	_, err := svc.Execute(context.Background(), "MATCH (n {name: $name}) RETURN n", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(mock.lastBody, "statement") {
		t.Errorf("expected 'statement' in request body, got: %s", mock.lastBody)
	}
	if !strings.Contains(mock.lastBody, "parameters") {
		t.Errorf("expected 'parameters' in request body, got: %s", mock.lastBody)
	}
	if !strings.Contains(mock.lastBody, "Alice") {
		t.Errorf("expected 'Alice' in request body, got: %s", mock.lastBody)
	}
}

func TestQueryService_Execute_NilParameters_NoParametersField(t *testing.T) {
	body := validQueryResponseJSON([]string{}, nil)
	mock := &mockRequestService{
		response: &api.Response{StatusCode: 200, Body: body},
	}
	svc := createTestQueryService(mock)

	_, err := svc.Execute(context.Background(), "RETURN 1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(mock.lastBody, "parameters") {
		t.Errorf("expected no 'parameters' field for nil params, got: %s", mock.lastBody)
	}
}

func TestQueryService_Execute_AccessMode(t *testing.T) {
	tests := []struct {
		name       string
		accessMode AccessMode
		want       string
		wantAbsent bool
	}{
		{name: "read", accessMode: AccessModeRead, want: `"accessMode":"Read"`},
		{name: "write", accessMode: AccessModeWrite, want: `"accessMode":"Write"`},
		{name: "unset", accessMode: AccessModeUnset, wantAbsent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := validQueryResponseJSON([]string{}, nil)
			mock := &mockRequestService{
				response: &api.Response{StatusCode: 200, Body: body},
			}
			svc := createTestQueryService(mock)
			svc.accessMode = tt.accessMode

			_, err := svc.Execute(context.Background(), "RETURN 1", nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantAbsent {
				if strings.Contains(mock.lastBody, "accessMode") {
					t.Errorf("expected no 'accessMode' field for unset access mode, got: %s", mock.lastBody)
				}
				return
			}
			if !strings.Contains(mock.lastBody, tt.want) {
				t.Errorf("expected body to contain %s, got: %s", tt.want, mock.lastBody)
			}
		})
	}
}

func TestQueryService_Execute_LegacyHTTP_OmitsAccessModeField(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"results": []any{}, "errors": []any{}})
	mock := &mockRequestService{
		response: &api.Response{StatusCode: 200, Body: body},
	}
	svc := createTestQueryService(mock)
	svc.useLegacyHTTP = true
	svc.accessMode = AccessModeRead

	_, err := svc.Execute(context.Background(), "RETURN 1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(mock.lastBody, "accessMode") {
		t.Errorf("expected no 'accessMode' field for legacy flavor, got: %s", mock.lastBody)
	}
}

func TestQueryService_Execute_APIError_401(t *testing.T) {
	mock := &mockRequestService{
		err: &api.Error{StatusCode: 401, Message: "Unauthorized"},
	}
	svc := createTestQueryService(mock)

	_, err := svc.Execute(context.Background(), "RETURN 1", nil)
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *api.Error, got %T: %v", err, err)
	}
	if !apiErr.IsUnauthorized() {
		t.Error("expected IsUnauthorized() = true")
	}
}

func TestQueryService_Execute_QueryError(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"errors": []map[string]any{
			{"code": "Neo.ClientError.Statement.SyntaxError", "message": "Invalid syntax"},
		},
	})
	mock := &mockRequestService{
		response: &api.Response{StatusCode: 200, Body: body},
	}
	svc := createTestQueryService(mock)

	_, err := svc.Execute(context.Background(), "INVALID CYPHER !!!", nil)
	if err == nil {
		t.Fatal("expected error for query error response")
	}
	var qErr *decode.QueryErrors
	if !errors.As(err, &qErr) {
		t.Fatalf("expected *decode.QueryErrors, got %T: %v", err, err)
	}
	if len(qErr.Errors) != 1 {
		t.Fatalf("expected 1 query error, got %d", len(qErr.Errors))
	}
	if qErr.Errors[0].Code != "Neo.ClientError.Statement.SyntaxError" {
		t.Errorf("unexpected error code: %s", qErr.Errors[0].Code)
	}
}

func TestQueryService_Execute_EmptyResultSet(t *testing.T) {
	body := validQueryResponseJSON([]string{"name"}, nil)
	mock := &mockRequestService{
		response: &api.Response{StatusCode: 200, Body: body},
	}
	svc := createTestQueryService(mock)

	result, err := svc.Execute(context.Background(), "MATCH (n) WHERE false RETURN n.name AS name", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(result.Rows))
	}
}

func TestQueryService_Execute_MultipleRows(t *testing.T) {
	row1Name, _ := json.Marshal(map[string]any{"$type": "String", "_value": "Alice"})
	row2Name, _ := json.Marshal(map[string]any{"$type": "String", "_value": "Bob"})
	body := validQueryResponseJSON(
		[]string{"name"},
		[][]json.RawMessage{{row1Name}, {row2Name}},
	)
	mock := &mockRequestService{
		response: &api.Response{StatusCode: 200, Body: body},
	}
	svc := createTestQueryService(mock)

	result, err := svc.Execute(context.Background(), "MATCH (n:Person) RETURN n.name AS name", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result.Rows))
	}
	if result.Rows[0][0] != "Alice" {
		t.Errorf("expected first row 'Alice', got %v", result.Rows[0][0])
	}
	if result.Rows[1][0] != "Bob" {
		t.Errorf("expected second row 'Bob', got %v", result.Rows[1][0])
	}
}

func TestQueryService_Execute_UsesPostMethod(t *testing.T) {
	body := validQueryResponseJSON([]string{}, nil)
	mock := &mockRequestService{
		response: &api.Response{StatusCode: 200, Body: body},
	}
	svc := createTestQueryService(mock)

	_, err := svc.Execute(context.Background(), "RETURN 1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.lastMethod != "POST" {
		t.Errorf("expected POST method, got %s", mock.lastMethod)
	}
}

// ============================================================================
// ExecuteStream
// ============================================================================

func createTestStreamingQueryService(mock api.RequestService) *queryService {
	return &queryService{
		api:              mock,
		timeout:          30 * time.Second,
		logger:           testLogger(),
		streamingEnabled: true,
		maxResponseSize:  10 * 1024 * 1024,
	}
}

func TestQueryService_ExecuteStream_NotEnabled_Error(t *testing.T) {
	mock := &mockRequestService{}
	svc := createTestQueryService(mock) // streamingEnabled left false

	_, err := svc.ExecuteStream(context.Background(), "RETURN 1", nil)
	if err == nil {
		t.Fatal("expected error when streaming is not enabled")
	}
	if mock.lastMethod != "" {
		t.Errorf("expected no API call to be made, got %s", mock.lastMethod)
	}
}

func TestQueryService_ExecuteStream_Success(t *testing.T) {
	mock := &mockRequestService{
		streamResponse: newStreamResponse(
			`{"$event":"Header","_body":{"fields":["name","age"]}}`,
			`{"$event":"Record","_body":["Alice",32]}`,
			`{"$event":"Record","_body":["Bob",29]}`,
			`{"$event":"Summary","_body":{"bookmarks":["bm1"],"queryType":"r"}}`,
		),
	}
	svc := createTestStreamingQueryService(mock)

	result, err := svc.ExecuteStream(context.Background(), "MATCH (n) RETURN n.name AS name, n.age AS age", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Fields()) != 2 || result.Fields()[0] != "name" || result.Fields()[1] != "age" {
		t.Errorf("fields = %v, want [name age]", result.Fields())
	}

	var names []string
	for rec, err := range result.Records() {
		if err != nil {
			t.Fatalf("unexpected error iterating records: %v", err)
		}
		name, _ := rec.GetString("name")
		names = append(names, name)
	}
	if len(names) != 2 || names[0] != "Alice" || names[1] != "Bob" {
		t.Errorf("names = %v, want [Alice Bob]", names)
	}

	summary := result.Summary()
	if summary == nil {
		t.Fatal("expected non-nil summary after draining records")
	}
	if len(summary.Bookmarks) != 1 || summary.Bookmarks[0] != "bm1" {
		t.Errorf("bookmarks = %v", summary.Bookmarks)
	}
	if summary.QueryType != "r" {
		t.Errorf("queryType = %q, want r", summary.QueryType)
	}
	if mock.lastMethod != "POSTSTREAM" {
		t.Errorf("expected POSTSTREAM method, got %s", mock.lastMethod)
	}
}

func TestQueryService_ExecuteStream_EarlyBreakStopsIteration(t *testing.T) {
	mock := &mockRequestService{
		streamResponse: newStreamResponse(
			`{"$event":"Header","_body":{"fields":["n"]}}`,
			`{"$event":"Record","_body":[1]}`,
			`{"$event":"Record","_body":[2]}`,
			`{"$event":"Record","_body":[3]}`,
			`{"$event":"Summary","_body":{}}`,
		),
	}
	svc := createTestStreamingQueryService(mock)

	result, err := svc.ExecuteStream(context.Background(), "UNWIND [1,2,3] AS n RETURN n", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count := 0
	for _, err := range result.Records() {
		if err != nil {
			t.Fatalf("unexpected error iterating records: %v", err)
		}
		count++
		if count == 1 {
			break
		}
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	if result.Summary() != nil {
		t.Error("expected nil summary after an early break")
	}
}

func TestQueryService_ExecuteStream_MidStreamError(t *testing.T) {
	mock := &mockRequestService{
		streamResponse: newStreamResponse(
			`{"$event":"Header","_body":{"fields":["n"]}}`,
			`{"$event":"Error","_body":[{"code":"Neo.ClientError.Statement.SyntaxError","message":"bad query"}]}`,
		),
	}
	svc := createTestStreamingQueryService(mock)

	result, err := svc.ExecuteStream(context.Background(), "RETURN n", nil)
	if err != nil {
		t.Fatalf("unexpected error from ExecuteStream: %v", err)
	}

	var gotErr error
	for _, err := range result.Records() {
		if err != nil {
			gotErr = err
			break
		}
	}

	var qErr *decode.QueryErrors
	if !errors.As(gotErr, &qErr) {
		t.Fatalf("expected *decode.QueryErrors, got %v (%T)", gotErr, gotErr)
	}
	if qErr.Errors[0].Message != "bad query" {
		t.Errorf("message = %q", qErr.Errors[0].Message)
	}
}

func TestQueryService_ExecuteStream_FailsImmediatelyOnLeadingError(t *testing.T) {
	mock := &mockRequestService{
		streamResponse: newStreamResponse(
			`{"$event":"Error","_body":[{"code":"Neo.ClientError.Statement.SyntaxError","message":"bad query"}]}`,
		),
	}
	svc := createTestStreamingQueryService(mock)

	_, err := svc.ExecuteStream(context.Background(), "RETURN n", nil)
	var qErr *decode.QueryErrors
	if !errors.As(err, &qErr) {
		t.Fatalf("expected *decode.QueryErrors, got %v (%T)", err, err)
	}
}
