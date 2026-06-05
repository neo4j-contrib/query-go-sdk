package query

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/neo4j-contrib/query-go-sdk/internal/api"
)

// testLogger creates a logger for testing that writes warn+ to stderr.
func testLogger() *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelWarn}
	handler := slog.NewTextHandler(os.Stderr, opts)
	return slog.New(handler)
}

// ============================================================================
// Mock types
// ============================================================================

// mockRequestService is a basic mock of api.RequestService.
// It records the last call method and body but does not inspect the context.
type mockRequestService struct {
	response   *api.Response
	err        error
	lastMethod string
	lastBody   string
}

// mockRequestServiceWithDelay is a mock that can simulate slow responses and
// respects context cancellation / deadlines. mu guards the recording fields so
// the mock is safe to share across goroutines in concurrent tests.
type mockRequestServiceWithDelay struct {
	mu         sync.Mutex
	response   *api.Response
	err        error
	delay      time.Duration
	lastMethod string
	lastBody   string
	callCount  int
}

// contextCheckMock is a mock that invokes OnPost on each Post call, allowing
// tests to inspect the context that was propagated to the API layer.
type contextCheckMock struct {
	response  *api.Response
	err       error
	OnPost    func(context.Context)
	callCount int
}

// ============================================================================
// mockRequestService — simple mock, does not check context
// ============================================================================

func (m *mockRequestService) Post(_ context.Context, body string) (*api.Response, error) {
	m.lastMethod = "POST"
	m.lastBody = body
	return m.response, m.err
}

func (m *mockRequestService) Discover(_ context.Context) (*api.DiscoveryResponse, error) {
	return &api.DiscoveryResponse{Neo4jVersion: "2026.04.0"}, nil
}

func (m *mockRequestService) Close() {}

// ============================================================================
// mockRequestServiceWithDelay — respects context cancellation, simulates slow APIs
// ============================================================================

func (m *mockRequestServiceWithDelay) Post(ctx context.Context, body string) (*api.Response, error) {
	m.mu.Lock()
	m.lastMethod = "POST"
	m.lastBody = body
	m.callCount++
	m.mu.Unlock()
	return m.executeWithDelay(ctx)
}

func (m *mockRequestServiceWithDelay) executeWithDelay(ctx context.Context) (*api.Response, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return m.response, m.err
}

func (m *mockRequestServiceWithDelay) Discover(ctx context.Context) (*api.DiscoveryResponse, error) {
	return &api.DiscoveryResponse{Neo4jVersion: "2026.04.0"}, nil
}

func (m *mockRequestServiceWithDelay) Close() {}

// ============================================================================
// contextCheckMock — invokes OnPost callback for context propagation tests
// ============================================================================

func (m *contextCheckMock) Post(ctx context.Context, _ string) (*api.Response, error) {
	m.callCount++
	if m.OnPost != nil {
		m.OnPost(ctx)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return m.response, m.err
}

func (m *contextCheckMock) Discover(_ context.Context) (*api.DiscoveryResponse, error) {
	return &api.DiscoveryResponse{Neo4jVersion: "2026.04.0"}, nil
}

func (m *contextCheckMock) Close() {}
