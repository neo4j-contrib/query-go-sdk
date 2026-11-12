// Package httpclient provides a low-level HTTP client with configurable retry
// behaviour. It is the transport layer beneath internal/api and has no knowledge
// of Aura-specific concepts such as base URLs, API versions, or authentication.
package httpclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"
)

type (
	retryOverrideKey struct{}
	retryBudgetKey   struct{}
)

// retryBudget tracks network retries consumed by one request.
type retryBudget struct{ max, used int }

// WithMaxNetworkRetries returns a context that overrides, for requests made
// with it, how many times a network-level failure (no response received) is
// retried. 0 disables network retries for the call. Negative n is treated as 0.
func WithMaxNetworkRetries(ctx context.Context, n int) context.Context {
	if n < 0 {
		n = 0
	}
	return context.WithValue(ctx, retryOverrideKey{}, n)
}

// networkOnlyRetryPolicy retries only on connection-level errors (e.g. refused,
// reset, DNS failure). HTTP responses — including 5xx — are returned as-is so
// the api layer above can inspect the status code and decide what to do.
func networkOnlyRetryPolicy(ctx context.Context, resp *http.Response, err error) (bool, error) {
	// Context cancelled/deadline exceeded — do not retry.
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	// Network-level error with no HTTP response — retry while the request's
	// budget lasts. The policy is consulted once per failed attempt, so the
	// budget counts retries.
	if err != nil && resp == nil {
		b, ok := ctx.Value(retryBudgetKey{}).(*retryBudget)
		if !ok {
			return true, nil // no budget attached: defer to retryablehttp's RetryMax
		}
		b.used++
		return b.used <= b.max, nil
	}
	// Any actual HTTP response, regardless of status code — do not retry.
	// Status-code interpretation is the responsibility of the api layer.
	return false, nil
}

// NewHTTPService creates a new HTTPService backed by a retryable HTTP client.
// Retries are attempted only on network-level errors (no response received);
// HTTP error responses (including 5xx) are always returned to the caller.
// The caller-supplied logger is used for debug output.
//
// When customClient is non-nil it is used as the base http.Client inside the
// retryable wrapper (replacing the default transport). When nil the service
// constructs a default client with production-suitable connection pool settings.
func NewHTTPService(timeout time.Duration, maxRetry int, maxResponse int, logger *slog.Logger, customClient *http.Client) HTTPService {
	retryClient := retryablehttp.NewClient()
	// The per-request budget in networkOnlyRetryPolicy decides when to stop;
	// RetryMax is only a ceiling so a per-call override above the client
	// default is honoured.
	retryClient.RetryMax = math.MaxInt32
	retryClient.RetryWaitMin = 1 * time.Second
	retryClient.RetryWaitMax = 5 * time.Second
	retryClient.Logger = nil // suppress retryablehttp's own logger; we use slog
	retryClient.CheckRetry = networkOnlyRetryPolicy

	if customClient != nil {
		// Use the caller's client as-is. The caller is responsible for setting
		// a Timeout on the provided *http.Client; the cfg.Timeout value is not
		// applied automatically when a custom client is supplied.
		retryClient.HTTPClient = customClient
	} else {
		// Configure an explicit transport with production-suitable connection pool
		// settings. Go's default transport caps MaxIdleConnsPerHost at 2, which
		// causes connection exhaustion under concurrent load since all requests go
		// to the same host. These values are sized for a typical management-plane
		// workload; tune MaxIdleConnsPerHost upward if you issue many parallel calls.
		// Although Go 1.18+ defaults to TLS 1.2, it's non-obvious so we explicity set min TLS to 1.2
		retryClient.HTTPClient = &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   20,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
			},
		}
	}

	return &httpService{
		timeout:         timeout,
		client:          retryClient,
		logger:          logger,
		maxResponseSize: maxResponse,
		defaultRetry:    maxRetry,
	}
}

// Close releases idle connections held by the underlying HTTP transport.
// It delegates to http.Client.CloseIdleConnections on the retryablehttp client's
// inner http.Client, draining the connection pool. Call this (typically via defer)
// when the service is no longer needed.
func (s *httpService) Close() {
	s.client.HTTPClient.CloseIdleConnections()
}

// Get performs an HTTP GET request with the provided headers.
func (s *httpService) Get(ctx context.Context, url string, headers map[string]string) (*HTTPResponse, error) {
	return s.doRequest(ctx, http.MethodGet, url, headers, nil)
}

// Post performs an HTTP POST request with the provided headers and body.
func (s *httpService) Post(ctx context.Context, url string, headers map[string]string, body []byte) (*HTTPResponse, error) {
	return s.doRequest(ctx, http.MethodPost, url, headers, body)
}

// PostStream performs an HTTP POST request and returns the response with its
// body unread, for incremental (streaming) consumption by the caller.
func (s *httpService) PostStream(ctx context.Context, url string, headers map[string]string, body []byte) (*StreamResponse, error) {
	resp, err := s.executeRequest(ctx, http.MethodPost, url, headers, body)
	if err != nil {
		return nil, err
	}

	return &StreamResponse{
		StatusCode: resp.StatusCode,
		Body:       newLimitedReadCloser(resp.Body, s.maxResponseSize),
		Headers:    resp.Header,
	}, nil
}

// Put performs an HTTP PUT request with the provided headers and body.
func (s *httpService) Put(ctx context.Context, url string, headers map[string]string, body []byte) (*HTTPResponse, error) {
	return s.doRequest(ctx, http.MethodPut, url, headers, body)
}

// Patch performs an HTTP PATCH request with the provided headers and body.
func (s *httpService) Patch(ctx context.Context, url string, headers map[string]string, body []byte) (*HTTPResponse, error) {
	return s.doRequest(ctx, http.MethodPatch, url, headers, body)
}

// Delete performs an HTTP DELETE request with the provided headers.
func (s *httpService) Delete(ctx context.Context, url string, headers map[string]string) (*HTTPResponse, error) {
	return s.doRequest(ctx, http.MethodDelete, url, headers, nil)
}

// executeRequest builds the request, attaches headers and the caller's
// context, and executes it via the retryable client. The caller owns
// resp.Body and must close it. Shared by doRequest (which buffers the body)
// and PostStream (which hands the live body to the caller).
func (s *httpService) executeRequest(ctx context.Context, method, url string, headers map[string]string, body []byte) (*http.Response, error) {
	// Passing the []byte straight to retryablehttp avoids copying a (possibly
	// multi-MB) body and lets it be replayed on each retry attempt.
	var bodyReader any
	if len(body) > 0 {
		bodyReader = body
	}

	// Each request gets its own network-retry budget: the per-call override
	// from WithMaxNetworkRetries if present, else the client default.
	budget := &retryBudget{max: s.defaultRetry}
	if n, ok := ctx.Value(retryOverrideKey{}).(int); ok {
		budget.max = n
	}
	ctx = context.WithValue(ctx, retryBudgetKey{}, budget)

	req, err := retryablehttp.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req = req.WithContext(ctx)

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	s.logger.DebugContext(ctx, "executing HTTP request",
		slog.String("method", method),
		slog.String("url", url),
	)

	return s.client.Do(req)
}

// doRequest is the shared implementation for all buffered HTTP methods. It
// executes the request and reads the response body up to DefaultMaxResponseSize.
func (s *httpService) doRequest(ctx context.Context, method, url string, headers map[string]string, body []byte) (*HTTPResponse, error) {
	resp, err := s.executeRequest(ctx, method, url, headers, body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	// We want to read just beyond our maxResponseSize.
	// as response body = maxResponseSize is valid
	// We can then see if the response is larger than max and return an error if it is
	limitedReader := io.LimitReader(resp.Body, int64(s.maxResponseSize+1024))
	responseBody, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if len(responseBody) > s.maxResponseSize {
		return nil, fmt.Errorf("response body size exceeds maximum size of %d bytes", s.maxResponseSize)
	}

	s.logger.DebugContext(ctx, "HTTP response received",
		slog.String("method", method),
		slog.String("url", url),
		slog.Int("status", resp.StatusCode),
		slog.Int("response body size", len(responseBody)),
	)

	return &HTTPResponse{
		StatusCode: resp.StatusCode,
		Body:       responseBody,
		Headers:    resp.Header,
	}, nil
}
