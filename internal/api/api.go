// Package api implements the authenticated HTTP request layer for the Neo4j Query API.
// It handles URL construction, authentication, and error parsing.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"

	"github.com/neo4j-contrib/query-go-sdk/internal/httpclient"
	"github.com/neo4j-contrib/query-go-sdk/internal/utils"
)

func (b *BasicCredentials) Authorize() string {
	return "Basic " + utils.Base64Encode(b.Username, b.Password)
}

func (s *StaticCredentials) Authorize() string {
	return "Bearer " + s.Token
}

// Error implements the error interface.
func (e *Error) Error() string {
	if len(e.Details) == 0 {
		return fmt.Sprintf("API error (status %d): %s", e.StatusCode, e.Message)
	}

	detail := e.Details[0]
	msg := fmt.Sprintf("API error (status %d): %s - %s", e.StatusCode, e.Message, detail.Message)
	if len(e.Details) > 1 {
		msg += fmt.Sprintf(" (and %d more error(s))", len(e.Details)-1)
	}
	return msg
}

// AllErrors returns all error messages as a slice.
func (e *Error) AllErrors() []string {
	errors := []string{e.Message}
	for _, detail := range e.Details {
		errors = append(errors, detail.Message)
	}
	return errors
}

// HasMultipleErrors returns true if there are multiple error details.
func (e *Error) HasMultipleErrors() bool {
	return len(e.Details) > 1
}

// IsNotFound returns true if the error is a 404.
func (e *Error) IsNotFound() bool {
	return e.StatusCode == http.StatusNotFound
}

// IsUnauthorized returns true if the error is a 401.
func (e *Error) IsUnauthorized() bool {
	return e.StatusCode == http.StatusUnauthorized
}

// IsBadRequest returns true if the error is a 400.
func (e *Error) IsBadRequest() bool {
	return e.StatusCode == http.StatusBadRequest
}

// NewRequestService creates a new RequestService. It constructs its own HTTP
// transport layer internally — callers do not need to know about or create an
// httpclient.
//
// When cfg.HTTPClient is non-nil it is used as the base http.Client inside the
// retryable wrapper, letting callers inject custom transports (mTLS, proxies,
// testing). When nil a default client with production-suitable settings is used.
func NewRequestService(cfg Config, logger *slog.Logger) RequestService {
	httpSvc := httpclient.NewHTTPService(cfg.Timeout, cfg.MaxRetry, cfg.MaxResponseSize, logger, cfg.HTTPClient)

	userAgent := cfg.UserAgent
	// Submit our own user agent when none is given
	if userAgent == "" {
		userAgent = fmt.Sprintf("%s/%s", "query-go-client", cfg.ClientVersion)
	}

	return &apiRequestService{
		httpClient:     httpSvc,
		baseURL:        cfg.BaseURL,
		database:       cfg.Database,
		clientVersion:  cfg.ClientVersion,
		userAgent:      userAgent,
		defaultHeaders: cfg.DefaultHeaders,
		authHeader:     cfg.AuthHeader,
		logger:         logger,
	}
}

// Close releases idle connections held by the underlying HTTP transport by
// delegating to the HTTPService.Close() method. Call this (typically via defer)
// when the RequestService is no longer needed.
func (s *apiRequestService) Close() {
	s.httpClient.Close()
}

// Discover calls the Neo4j discovery endpoint (GET /) and returns server
// metadata. The same Authorization and User-Agent headers are sent as for
// regular query requests.
func (s *apiRequestService) Discover(ctx context.Context) (*DiscoveryResponse, error) {
	if err := ctx.Err(); err != nil {
		s.logger.ErrorContext(ctx, "context already cancelled before discover", slog.String("error", err.Error()))
		return nil, err
	}

	headers := map[string]string{
		"Content-Type":  "application/json",
		"User-Agent":    s.userAgent,
		"Authorization": s.authHeader.Authorize(),
	}

	url := s.baseURL + "/"

	s.logger.DebugContext(ctx, "calling discovery endpoint", slog.String("url", url))

	resp, err := s.httpClient.Get(ctx, url, headers)
	if err != nil {
		s.logger.ErrorContext(ctx, "discovery request failed",
			slog.String("url", url),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseError(resp.Body, resp.StatusCode)
	}

	var discovery DiscoveryResponse
	if err := json.Unmarshal(resp.Body, &discovery); err != nil {
		return nil, fmt.Errorf("discover: unmarshal response: %w", err)
	}

	s.logger.DebugContext(ctx, "discovery response",
		slog.String("neo4j_version", discovery.Neo4jVersion),
		slog.String("neo4j_edition", discovery.Neo4jEdition),
	)

	return &discovery, nil
}

// Get performs an authenticated GET request.
func (s *apiRequestService) Get(ctx context.Context) (*Response, error) {
	return s.doAuthenticatedRequest(ctx, http.MethodGet, "")
}

// Post performs an authenticated POST request.
func (s *apiRequestService) Post(ctx context.Context, body string) (*Response, error) {
	return s.doAuthenticatedRequest(ctx, http.MethodPost, body)
}

// Put performs an authenticated PUT request.
func (s *apiRequestService) Put(ctx context.Context, body string) (*Response, error) {
	return s.doAuthenticatedRequest(ctx, http.MethodPut, body)
}

// Patch performs an authenticated PATCH request.
func (s *apiRequestService) Patch(ctx context.Context, body string) (*Response, error) {
	return s.doAuthenticatedRequest(ctx, http.MethodPatch, body)
}

// Delete performs an authenticated DELETE request.
func (s *apiRequestService) Delete(ctx context.Context) (*Response, error) {
	return s.doAuthenticatedRequest(ctx, http.MethodDelete, "")
}

// doAuthenticatedRequest handles the common pattern of making an authenticated
// API request. It trusts the deadline already set on ctx by the calling service
// layer — no additional timeout is applied here.
func (s *apiRequestService) doAuthenticatedRequest(ctx context.Context, method, body string) (*Response, error) {
	if err := ctx.Err(); err != nil {
		s.logger.ErrorContext(ctx, "context already cancelled before function", slog.String("error", err.Error()))
		return nil, err
	}

	// Start with any caller-supplied default headers, then overwrite with the
	// required protocol headers so they can never be replaced.
	headers := make(map[string]string, len(s.defaultHeaders)+3)
	maps.Copy(headers, s.defaultHeaders)
	headers["Content-Type"] = "application/json"
	headers["Accept"] = "application/vnd.neo4j.query"
	headers["User-Agent"] = s.userAgent
	headers["Authorization"] = s.authHeader.Authorize()

	fullURL := fmt.Sprintf("%s/db/%s/query/v2", s.baseURL, s.database)

	s.logger.DebugContext(ctx, "making authenticated API request",
		slog.String("URL", fullURL),
		slog.String("header content", headers["Content-Type"]),
		slog.String("header auth", headers["Authorization"]),
		slog.String("header accept", headers["Accept"]),
		slog.String("method", method),
		slog.String("body", body),
	)

	var resp *httpclient.HTTPResponse

	var err error

	switch method {
	case http.MethodGet:
		resp, err = s.httpClient.Get(ctx, fullURL, headers)
	case http.MethodPost:
		resp, err = s.httpClient.Post(ctx, fullURL, headers, body)
	case http.MethodPut:
		resp, err = s.httpClient.Put(ctx, fullURL, headers, body)
	case http.MethodPatch:
		resp, err = s.httpClient.Patch(ctx, fullURL, headers, body)
	case http.MethodDelete:
		resp, err = s.httpClient.Delete(ctx, fullURL, headers)
	default:
		return nil, fmt.Errorf("unsupported HTTP method: %s", method)
	}

	if err != nil {
		s.logger.ErrorContext(ctx, "HTTP request failed",
			slog.String("method", method),
			slog.String("endpoint", fullURL),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := parseError(resp.Body, resp.StatusCode)
		s.logger.DebugContext(ctx, "API returned error",
			slog.String("method", method),
			slog.String("endpoint", fullURL),
			slog.Int("statusCode", resp.StatusCode),
			slog.String("message", apiErr.Message),
		)
		return nil, apiErr
	}

	s.logger.DebugContext(ctx, "API request successful",
		slog.String("method", method),
		slog.String("endpoint", fullURL),
		slog.Int("statusCode", resp.StatusCode),
	)

	return &Response{
		StatusCode: resp.StatusCode,
		Body:       resp.Body,
	}, nil
}

// parseError attempts to parse an error response body from the API.
func parseError(responseBody []byte, statusCode int) *Error {
	apiErr := &Error{
		StatusCode: statusCode,
		Message:    http.StatusText(statusCode),
	}

	if len(responseBody) == 0 {
		return apiErr
	}

	var errResponse struct {
		Message string        `json:"message"`
		Errors  []ErrorDetail `json:"errors"`
		Details []ErrorDetail `json:"details"`
	}

	if err := json.Unmarshal(responseBody, &errResponse); err == nil {
		if errResponse.Message != "" {
			apiErr.Message = errResponse.Message
		}
		if len(errResponse.Errors) > 0 {
			apiErr.Details = errResponse.Errors
		} else if len(errResponse.Details) > 0 {
			apiErr.Details = errResponse.Details
		}
	}

	return apiErr
}
