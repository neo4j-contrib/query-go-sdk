package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/neo4j-contrib/query-go-sdk/internal/httpclient"
)

// Response represents a response from the Query API.
type Response struct {
	StatusCode int
	Body       []byte
}

// Error represents an error response from the Query API.
type Error struct {
	StatusCode int           `json:"status_code"`
	Message    string        `json:"message"`
	Details    []ErrorDetail `json:"details,omitempty"`
}

// ErrorDetail represents individual error details.
type ErrorDetail struct {
	Message string `json:"message"`
	Reason  string `json:"reason,omitempty"`
	Field   string `json:"field,omitempty"`
}

// Credentials produces the Authorization header value on demand.
type Credentials interface {
	Authorize() (headerValue string)
}

// basicCredentials uses username+password to set Basic auth on every request.
type BasicCredentials struct {
	Username string
	Password string
}

// staticCredentials holds a pre-supplied bearer token.
type StaticCredentials struct {
	Token string
}

// AccessMode controls the Access-Mode header sent with every legacy-flavor
// API request.
type AccessMode int

const (
	// AccessModeUnset sends no Access-Mode header at all.
	AccessModeUnset AccessMode = iota
	// AccessModeRead sets the Access-Mode header to "READ".
	AccessModeRead
	// AccessModeWrite sets the Access-Mode header to "WRITE".
	AccessModeWrite
)

// Config holds configuration for the API service.
type Config struct {
	AuthHeader      Credentials
	BaseURL         string
	Database        string
	ClientVersion   string
	Timeout         time.Duration
	MaxRetry        int
	UserAgent       string            // e.g. "query-go-sdk/v1.0.0"; defaults to "query-go-client/<version>" if empty
	HTTPClient      *http.Client      // optional custom HTTP client; when non-nil it replaces the default transport
	DefaultHeaders  map[string]string // optional headers merged into every authenticated request
	MaxResponseSize int               // The max size for a response.  Default is 10Mb
	UseLegacyHTTP   bool              // target /db/{db}/tx/commit instead of /db/{db}/query/v2
	AccessMode      AccessMode        // Access-Mode header to send for legacy-flavor requests; unset by default
}

// apiRequestService is the concrete implementation of RequestService.
type apiRequestService struct {
	httpClient     httpclient.HTTPService
	authHeader     Credentials
	baseURL        string
	database       string
	clientVersion  string
	userAgent      string
	defaultHeaders map[string]string
	useLegacyHTTP  bool
	accessMode     AccessMode
	logger         *slog.Logger
}

// Compile-time interface compliance check.
var _ RequestService = (*apiRequestService)(nil)

// DiscoveryResponse holds the fields returned by the Neo4j discovery endpoint (GET /).
type DiscoveryResponse struct {
	Neo4jVersion string `json:"neo4j_version"`
	Neo4jEdition string `json:"neo4j_edition"`
}

// RequestService defines the interface for making authenticated API requests.
// This is the middle layer that handles authentication and common API patterns.
type RequestService interface {
	Post(ctx context.Context, body string) (*Response, error)
	// Discover calls the Neo4j discovery endpoint (GET /) and returns server metadata,
	// including the neo4j_version field used by CheckVersion.
	Discover(ctx context.Context) (*DiscoveryResponse, error)
	// Close releases idle connections held by the underlying HTTP transport.
	// It should be called (typically via defer) when the client is no longer needed.
	Close()
}
