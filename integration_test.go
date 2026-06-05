// Package query_test provides black-box integration tests for the query package.
//
// These tests exercise the package's public API exclusively — no internal types,
// unexported symbols, or mock infrastructure from the main package are used.
// A local httptest.Server replaces the real Neo4j server, giving deterministic,
// network-free coverage of the full request/response path.
package query_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	query "github.com/neo4j-contrib/query-go-sdk"
)

// ─── Test server helpers ─────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func newTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func newClient(t *testing.T, srv *httptest.Server) *query.QueryAPIClient {
	t.Helper()
	client, err := query.NewClient(
		query.WithBasicAuth("neo4j", "password"),
		query.WithBaseURL(srv.URL),
		query.WithTimeout(5*time.Second),
		query.WithMaxRetry(1),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func validQueryPayload(fields []string, rows [][]any) map[string]any {
	values := make([][]map[string]any, len(rows))
	for i, row := range rows {
		values[i] = make([]map[string]any, len(row))
		for j, v := range row {
			switch val := v.(type) {
			case string:
				values[i][j] = map[string]any{"$type": "String", "_value": val}
			case int64:
				values[i][j] = map[string]any{"$type": "Integer", "_value": val}
			default:
				values[i][j] = map[string]any{"$type": "String", "_value": val}
			}
		}
	}
	return map[string]any{
		"data":      map[string]any{"fields": fields, "values": values},
		"bookmarks": []string{},
	}
}

// ─── Query execution tests ─────────────────────────────────────────────────

func TestQuery_Execute_Success(t *testing.T) {
	payload := validQueryPayload([]string{"name"}, [][]any{{"Alice"}, {"Bob"}})

	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, payload)
	}))

	result, err := newClient(t, srv).Query.Execute(context.Background(), "MATCH (n:Person) RETURN n.name AS name", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Fields) != 1 || result.Fields[0] != "name" {
		t.Errorf("expected fields ['name'], got %v", result.Fields)
	}
	if len(result.Rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(result.Rows))
	}
}

func TestQuery_Execute_SendsCorrectPath(t *testing.T) {
	payload := validQueryPayload([]string{}, nil)

	var gotPath string
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(w, http.StatusOK, payload)
	}))

	_, err := newClient(t, srv).Query.Execute(context.Background(), "RETURN 1", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/db/neo4j/query/v2" {
		t.Errorf("expected path '/db/neo4j/query/v2', got '%s'", gotPath)
	}
}

func TestQuery_Execute_SendsAuthorizationHeader(t *testing.T) {
	payload := validQueryPayload([]string{}, nil)

	var gotAuth string
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeJSON(w, http.StatusOK, payload)
	}))

	_, err := newClient(t, srv).Query.Execute(context.Background(), "RETURN 1", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Errorf("expected Basic Authorization header, got '%s'", gotAuth)
	}
}

func TestQuery_Execute_SendsRequestBody(t *testing.T) {
	payload := validQueryPayload([]string{}, nil)

	var gotBody map[string]any
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeJSON(w, http.StatusOK, payload)
	}))

	params := map[string]any{"name": "Alice"}
	_, err := newClient(t, srv).Query.Execute(context.Background(), "RETURN $name AS name", params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotBody["statement"] != "RETURN $name AS name" {
		t.Errorf("expected statement in body, got: %v", gotBody)
	}
	if gotBody["parameters"] == nil {
		t.Error("expected parameters in request body")
	}
}

func TestQuery_Execute_401_Returns_Error(t *testing.T) {
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "Unauthorized"})
	}))

	_, err := newClient(t, srv).Query.Execute(context.Background(), "RETURN 1", nil)
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	var apiErr *query.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *query.Error, got %T: %v", err, err)
	}
	if !apiErr.IsUnauthorized() {
		t.Errorf("expected IsUnauthorized() = true, got status %d", apiErr.StatusCode)
	}
}

func TestQuery_Execute_500_Returns_Error(t *testing.T) {
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"message": "Internal Server Error"})
	}))

	_, err := newClient(t, srv).Query.Execute(context.Background(), "RETURN 1", nil)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	var apiErr *query.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *query.Error, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 500 {
		t.Errorf("expected status 500, got %d", apiErr.StatusCode)
	}
}

func TestQuery_Execute_QueryError(t *testing.T) {
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"errors": []map[string]any{
				{"code": "Neo.ClientError.Statement.SyntaxError", "message": "Invalid input"},
			},
		})
	}))

	_, err := newClient(t, srv).Query.Execute(context.Background(), "INVALID !!!", nil)
	if err == nil {
		t.Fatal("expected error for query error response")
	}
	var qErr *query.QueryErrors
	if !errors.As(err, &qErr) {
		t.Fatalf("expected *query.QueryErrors, got %T: %v", err, err)
	}
	if len(qErr.Errors) != 1 {
		t.Fatalf("expected 1 query error, got %d", len(qErr.Errors))
	}
}

func TestQuery_Execute_SlowServer_Timeout(t *testing.T) {
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		writeJSON(w, http.StatusOK, validQueryPayload([]string{}, nil))
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := newClient(t, srv).Query.Execute(ctx, "RETURN 1", nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("expected context error, got: %v", err)
	}
}

func TestQuery_Execute_CancelledContext(t *testing.T) {
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		writeJSON(w, http.StatusOK, validQueryPayload([]string{}, nil))
	}))

	client := newClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := client.Query.Execute(ctx, "RETURN 1", nil)
		done <- err
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error after context cancellation")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("operation did not stop promptly after cancellation")
	}
}

// ─── WithTransformer tests ────────────────────────────────────────────────────

func TestWithTransformer_EagerResult(t *testing.T) {
	payload := validQueryPayload([]string{"name"}, [][]any{{"Alice"}, {"Bob"}})

	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, payload)
	}))

	client := newClient(t, srv)
	result, err := query.WithTransformer(
		client.Query,
		context.Background(),
		"MATCH (n:Person) RETURN n.name AS name",
		nil,
		query.EagerResultTransformer,
	)
	if err != nil {
		t.Fatalf("WithTransformer: %v", err)
	}
	if len(result.Keys) != 1 || result.Keys[0] != "name" {
		t.Errorf("expected Keys ['name'], got %v", result.Keys)
	}
	if len(result.Records) != 2 {
		t.Errorf("expected 2 records, got %d", len(result.Records))
	}
	name, ok := result.Records[0].GetString("name")
	if !ok || name != "Alice" {
		t.Errorf("expected first record name 'Alice', got '%s'", name)
	}
}

func TestWithTransformer_Collect(t *testing.T) {
	payload := validQueryPayload([]string{"name"}, [][]any{{"Alice"}, {"Bob"}})

	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, payload)
	}))

	client := newClient(t, srv)
	names, err := query.WithTransformer(
		client.Query,
		context.Background(),
		"MATCH (n:Person) RETURN n.name AS name",
		nil,
		query.Collect(func(rec *query.Record) (string, error) {
			name, _ := rec.GetString("name")
			return name, nil
		}),
	)
	if err != nil {
		t.Fatalf("WithTransformer Collect: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
	if names[0] != "Alice" || names[1] != "Bob" {
		t.Errorf("expected ['Alice', 'Bob'], got %v", names)
	}
}

// ─── Error type tests ─────────────────────────────────────────────────────────

func TestErrorType_ImplementsError(t *testing.T) {
	var err error = &query.Error{StatusCode: 500, Message: "server error"}
	if err.Error() == "" {
		t.Error("Error() must return a non-empty string")
	}
}

func TestErrorType_IsNotFound(t *testing.T) {
	apiErr := &query.Error{StatusCode: 404, Message: "not found"}
	if !apiErr.IsNotFound() {
		t.Error("expected IsNotFound() = true for 404")
	}
	other := &query.Error{StatusCode: 200, Message: "ok"}
	if other.IsNotFound() {
		t.Error("expected IsNotFound() = false for 200")
	}
}

func TestErrorType_IsUnauthorized(t *testing.T) {
	apiErr := &query.Error{StatusCode: 401, Message: "unauthorized"}
	if !apiErr.IsUnauthorized() {
		t.Error("expected IsUnauthorized() = true for 401")
	}
}

func TestErrorType_IsBadRequest(t *testing.T) {
	apiErr := &query.Error{StatusCode: 400, Message: "bad request"}
	if !apiErr.IsBadRequest() {
		t.Error("expected IsBadRequest() = true for 400")
	}
}

func TestClientVersion_IsAccessible(t *testing.T) {
	if query.ClientVersion == "" {
		t.Error("ClientVersion must not be empty")
	}
}

// ─── CheckVersion ─────────────────────────────────────────────────────────────

func TestCheckVersion_Integration_Passes(t *testing.T) {
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" && r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, map[string]any{
				"neo4j_version": "2026.04.0",
				"neo4j_edition": "enterprise",
			})
			return
		}
		http.NotFound(w, r)
	}))

	if err := newClient(t, srv).CheckVersion(context.Background()); err != nil {
		t.Fatalf("CheckVersion: %v", err)
	}
}

func TestCheckVersion_Integration_TooOld(t *testing.T) {
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"neo4j_version": "2025.12.0",
			"neo4j_edition": "community",
		})
	}))

	err := newClient(t, srv).CheckVersion(context.Background())
	if err == nil {
		t.Fatal("expected VersionError for old server")
	}
	var verErr *query.VersionError
	if !errors.As(err, &verErr) {
		t.Fatalf("expected *query.VersionError, got %T: %v", err, err)
	}
	if verErr.Got != "2025.12.0" {
		t.Errorf("expected Got=2025.12.0, got %q", verErr.Got)
	}
}

func TestCheckVersion_Integration_Unauthorised(t *testing.T) {
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "Unauthorized"})
	}))

	err := newClient(t, srv).CheckVersion(context.Background())
	if err == nil {
		t.Fatal("expected error for 401 discovery response")
	}
	var apiErr *query.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *query.Error, got %T: %v", err, err)
	}
	if !apiErr.IsUnauthorized() {
		t.Errorf("expected IsUnauthorized() = true, got status %d", apiErr.StatusCode)
	}
}
