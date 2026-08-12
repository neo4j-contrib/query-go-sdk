package query

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/neo4j-contrib/query-go-sdk/internal/api"
)

func TestNewClient_WithBasicAuth_Success(t *testing.T) {
	client, err := NewClient(WithBasicAuth("neo4j", "password"))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected client to be non-nil")
	}
	if client.Query == nil {
		t.Error("expected Query service to be initialized")
	}
}

func TestNewClient_WithBearerToken_Success(t *testing.T) {
	client, err := NewClient(WithBearerToken("my-token"))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected client to be non-nil")
	}
}

func TestNewClient_NoAuth_Error(t *testing.T) {
	_, err := NewClient()
	if err == nil {
		t.Fatal("expected error when no auth is provided")
	}
}

func TestNewClient_BothAuth_Error(t *testing.T) {
	_, err := NewClient(
		WithBasicAuth("neo4j", "password"),
		WithBearerToken("token"),
	)
	if err == nil {
		t.Fatal("expected error when both auth options are set")
	}
}

func TestNewClient_QueryServiceInitialized(t *testing.T) {
	client, err := NewClient(WithBasicAuth("neo4j", "password"))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client.Query == nil {
		t.Error("expected Query service to be non-nil")
	}
}

func TestWithDatabase_Valid(t *testing.T) {
	client, err := NewClient(
		WithBasicAuth("neo4j", "password"),
		WithDatabase("mydb"),
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected client to be non-nil")
	}
}

func TestWithDatabase_Empty_Error(t *testing.T) {
	_, err := NewClient(
		WithBasicAuth("neo4j", "password"),
		WithDatabase(""),
	)
	if err == nil {
		t.Fatal("expected error for empty database")
	}
}

func TestWithStreamingSupport_Enabled(t *testing.T) {
	client, err := NewClient(
		WithBasicAuth("neo4j", "password"),
		WithStreamingSupport(true),
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected client to be non-nil")
	}
}

func TestWithStreamingSupport_LegacyFlavor_Error(t *testing.T) {
	_, err := NewClient(
		WithBasicAuth("neo4j", "password"),
		WithStreamingSupport(true),
		WithAPIFlavor(FlavorLegacyHTTP),
	)
	if err == nil {
		t.Fatal("expected error combining WithStreamingSupport(true) and FlavorLegacyHTTP")
	}
}

func TestWithStreamingSupport_LegacyFlavor_OptionOrderIndependent(t *testing.T) {
	_, err := NewClient(
		WithBasicAuth("neo4j", "password"),
		WithAPIFlavor(FlavorLegacyHTTP),
		WithStreamingSupport(true),
	)
	if err == nil {
		t.Fatal("expected error combining FlavorLegacyHTTP and WithStreamingSupport(true) regardless of option order")
	}
}

func TestWithTimeout_Valid(t *testing.T) {
	client, err := NewClient(
		WithBasicAuth("neo4j", "password"),
		WithTimeout(60*time.Second),
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected client to be non-nil")
	}
}

func TestWithTimeout_Zero_Error(t *testing.T) {
	_, err := NewClient(
		WithBasicAuth("neo4j", "password"),
		WithTimeout(0),
	)
	if err == nil {
		t.Fatal("expected error for zero timeout")
	}
	if err.Error() != "timeout must be greater than zero" {
		t.Errorf("expected timeout error message, got '%s'", err.Error())
	}
}

func TestWithTimeout_Negative_Error(t *testing.T) {
	_, err := NewClient(
		WithBasicAuth("neo4j", "password"),
		WithTimeout(-10*time.Second),
	)
	if err == nil {
		t.Fatal("expected error for negative timeout")
	}
}

func TestWithMaxRetry_Valid(t *testing.T) {
	client, err := NewClient(
		WithBasicAuth("neo4j", "password"),
		WithMaxRetry(5),
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected client to be non-nil")
	}
}

func TestWithMaxRetry_Zero_Error(t *testing.T) {
	_, err := NewClient(
		WithBasicAuth("neo4j", "password"),
		WithMaxRetry(0),
	)
	if err == nil {
		t.Fatal("expected error for zero max retry")
	}
}

func TestWithLogger_Valid(t *testing.T) {
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	handler := slog.NewTextHandler(os.Stderr, opts)
	customLogger := slog.New(handler)

	client, err := NewClient(
		WithBasicAuth("neo4j", "password"),
		WithLogger(customLogger),
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected client to be non-nil")
	}
}

func TestWithLogger_Nil_Error(t *testing.T) {
	_, err := NewClient(
		WithBasicAuth("neo4j", "password"),
		WithLogger(nil),
	)
	if err == nil {
		t.Fatal("expected error for nil logger")
	}
	if err.Error() != "logger cannot be nil" {
		t.Errorf("expected 'logger cannot be nil', got '%s'", err.Error())
	}
}

func TestWithBaseURL_Valid(t *testing.T) {
	client, err := NewClient(
		WithBasicAuth("neo4j", "password"),
		WithBaseURL("http://neo4j.example.com:7474"),
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected client to be non-nil")
	}
}

func TestWithBaseURL_Empty_Error(t *testing.T) {
	_, err := NewClient(
		WithBasicAuth("neo4j", "password"),
		WithBaseURL(""),
	)
	if err == nil {
		t.Fatal("expected error for empty base URL")
	}
	if err.Error() != "base URL must not be empty" {
		t.Errorf("expected 'base URL must not be empty', got '%s'", err.Error())
	}
}

func TestWithHTTPClient_Valid(t *testing.T) {
	client, err := NewClient(
		WithBasicAuth("neo4j", "password"),
		WithHTTPClient(&http.Client{}),
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected client to be non-nil")
	}
}

func TestWithHTTPClient_Nil_Error(t *testing.T) {
	_, err := NewClient(
		WithBasicAuth("neo4j", "password"),
		WithHTTPClient(nil),
	)
	if err == nil {
		t.Fatal("expected error for nil HTTP client")
	}
}

func TestWithUserAgent_Valid(t *testing.T) {
	client, err := NewClient(
		WithBasicAuth("neo4j", "password"),
		WithUserAgent("my-app/1.0"),
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected client to be non-nil")
	}
}

func TestWithUserAgent_Empty_Error(t *testing.T) {
	_, err := NewClient(
		WithBasicAuth("neo4j", "password"),
		WithUserAgent(""),
	)
	if err == nil {
		t.Fatal("expected error for empty user agent")
	}
}

func TestWithDefaultHeaders_Valid(t *testing.T) {
	client, err := NewClient(
		WithBasicAuth("neo4j", "password"),
		WithDefaultHeaders(map[string]string{"X-Custom": "value"}),
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected client to be non-nil")
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.config.apiTimeout != 120*time.Second {
		t.Errorf("expected default timeout 120s, got %v", opts.config.apiTimeout)
	}
	if opts.config.apiRetryMax != 3 {
		t.Errorf("expected default apiRetryMax 3, got %d", opts.config.apiRetryMax)
	}
	if opts.logger == nil {
		t.Error("expected default logger to be initialized")
	}
}

func TestClientVersion_IsNotEmpty(t *testing.T) {
	if ClientVersion == "" {
		t.Error("ClientVersion must not be empty")
	}
}

func TestNewClient_MultipleOptions_Success(t *testing.T) {
	client, err := NewClient(
		WithBasicAuth("neo4j", "password"),
		WithTimeout(90*time.Second),
		WithBaseURL("http://neo4j.example.com:7474"),
		WithMaxRetry(5),
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected client to be non-nil")
	}
}

// ============================================================================
// CheckVersion tests
// ============================================================================

// versionMock wraps mockRequestService and overrides Discover.
type versionMock struct {
	mockRequestService
	discoverResp *api.DiscoveryResponse
	discoverErr  error
}

func (m *versionMock) Discover(_ context.Context) (*api.DiscoveryResponse, error) {
	return m.discoverResp, m.discoverErr
}

func newVersionClient(mock api.RequestService) *QueryAPIClient {
	c := &QueryAPIClient{api: mock, logger: testLogger()}
	c.Query = &queryService{api: mock, timeout: 30 * time.Second, logger: testLogger()}
	return c
}

func TestCheckVersion_PassesWhenAtMinimum(t *testing.T) {
	mock := &versionMock{discoverResp: &api.DiscoveryResponse{Neo4jVersion: "2026.04.0"}}
	if err := newVersionClient(mock).CheckVersion(context.Background()); err != nil {
		t.Fatalf("expected no error for minimum version, got: %v", err)
	}
}

func TestCheckVersion_PassesWhenNewer(t *testing.T) {
	for _, v := range []string{"2026.05.0", "2026.04.1", "2027.01.0"} {
		mock := &versionMock{discoverResp: &api.DiscoveryResponse{Neo4jVersion: v}}
		if err := newVersionClient(mock).CheckVersion(context.Background()); err != nil {
			t.Errorf("version %s: expected pass, got: %v", v, err)
		}
	}
}

func TestCheckVersion_FailsWhenTooOld(t *testing.T) {
	for _, v := range []string{"2026.03.9", "2025.12.0", "2024.01.0"} {
		mock := &versionMock{discoverResp: &api.DiscoveryResponse{Neo4jVersion: v}}
		err := newVersionClient(mock).CheckVersion(context.Background())
		if err == nil {
			t.Errorf("version %s: expected VersionError, got nil", v)
			continue
		}
		var verErr *VersionError
		if !errors.As(err, &verErr) {
			t.Errorf("version %s: expected *VersionError, got %T: %v", v, err, err)
			continue
		}
		if verErr.Got != v {
			t.Errorf("version %s: VersionError.Got = %q", v, verErr.Got)
		}
		if verErr.Required != minNeo4jVersion {
			t.Errorf("version %s: VersionError.Required = %q, want %q", v, verErr.Required, minNeo4jVersion)
		}
	}
}

func TestCheckVersion_MissingVersionField(t *testing.T) {
	mock := &versionMock{discoverResp: &api.DiscoveryResponse{Neo4jVersion: ""}}
	if err := newVersionClient(mock).CheckVersion(context.Background()); err == nil {
		t.Fatal("expected error for missing neo4j_version")
	}
}

func TestCheckVersion_DiscoverError(t *testing.T) {
	discoverErr := &api.Error{StatusCode: 401, Message: "Unauthorized"}
	mock := &versionMock{discoverErr: discoverErr}
	err := newVersionClient(mock).CheckVersion(context.Background())
	if err == nil {
		t.Fatal("expected error when Discover fails")
	}
	if !errors.Is(err, discoverErr) {
		t.Errorf("expected wrapped discoverErr, got: %v", err)
	}
}
