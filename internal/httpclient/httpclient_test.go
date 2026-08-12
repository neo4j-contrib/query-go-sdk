package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"log/slog"
)

// testLogger returns a warn-level logger that writes to stderr.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// newTestService returns an HTTPService with a short timeout, no retries, and
// a warn-level logger — suitable for fast unit tests.
func newTestService() HTTPService {
	return NewHTTPService(5*time.Second, 0, 10*1024*1024, testLogger(), nil)
}

// ─── GET ──────────────────────────────────────────────────────────────────────

func TestGet_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":"ok"}`))
	}))
	defer srv.Close()

	svc := newTestService()
	resp, err := svc.Get(context.Background(), srv.URL+"/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if string(resp.Body) != `{"data":"ok"}` {
		t.Errorf("unexpected body: %s", resp.Body)
	}
}

func TestGet_Headers_Forwarded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Authorization header, got '%s'", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type header, got '%s'", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestService()
	headers := map[string]string{
		"Authorization": "Bearer test-token",
		"Content-Type":  "application/json",
	}
	_, err := svc.Get(context.Background(), srv.URL, headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGet_ReturnsResponseHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Request-ID", "req-12345")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestService()
	resp, err := svc.Get(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Headers.Get("X-Request-ID") != "req-12345" {
		t.Errorf("expected X-Request-ID header, got '%s'", resp.Headers.Get("X-Request-ID"))
	}
}

func TestGet_404NotError(t *testing.T) {
	// The HTTP layer should return status 404 without wrapping it as an error.
	// Error interpretation is the responsibility of the api layer above.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"not found"}`))
	}))
	defer srv.Close()

	svc := newTestService()
	resp, err := svc.Get(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatalf("expected no error for 404 at this layer, got: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// ─── POST ─────────────────────────────────────────────────────────────────────

func TestPost_BodyForwarded(t *testing.T) {
	const expectedBody = `{"name":"test-instance"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != expectedBody {
			t.Errorf("expected body '%s', got '%s'", expectedBody, body)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{}}`)) // POST test
	}))
	defer srv.Close()

	svc := newTestService()
	resp, err := svc.Post(context.Background(), srv.URL, nil, expectedBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestPost_EmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(body) != 0 {
			t.Errorf("expected empty body, got '%s'", body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestService()
	_, err := svc.Post(context.Background(), srv.URL, nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ─── PostStream ───────────────────────────────────────────────────────────────

func TestPostStream_BodyForwarded(t *testing.T) {
	const expectedBody = `{"statement":"RETURN 1"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != expectedBody {
			t.Errorf("expected body '%s', got '%s'", expectedBody, body)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"$event":"Header","_body":{"fields":[]}}` + "\n"))
	}))
	defer srv.Close()

	svc := newTestService()
	resp, err := svc.PostStream(context.Background(), srv.URL, nil, expectedBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("unexpected error reading body: %v", err)
	}
	if string(got) != `{"$event":"Header","_body":{"fields":[]}}`+"\n" {
		t.Errorf("unexpected body: %s", got)
	}
}

func TestPostStream_BodyIsLiveNotBuffered(t *testing.T) {
	// The body must be readable incrementally, i.e. PostStream itself must not
	// have consumed it — proven here by reading it after the handler has
	// already returned and the response headers have been flushed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("line1\n"))
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("line2\n"))
	}))
	defer srv.Close()

	svc := newTestService()
	resp, err := svc.PostStream(context.Background(), srv.URL, nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("unexpected error reading body: %v", err)
	}
	if string(got) != "line1\nline2\n" {
		t.Errorf("unexpected body: %q", got)
	}
}

func TestPostStream_ExceedsMaxResponseSize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("a", 100)))
	}))
	defer srv.Close()

	svc := NewHTTPService(5*time.Second, 0, 10, testLogger(), nil) // 10-byte cap
	resp, err := svc.PostStream(context.Background(), srv.URL, nil, "")
	if err != nil {
		t.Fatalf("unexpected error from PostStream itself: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	_, err = io.ReadAll(resp.Body)
	if err == nil {
		t.Fatal("expected an error reading past the size cap, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ─── PUT / PATCH / DELETE ─────────────────────────────────────────────────────

func TestPut_MethodForwarded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestService()
	_, err := svc.Put(context.Background(), srv.URL, nil, `{"memory":"16GB"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPatch_MethodForwarded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestService()
	_, err := svc.Patch(context.Background(), srv.URL, nil, `{"name":"updated"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDelete_MethodForwarded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestService()
	_, err := svc.Delete(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ─── Response body ────────────────────────────────────────────────────────────

func TestResponse_EmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	svc := newTestService()
	resp, err := svc.Get(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Body) != 0 {
		t.Errorf("expected empty body, got '%s'", resp.Body)
	}
}

func TestResponse_LargeBody_ReadFully(t *testing.T) {
	const size = 1024 * 100 // 100KB — well under the 10MB limit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("x", size)))
	}))
	defer srv.Close()

	svc := newTestService()
	resp, err := svc.Get(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Body) != size {
		t.Errorf("expected body size %d, got %d", size, len(resp.Body))
	}
}

// ─── Context ──────────────────────────────────────────────────────────────────

func TestGet_CancelledContext(t *testing.T) {
	// Server that sleeps longer than we allow.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	svc := newTestService()
	_, err := svc.Get(ctx, srv.URL, nil)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestGet_AlreadyCancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	svc := newTestService()
	_, err := svc.Get(ctx, srv.URL, nil)
	if err == nil {
		t.Fatal("expected error for pre-cancelled context")
	}
}

// ─── Invalid URL ──────────────────────────────────────────────────────────────

func TestGet_InvalidURL(t *testing.T) {
	svc := newTestService()
	_, err := svc.Get(context.Background(), "://bad-url", nil)
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}
