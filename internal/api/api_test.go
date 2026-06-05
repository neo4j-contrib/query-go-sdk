package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/neo4j-contrib/query-go-sdk/internal/testutil"
)

func testLogger() *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelWarn}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

func newTestService(mock *testutil.MockHTTPService) *apiRequestService {
	return &apiRequestService{
		httpClient: mock,
		authHeader: &BasicCredentials{Username: "neo4j", Password: "test"},
		baseURL:    "http://localhost:7474",
		database:   "neo4j",
		userAgent:  "test-agent/1.0",
		logger:     testLogger(),
	}
}

// ============================================================================
// parseError
// ============================================================================

func TestParseError_EmptyBody(t *testing.T) {
	err := parseError(nil, http.StatusNotFound)
	if err.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", err.StatusCode)
	}
	if err.Message != "Not Found" {
		t.Errorf("expected message 'Not Found', got '%s'", err.Message)
	}
	if len(err.Details) != 0 {
		t.Errorf("expected no details, got %d", len(err.Details))
	}
}

func TestParseError_MessageField(t *testing.T) {
	body := []byte(`{"message":"Instance not found"}`)
	err := parseError(body, http.StatusNotFound)
	if err.Message != "Instance not found" {
		t.Errorf("expected message 'Instance not found', got '%s'", err.Message)
	}
}

func TestParseError_ErrorsArray(t *testing.T) {
	body := []byte(`{"message":"Validation failed","errors":[{"message":"name is required","field":"name"},{"message":"region is required","field":"region"}]}`)
	err := parseError(body, http.StatusBadRequest)
	if len(err.Details) != 2 {
		t.Fatalf("expected 2 details, got %d", len(err.Details))
	}
	if err.Details[0].Message != "name is required" {
		t.Errorf("expected first detail 'name is required', got '%s'", err.Details[0].Message)
	}
	if err.Details[0].Field != "name" {
		t.Errorf("expected first detail field 'name', got '%s'", err.Details[0].Field)
	}
}

func TestParseError_DetailsArray(t *testing.T) {
	body := []byte(`{"message":"Validation failed","details":[{"message":"memory must be positive","reason":"invalid_value"}]}`)
	err := parseError(body, http.StatusUnprocessableEntity)
	if len(err.Details) != 1 {
		t.Fatalf("expected 1 detail, got %d", len(err.Details))
	}
	if err.Details[0].Reason != "invalid_value" {
		t.Errorf("expected reason 'invalid_value', got '%s'", err.Details[0].Reason)
	}
}

func TestParseError_ErrorsArrayTakesPrecedenceOverDetails(t *testing.T) {
	body := []byte(`{"message":"conflict","errors":[{"message":"from errors"}],"details":[{"message":"from details"}]}`)
	err := parseError(body, http.StatusBadRequest)
	if err.Details[0].Message != "from errors" {
		t.Errorf("expected 'from errors', got '%s'", err.Details[0].Message)
	}
}

func TestParseError_InvalidJSON_FallsBackToDefault(t *testing.T) {
	body := []byte(`not valid json`)
	err := parseError(body, http.StatusInternalServerError)
	if err.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", err.StatusCode)
	}
	if err.Message != "Internal Server Error" {
		t.Errorf("expected 'Internal Server Error', got '%s'", err.Message)
	}
}

func TestParseError_EmptyMessageField_FallsBackToStatusText(t *testing.T) {
	body := []byte(`{"message":""}`)
	err := parseError(body, http.StatusForbidden)
	if err.Message != "Forbidden" {
		t.Errorf("expected 'Forbidden' fallback, got '%s'", err.Message)
	}
}

// ============================================================================
// URL construction
// ============================================================================

func TestAPIService_URLConstruction(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusOK, `{"data":{"fields":[],"values":[]},"bookmarks":[]}`)
	svc := newTestService(mock)

	_, err := svc.Post(context.Background(), `{"statement":"RETURN 1"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "http://localhost:7474/db/neo4j/query/v2"
	if mock.LastURL != expected {
		t.Errorf("expected URL '%s', got '%s'", expected, mock.LastURL)
	}
}

// ============================================================================
// HTTP method routing
// ============================================================================

func TestAPIService_Post_UsesPostMethod(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusOK, `{"data":{"fields":[],"values":[]},"bookmarks":[]}`)
	svc := newTestService(mock)

	body := `{"statement":"RETURN 1"}`
	_, err := svc.Post(context.Background(), body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.LastMethod != "POST" {
		t.Errorf("expected POST, got %s", mock.LastMethod)
	}
	if mock.LastBody != body {
		t.Errorf("expected body '%s', got '%s'", body, mock.LastBody)
	}
}

func TestAPIService_Get_UsesGetMethod(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusOK, `{"data":{"fields":[],"values":[]},"bookmarks":[]}`)
	svc := newTestService(mock)

	_, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.LastMethod != "GET" {
		t.Errorf("expected GET, got %s", mock.LastMethod)
	}
}

func TestAPIService_Delete_UsesDeleteMethod(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusOK, `{}`)
	svc := newTestService(mock)

	_, err := svc.Delete(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.LastMethod != "DELETE" {
		t.Errorf("expected DELETE, got %s", mock.LastMethod)
	}
}

func TestAPIService_Put_UsesPutMethod(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusOK, `{}`)
	svc := newTestService(mock)

	_, err := svc.Put(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.LastMethod != "PUT" {
		t.Errorf("expected PUT, got %s", mock.LastMethod)
	}
}

func TestAPIService_Patch_UsesPatchMethod(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusOK, `{}`)
	svc := newTestService(mock)

	_, err := svc.Patch(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.LastMethod != "PATCH" {
		t.Errorf("expected PATCH, got %s", mock.LastMethod)
	}
}

// ============================================================================
// Request headers
// ============================================================================

func TestAPIService_Headers_ContentType(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusOK, `{"data":{"fields":[],"values":[]},"bookmarks":[]}`)
	svc := newTestService(mock)

	_, err := svc.Post(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.LastHeaders["Content-Type"] != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got '%s'", mock.LastHeaders["Content-Type"])
	}
}

func TestAPIService_Headers_Accept(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusOK, `{"data":{"fields":[],"values":[]},"bookmarks":[]}`)
	svc := newTestService(mock)

	_, err := svc.Post(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.LastHeaders["Accept"] != "application/vnd.neo4j.query" {
		t.Errorf("expected Accept 'application/vnd.neo4j.query', got '%s'", mock.LastHeaders["Accept"])
	}
}

func TestAPIService_Headers_UserAgent(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusOK, `{"data":{"fields":[],"values":[]},"bookmarks":[]}`)
	svc := newTestService(mock)

	_, err := svc.Post(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.LastHeaders["User-Agent"] != "test-agent/1.0" {
		t.Errorf("expected User-Agent 'test-agent/1.0', got '%s'", mock.LastHeaders["User-Agent"])
	}
}

func TestAPIService_Headers_AuthorizationBasic(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusOK, `{"data":{"fields":[],"values":[]},"bookmarks":[]}`)
	svc := newTestService(mock)

	_, err := svc.Post(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(mock.LastHeaders["Authorization"], "Basic ") {
		t.Errorf("expected Basic Authorization header, got '%s'", mock.LastHeaders["Authorization"])
	}
}

func TestAPIService_Headers_AuthorizationBearer(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusOK, `{"data":{"fields":[],"values":[]},"bookmarks":[]}`)
	svc := &apiRequestService{
		httpClient: mock,
		authHeader: &StaticCredentials{Token: "my-bearer-token"},
		baseURL:    "http://localhost:7474",
		userAgent:  "test-agent/1.0",
		logger:     testLogger(),
	}

	_, err := svc.Post(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.LastHeaders["Authorization"] != "Bearer my-bearer-token" {
		t.Errorf("expected 'Bearer my-bearer-token', got '%s'", mock.LastHeaders["Authorization"])
	}
}

func TestAPIService_DefaultHeaders_ReachServer(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusOK, `{"data":{"fields":[],"values":[]},"bookmarks":[]}`)
	svc := newTestService(mock)
	svc.defaultHeaders = map[string]string{
		"X-Request-ID": "req-abc-123",
		"X-Custom":     "custom-value",
	}

	_, err := svc.Post(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.LastHeaders["X-Request-ID"] != "req-abc-123" {
		t.Errorf("expected X-Request-ID 'req-abc-123', got '%s'", mock.LastHeaders["X-Request-ID"])
	}
	if mock.LastHeaders["X-Custom"] != "custom-value" {
		t.Errorf("expected X-Custom 'custom-value', got '%s'", mock.LastHeaders["X-Custom"])
	}
}

func TestAPIService_DefaultHeaders_CannotOverrideProtected(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusOK, `{"data":{"fields":[],"values":[]},"bookmarks":[]}`)
	svc := newTestService(mock)
	svc.defaultHeaders = map[string]string{
		"Authorization": "Bearer sneaky",
		"Content-Type":  "text/plain",
		"User-Agent":    "evil-agent/1.0",
	}

	_, err := svc.Post(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(mock.LastHeaders["Authorization"], "Basic ") {
		t.Errorf("Authorization was overridden; got '%s'", mock.LastHeaders["Authorization"])
	}
	if mock.LastHeaders["Content-Type"] != "application/json" {
		t.Errorf("Content-Type was overridden; got '%s'", mock.LastHeaders["Content-Type"])
	}
	if mock.LastHeaders["User-Agent"] != "test-agent/1.0" {
		t.Errorf("User-Agent was overridden; got '%s'", mock.LastHeaders["User-Agent"])
	}
}

// ============================================================================
// Response handling
// ============================================================================

func TestAPIService_Response_BodyAndStatusReturned(t *testing.T) {
	expectedBody := []byte(`{"data":{"fields":["n"],"values":[]},"bookmarks":[]}`)
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusOK, string(expectedBody))
	svc := newTestService(mock)

	resp, err := svc.Post(context.Background(), `{"statement":"RETURN 1"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if string(resp.Body) != string(expectedBody) {
		t.Errorf("expected body '%s', got '%s'", expectedBody, resp.Body)
	}
}

func TestAPIService_Response_201IsSuccess(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusCreated, `{}`)
	svc := newTestService(mock)

	resp, err := svc.Post(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error for 201: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected status 201, got %d", resp.StatusCode)
	}
}

// ============================================================================
// API error responses (non-2xx → *Error)
// ============================================================================

func TestAPIService_ErrorResponse_400(t *testing.T) {
	body := `{"message":"Bad Request","errors":[{"message":"statement is required","field":"statement"}]}`
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusBadRequest, body)
	svc := newTestService(mock)

	_, err := svc.Post(context.Background(), `{}`)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	apiErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if !apiErr.IsBadRequest() {
		t.Error("expected IsBadRequest() to be true")
	}
}

func TestAPIService_ErrorResponse_401(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusUnauthorized, `{"message":"Invalid credentials"}`)
	svc := newTestService(mock)

	_, err := svc.Post(context.Background(), `{}`)
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	apiErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if !apiErr.IsUnauthorized() {
		t.Error("expected IsUnauthorized() to be true")
	}
}

func TestAPIService_ErrorResponse_404(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusNotFound, `{"message":"Not Found"}`)
	svc := newTestService(mock)

	_, err := svc.Get(context.Background())
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	apiErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if !apiErr.IsNotFound() {
		t.Error("expected IsNotFound() to be true")
	}
}

func TestAPIService_HTTPClientError_Propagated(t *testing.T) {
	networkErr := fmt.Errorf("connection refused")
	mock := testutil.NewMockHTTPService()
	mock.WithError(networkErr)
	svc := newTestService(mock)

	_, err := svc.Post(context.Background(), `{}`)
	if err == nil {
		t.Fatal("expected error to be propagated")
	}
	if !errors.Is(err, networkErr) {
		t.Errorf("expected networkErr, got %v", err)
	}
}

// ============================================================================
// Context handling
// ============================================================================

func TestAPIService_CancelledContext_RejectedBeforeHTTPCall(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusOK, `{}`)
	svc := newTestService(mock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.Post(ctx, `{}`)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if mock.CallCount != 0 {
		t.Errorf("expected 0 HTTP calls, got %d", mock.CallCount)
	}
}

func TestAPIService_ExpiredDeadline_RejectedBeforeHTTPCall(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusOK, `{}`)
	svc := newTestService(mock)

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, err := svc.Post(ctx, `{}`)
	if err == nil {
		t.Fatal("expected error for expired deadline")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
	if mock.CallCount != 0 {
		t.Errorf("expected 0 HTTP calls, got %d", mock.CallCount)
	}
}

// ============================================================================
// Error type helper methods
// ============================================================================

func TestError_IsNotFound(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{http.StatusNotFound, true},
		{http.StatusOK, false},
		{http.StatusUnauthorized, false},
	}
	for _, tt := range tests {
		e := &Error{StatusCode: tt.code}
		if e.IsNotFound() != tt.want {
			t.Errorf("status %d: IsNotFound() = %v, want %v", tt.code, e.IsNotFound(), tt.want)
		}
	}
}

func TestError_IsUnauthorized(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{http.StatusUnauthorized, true},
		{http.StatusForbidden, false},
		{http.StatusOK, false},
	}
	for _, tt := range tests {
		e := &Error{StatusCode: tt.code}
		if e.IsUnauthorized() != tt.want {
			t.Errorf("status %d: IsUnauthorized() = %v, want %v", tt.code, e.IsUnauthorized(), tt.want)
		}
	}
}

func TestError_IsBadRequest(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{http.StatusBadRequest, true},
		{http.StatusUnprocessableEntity, false},
		{http.StatusOK, false},
	}
	for _, tt := range tests {
		e := &Error{StatusCode: tt.code}
		if e.IsBadRequest() != tt.want {
			t.Errorf("status %d: IsBadRequest() = %v, want %v", tt.code, e.IsBadRequest(), tt.want)
		}
	}
}

func TestError_Error_NoDetails(t *testing.T) {
	e := &Error{StatusCode: 404, Message: "Not Found"}
	expected := "API error (status 404): Not Found"
	if e.Error() != expected {
		t.Errorf("expected '%s', got '%s'", expected, e.Error())
	}
}

func TestError_Error_SingleDetail(t *testing.T) {
	e := &Error{
		StatusCode: 400,
		Message:    "Bad Request",
		Details:    []ErrorDetail{{Message: "name is required"}},
	}
	expected := "API error (status 400): Bad Request - name is required"
	if e.Error() != expected {
		t.Errorf("expected '%s', got '%s'", expected, e.Error())
	}
}

func TestError_Error_MultipleDetails(t *testing.T) {
	e := &Error{
		StatusCode: 422,
		Message:    "Validation Error",
		Details: []ErrorDetail{
			{Message: "field A"},
			{Message: "field B"},
			{Message: "field C"},
		},
	}
	msg := e.Error()
	if !strings.Contains(msg, "and 2 more error(s)") {
		t.Errorf("expected '2 more error(s)' in message, got '%s'", msg)
	}
}

func TestError_AllErrors(t *testing.T) {
	e := &Error{
		StatusCode: 400,
		Message:    "top-level",
		Details:    []ErrorDetail{{Message: "detail-1"}, {Message: "detail-2"}},
	}
	all := e.AllErrors()
	if len(all) != 3 {
		t.Fatalf("expected 3 errors, got %d", len(all))
	}
	if all[0] != "top-level" {
		t.Errorf("expected first to be top-level message, got '%s'", all[0])
	}
}

func TestError_HasMultipleErrors(t *testing.T) {
	single := &Error{Details: []ErrorDetail{{Message: "one"}}}
	if single.HasMultipleErrors() {
		t.Error("single detail: expected HasMultipleErrors() = false")
	}
	multi := &Error{Details: []ErrorDetail{{Message: "one"}, {Message: "two"}}}
	if !multi.HasMultipleErrors() {
		t.Error("two details: expected HasMultipleErrors() = true")
	}
}

// ============================================================================
// Discover
// ============================================================================

func TestAPIService_Discover_Success(t *testing.T) {
	body := `{"neo4j_version":"2026.04.0","neo4j_edition":"enterprise"}`
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusOK, body)
	svc := newTestService(mock)

	resp, err := svc.Discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.LastMethod != "GET" {
		t.Errorf("expected GET, got %s", mock.LastMethod)
	}
	if mock.LastURL != "http://localhost:7474/" {
		t.Errorf("expected URL 'http://localhost:7474/', got %s", mock.LastURL)
	}
	if resp.Neo4jVersion != "2026.04.0" {
		t.Errorf("expected version '2026.04.0', got %q", resp.Neo4jVersion)
	}
	if resp.Neo4jEdition != "enterprise" {
		t.Errorf("expected edition 'enterprise', got %q", resp.Neo4jEdition)
	}
}

func TestAPIService_Discover_SetsAuthHeader(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusOK, `{"neo4j_version":"2026.04.0"}`)
	svc := newTestService(mock)

	_, err := svc.Discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(mock.LastHeaders["Authorization"], "Basic ") {
		t.Errorf("expected Basic auth header, got %q", mock.LastHeaders["Authorization"])
	}
}

func TestAPIService_Discover_NonSuccessStatus(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusUnauthorized, `{"message":"Unauthorized"}`)
	svc := newTestService(mock)

	_, err := svc.Discover(context.Background())
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	apiErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if !apiErr.IsUnauthorized() {
		t.Errorf("expected IsUnauthorized() = true, got status %d", apiErr.StatusCode)
	}
}

func TestAPIService_Discover_NetworkError(t *testing.T) {
	networkErr := fmt.Errorf("connection refused")
	mock := testutil.NewMockHTTPService()
	mock.WithError(networkErr)
	svc := newTestService(mock)

	_, err := svc.Discover(context.Background())
	if err == nil {
		t.Fatal("expected error from network failure")
	}
	if !errors.Is(err, networkErr) {
		t.Errorf("expected networkErr, got %v", err)
	}
}

func TestAPIService_Discover_CancelledContext(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusOK, `{"neo4j_version":"2026.04.0"}`)
	svc := newTestService(mock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.Discover(ctx)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if mock.CallCount != 0 {
		t.Errorf("expected no HTTP call for cancelled context, got %d", mock.CallCount)
	}
}

func TestAPIService_Discover_MalformedJSON(t *testing.T) {
	mock := testutil.NewMockHTTPService()
	mock.WithResponse(http.StatusOK, `not-json`)
	svc := newTestService(mock)

	_, err := svc.Discover(context.Background())
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("expected unmarshal error, got: %v", err)
	}
}
