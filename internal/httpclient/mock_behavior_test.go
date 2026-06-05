// Package httpclient_test contains black-box behavioral tests for the httpclient
// package. These tests exercise the package through its exported interface only,
// using real httptest servers so the full HTTP path is exercised without any
// network access.
package httpclient_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"log/slog"

	"github.com/neo4j-contrib/query-go-sdk/internal/httpclient"
)

// newSvc creates an HTTPService suitable for behavioral tests.
func newSvc(timeout time.Duration) httpclient.HTTPService {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	return httpclient.NewHTTPService(timeout, 0, 10*1024*1024, logger, nil)
}

// ─── Basic method dispatch ────────────────────────────────────────────────────

func TestBehavior_AllMethodsDispatchCorrectly(t *testing.T) {
	methods := []struct {
		method string
		call   func(svc httpclient.HTTPService, url string) (*httpclient.HTTPResponse, error)
	}{
		{"GET", func(svc httpclient.HTTPService, url string) (*httpclient.HTTPResponse, error) {
			return svc.Get(context.Background(), url, nil)
		}},
		{"POST", func(svc httpclient.HTTPService, url string) (*httpclient.HTTPResponse, error) {
			return svc.Post(context.Background(), url, nil, "")
		}},
		{"PUT", func(svc httpclient.HTTPService, url string) (*httpclient.HTTPResponse, error) {
			return svc.Put(context.Background(), url, nil, "")
		}},
		{"PATCH", func(svc httpclient.HTTPService, url string) (*httpclient.HTTPResponse, error) {
			return svc.Patch(context.Background(), url, nil, "")
		}},
		{"DELETE", func(svc httpclient.HTTPService, url string) (*httpclient.HTTPResponse, error) {
			return svc.Delete(context.Background(), url, nil)
		}},
	}

	for _, m := range methods {
		t.Run(m.method, func(t *testing.T) {
			var gotMethod string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			svc := newSvc(5 * time.Second)
			_, err := m.call(svc, srv.URL)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotMethod != m.method {
				t.Errorf("expected %s, got %s", m.method, gotMethod)
			}
		})
	}
}

// ─── Headers ──────────────────────────────────────────────────────────────────

func TestBehavior_HeadersForwardedToServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom-Header") != "custom-value" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") != "Bearer my-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newSvc(5 * time.Second)
	resp, err := svc.Get(context.Background(), srv.URL, map[string]string{
		"X-Custom-Header": "custom-value",
		"Authorization":   "Bearer my-token",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, server rejected headers with status %d", resp.StatusCode)
	}
}

// ─── Request body ─────────────────────────────────────────────────────────────

func TestBehavior_RequestBodyForwarded(t *testing.T) {
	const expectedBody = `{"key":"value"}`
	var gotBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newSvc(5 * time.Second)
	_, err := svc.Post(context.Background(), srv.URL, nil, expectedBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody != expectedBody {
		t.Errorf("expected body '%s', got '%s'", expectedBody, gotBody)
	}
}

// ─── Response body ────────────────────────────────────────────────────────────

func TestBehavior_ResponseBodyReturned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"abc123"}`)) // response body
	}))
	defer srv.Close()

	svc := newSvc(5 * time.Second)
	resp, err := svc.Get(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(resp.Body) != `{"id":"abc123"}` {
		t.Errorf("unexpected body: %s", resp.Body)
	}
}

// ─── HTTPResponse interface fields ────────────────────────────────────────────

func TestBehavior_ResponseContainsStatusCodeBodyAndHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Trace-ID", "trace-999")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"created":true}`)) // response body
	}))
	defer srv.Close()

	svc := newSvc(5 * time.Second)
	resp, err := svc.Post(context.Background(), srv.URL, nil, `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}
	if string(resp.Body) != `{"created":true}` {
		t.Errorf("unexpected body: %s", resp.Body)
	}
	if resp.Headers.Get("X-Trace-ID") != "trace-999" {
		t.Errorf("expected X-Trace-ID header, got '%s'", resp.Headers.Get("X-Trace-ID"))
	}
}

// ─── Context propagation ──────────────────────────────────────────────────────

func TestBehavior_ContextCancellation_StopsInFlightRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	svc := newSvc(10 * time.Second) // service timeout much larger than test deadline
	start := time.Now()
	_, err := svc.Get(ctx, srv.URL, nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("cancellation took too long: %v (expected <200ms)", elapsed)
	}
}

// ─── Non-2xx statuses ─────────────────────────────────────────────────────────

func TestBehavior_NonSuccessStatusCodes_NotErrors(t *testing.T) {
	// The httpclient layer returns non-2xx responses as-is.
	// Only the api layer interprets them as errors.
	statuses := []int{400, 401, 403, 404, 422, 500, 503}

	for _, code := range statuses {
		t.Run(http.StatusText(code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(code)
			}))
			defer srv.Close()

			svc := newSvc(5 * time.Second)
			resp, err := svc.Get(context.Background(), srv.URL, nil)
			if err != nil {
				t.Fatalf("status %d: expected no error at httpclient layer, got: %v", code, err)
			}
			if resp.StatusCode != code {
				t.Errorf("expected status %d, got %d", code, resp.StatusCode)
			}
		})
	}
}

// ─── Concurrent requests ──────────────────────────────────────────────────────

func TestBehavior_ConcurrentRequests_AllSucceed(t *testing.T) {
	const concurrent = 20
	var callCount atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newSvc(5 * time.Second)
	done := make(chan error, concurrent)

	for range concurrent {
		go func() {
			_, err := svc.Get(context.Background(), srv.URL, nil)
			done <- err
		}()
	}

	for range concurrent {
		if err := <-done; err != nil {
			t.Errorf("concurrent request failed: %v", err)
		}
	}

	if count := callCount.Load(); count != concurrent {
		t.Errorf("expected %d calls, got %d", concurrent, count)
	}
}
