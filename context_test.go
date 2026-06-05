package query

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/neo4j-contrib/query-go-sdk/internal/api"
)

func validQueryBody() []byte {
	return []byte(`{"data":{"fields":[],"values":[]},"bookmarks":[]}`)
}

func TestQueryService_Timeout_EnforcedByService(t *testing.T) {
	mock := &mockRequestServiceWithDelay{
		response: &api.Response{StatusCode: 200, Body: validQueryBody()},
		delay:    2 * time.Second,
	}
	svc := &queryService{
		api:     mock,
		timeout: 100 * time.Millisecond,
		logger:  testLogger(),
	}

	start := time.Now()
	_, err := svc.Execute(context.Background(), "RETURN 1", nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got: %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("timeout took too long: %v (expected ~100ms)", elapsed)
	}
}

func TestQueryService_PreCancelledContext(t *testing.T) {
	mock := &mockRequestServiceWithDelay{
		response: &api.Response{StatusCode: 200, Body: validQueryBody()},
		delay:    0,
	}
	svc := createTestQueryService(mock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.Execute(ctx, "RETURN 1", nil)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context error, got: %v", err)
	}
}

func TestQueryService_ParentDeadline_Wins(t *testing.T) {
	mock := &mockRequestServiceWithDelay{
		response: &api.Response{StatusCode: 200, Body: validQueryBody()},
		delay:    1 * time.Second,
	}
	svc := &queryService{
		api:     mock,
		timeout: 10 * time.Second,
		logger:  testLogger(),
	}

	parentCtx, parentCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer parentCancel()

	start := time.Now()
	_, err := svc.Execute(parentCtx, "RETURN 1", nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got: %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("should have used parent deadline (~100ms), took: %v", elapsed)
	}
}

func TestQueryService_ServiceTimeout_Wins(t *testing.T) {
	mock := &mockRequestServiceWithDelay{
		response: &api.Response{StatusCode: 200, Body: validQueryBody()},
		delay:    1 * time.Second,
	}
	svc := &queryService{
		api:     mock,
		timeout: 100 * time.Millisecond,
		logger:  testLogger(),
	}

	parentCtx, parentCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer parentCancel()

	start := time.Now()
	_, err := svc.Execute(parentCtx, "RETURN 1", nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got: %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("should have used service deadline (~100ms), took: %v", elapsed)
	}
}

func TestQueryService_MidFlightCancellation(t *testing.T) {
	mock := &mockRequestServiceWithDelay{
		response: &api.Response{StatusCode: 200, Body: validQueryBody()},
		delay:    10 * time.Second,
	}
	svc := &queryService{
		api:     mock,
		timeout: 30 * time.Second,
		logger:  testLogger(),
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := svc.Execute(ctx, "RETURN 1", nil)
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("operation did not stop quickly after cancellation")
	}
}

func TestQueryService_ContextValues_Propagate(t *testing.T) {
	type contextKey string
	testKey := contextKey("request-id")
	testValue := "test-123"

	ctx := context.WithValue(context.Background(), testKey, testValue)

	valueFound := false
	mock := &contextCheckMock{
		response: &api.Response{StatusCode: 200, Body: validQueryBody()},
		OnPost: func(receivedCtx context.Context) {
			if val := receivedCtx.Value(testKey); val == testValue {
				valueFound = true
			}
		},
	}

	svc := createTestQueryService(mock)
	_, err := svc.Execute(ctx, "RETURN 1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valueFound {
		t.Error("context value was not propagated to API layer")
	}
}

func TestQueryService_RepeatedCalls_NoContextLeak(t *testing.T) {
	mock := &mockRequestService{
		response: &api.Response{StatusCode: 200, Body: validQueryBody()},
	}
	svc := createTestQueryService(mock)

	start := time.Now()
	for i := range 1000 {
		_, err := svc.Execute(context.Background(), "RETURN 1", nil)
		if err != nil {
			t.Fatalf("iteration %d failed: %v", i, err)
		}
	}
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Errorf("1000 calls took %v — possible context leak", elapsed)
	}
}

// ============================================================================
// Benchmarks
// ============================================================================

func BenchmarkQueryService_Execute_Sequential(b *testing.B) {
	mock := &mockRequestService{
		response: &api.Response{StatusCode: 200, Body: validQueryBody()},
	}
	svc := createTestQueryService(mock)

	b.ResetTimer()
	for range b.N {
		_, _ = svc.Execute(context.Background(), "RETURN 1", nil)
	}
}

func BenchmarkQueryService_Execute_Parallel(b *testing.B) {
	mock := &mockRequestServiceWithDelay{
		response: &api.Response{StatusCode: 200, Body: validQueryBody()},
	}
	svc := createTestQueryService(mock)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = svc.Execute(context.Background(), "RETURN 1", nil)
		}
	})
}
