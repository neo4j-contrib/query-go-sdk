// Package query provides a Go client for the Neo4j Query API.
//
// Execute Cypher queries against a Neo4j database over plain HTTP using the
// typed JSON wire format. No Bolt protocol, no binary dependencies.
//
// Basic usage:
//
//	client, err := query.NewClient(
//	    query.WithBasicAuth("neo4j", "password"),
//	    query.WithBaseURL("http://localhost:7474"),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close()
//
//	result, err := query.WithTransformer(
//	    client.Query,
//	    context.Background(),
//	    "MATCH (n:Person) RETURN n.name AS name LIMIT 10",
//	    nil,
//	    query.EagerResultTransformer,
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for _, rec := range result.Records {
//	    name, _ := rec.GetString("name")
//	    fmt.Println(name)
//	}

package query

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/neo4j-contrib/query-go-sdk/internal/api"
	"github.com/neo4j-contrib/query-go-sdk/internal/utils"
)

// ============================================================================
// Constants and version
// ============================================================================

// clientVersionFallback is embedded in the User-Agent when the real module version cannot
// be determined (local builds, go test, go run). It is intentionally kept as "development"
// in source — there is no need to update it before tagging a release.
const clientVersionFallback = "development"

// moduleName is this module's own import path, matching the `module` directive in go.mod.
// It identifies this SDK's own entry in a consumer's dependency list — see resolveClientVersion.
const moduleName = "github.com/neo4j-contrib/query-go-sdk"

// ClientVersion is the version of this client library, embedded in every User-Agent header.
//
// Why debug.ReadBuildInfo()?
// Go consumers import this library by source (via the module proxy). There are no compiled
// binaries to stamp at build time. When a consumer builds their application, the Go toolchain
// records all module dependencies and their exact versions in the binary. debug.ReadBuildInfo()
// reads that information at runtime, so the User-Agent automatically reflects the version the
// consumer actually imported (e.g. "v1.10.0") without any source edits or workflow tricks.
//
// Why info.Deps and not info.Main?
// info.Main describes the consumer's own application module, not this SDK — its Version is
// "(devel)" for essentially any normal `go build` regardless of which SDK version was imported.
// This SDK's own version only appears in info.Deps, keyed by module path, which is what
// resolveClientVersion looks up.
//
// In local and test builds (where this module is its own main module and so never appears in
// its own Deps) or if the lookup otherwise comes up empty, we fall back to clientVersionFallback
// ("development") to make it obvious the binary is not tracking a release.
var ClientVersion = clientVersionFallback

func init() {
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := resolveClientVersion(info); v != "" {
			ClientVersion = v
		}
	}
}

// resolveClientVersion finds this module's own version in a consumer's build info by scanning
// info.Deps for moduleName. A replaced dependency's effective version comes from Replace,
// matching how `go list -m` reports it — this is also how a local filesystem `replace` (no
// tagged version to report) surfaces, as the literal string "(devel)", so that value is treated
// the same as "not found" here, just as it always has been for info.Main.Version. Returns "" if
// this module isn't found at all (e.g. it is the main module itself, as in this module's own
// tests) or only resolves to "(devel)".
func resolveClientVersion(info *debug.BuildInfo) string {
	for _, dep := range info.Deps {
		if dep.Path != moduleName {
			continue
		}
		version := dep.Version
		if dep.Replace != nil {
			version = dep.Replace.Version
		}
		if version == "(devel)" {
			return ""
		}
		return version
	}
	return ""
}

// ============================================================================
// API flavour
// ============================================================================

// APIFlavor selects which Neo4j HTTP API the client will target.
type APIFlavor int

const (
	// FlavorQueryV2 targets the modern Query API v2 endpoint (/db/{db}/query/v2).
	// This is the default.
	FlavorQueryV2 APIFlavor = iota
	// FlavorLegacyHTTP targets the older Cypher HTTP Transaction API
	// endpoint (/db/{db}/tx/commit). Use this for comparison testing against
	// Neo4j versions that pre-date the Query API.
	FlavorLegacyHTTP
)

// ============================================================================
// Access mode
// ============================================================================

// AccessMode controls the accessMode sent with every API request.
type AccessMode int

const (
	// AccessModeUnset sends no accessMode at all. This is the default: the
	// server-side router forwards the request to whichever cluster member
	// can handle it — any member for a read, the leader for a write.
	AccessModeUnset AccessMode = iota
	// AccessModeRead sets the accessMode to "read".
	AccessModeRead
	// AccessModeWrite sets the accessMode to "write".
	AccessModeWrite
)

// ============================================================================
// Client types
// ============================================================================

// QueryAPIClient is the main client for the Neo4j Query API.
//
//nolint:revive // QueryAPIClient is intentional: the package is named query and the type name is established in v1.
type QueryAPIClient struct {
	api    api.RequestService // Handles authenticated API requests
	logger *slog.Logger       // Structured logger

	// Grouped services — using interface types for testability.
	Query QueryService
}

// config holds internal configuration (unexported).
type config struct {
	baseURL         string            // the base URL of neo4j server
	apiTimeout      time.Duration     // how long to wait for a response from the Query API endpoint
	apiRetryMax     int               // the number of retries to attempt
	authHeader      api.Credentials   // The auth header value to use
	database        string            // database
	httpClient      *http.Client      // optional custom HTTP client (injected transport)
	userAgent       string            // optional User-Agent override
	defaultHeaders  map[string]string // optional headers added to every API request
	maxResponseSize int               // optional max response size in bytes
	clientVersion   string            // the version of this query client
	flavor          APIFlavor         // which HTTP API endpoint to target
	accessMode      AccessMode        // which accessMode to send with every request; unset by default
	streaming       bool              // whether ExecuteStream is enabled
}

// Option is a functional option for configuring the AuraAPIClient.
type Option func(*options) error

// options holds the configuration that will be applied to the client.
type options struct {
	config config
	logger *slog.Logger
}

// ============================================================================
// Constructor and options
// ============================================================================

// defaultOptions returns options with sensible defaults.
func defaultOptions() *options {
	opts := &slog.HandlerOptions{Level: slog.LevelWarn}
	handler := slog.NewTextHandler(os.Stderr, opts)

	return &options{
		config: config{
			baseURL:         "http://localhost:7474",
			database:        "neo4j",
			apiTimeout:      120 * time.Second,
			apiRetryMax:     3,
			clientVersion:   ClientVersion,
			userAgent:       "query-go-sdk/" + ClientVersion,
			maxResponseSize: 10 * 1024 * 1024, // This is 10mb
		},
		logger: slog.New(handler),
	}
}

func WithBasicAuth(username, password string) Option {
	return func(o *options) error {
		if o.config.authHeader != nil {
			return errors.New("auth already set: WithBasicAuth and WithBearerToken are mutually exclusive")
		}
		o.config.authHeader = &api.BasicCredentials{Username: username, Password: password}
		return nil
	}
}

func WithBearerToken(token string) Option {
	return func(o *options) error {
		if o.config.authHeader != nil {
			return errors.New("auth already set: WithBasicAuth and WithBearerToken are mutually exclusive")
		}
		o.config.authHeader = &api.StaticCredentials{Token: token}
		return nil
	}
}

// WithDatabase
func WithDatabase(database string) Option {
	return func(o *options) error {
		// check database is not empty
		if database == "" {
			return errors.New("database must not be empty")
		}
		o.config.database = database
		return nil
	}
}

// WithTimeout sets a custom API timeout. Defaults to 120 seconds.
func WithTimeout(timeout time.Duration) Option {
	return func(o *options) error {
		if timeout <= 0 {
			return errors.New("timeout must be greater than zero")
		}
		o.config.apiTimeout = timeout
		return nil
	}
}

// WithMaxRetry sets the maximum number of retries for failed requests. Defaults to 3.
func WithMaxRetry(maxRetry int) Option {
	return func(o *options) error {
		if maxRetry <= 0 {
			return errors.New("max retries must be greater than zero")
		}
		o.config.apiRetryMax = maxRetry
		return nil
	}
}

// WithMaxResponseSize sets the maximum size for  response.  Default is 10mb
func WithMaxResponseSize(maxResponse int) Option {
	return func(o *options) error {
		if maxResponse <= 0 {
			return errors.New("max response size must be greater than zero")
		}
		o.config.maxResponseSize = maxResponse
		return nil
	}
}

// WithLogger sets a custom slog.Logger. Defaults to warn-level logging to stderr.
func WithLogger(logger *slog.Logger) Option {
	return func(o *options) error {
		if logger == nil {
			return errors.New("logger cannot be nil")
		}
		o.logger = logger
		return nil
	}
}

// WithBaseURL overrides the default base URL of the Neo4j server.
// The SDK does not enforce HTTPS — it is the caller's responsibility to use
// an appropriate scheme for their deployment. Use HTTPS for any server that
// is not on a trusted loopback interface.
func WithBaseURL(baseURL string) Option {
	return func(o *options) error {
		if baseURL == "" {
			return errors.New("base URL must not be empty")
		}
		o.config.baseURL = baseURL
		return nil
	}
}

// WithHTTPClient sets a custom *http.Client to use for all API requests. This
// lets callers inject a custom transport (e.g. for mTLS, proxies, or testing).
// Returns an error if client is nil.
//
// Note: when a custom client is supplied, WithTimeout has no effect. The caller
// is responsible for setting a Timeout on the provided *http.Client directly.
func WithHTTPClient(client *http.Client) Option {
	return func(o *options) error {
		if client == nil {
			return errors.New("HTTP client cannot be nil")
		}
		o.config.httpClient = client
		return nil
	}
}

// WithUserAgent overrides the default User-Agent header sent with every request.
// Returns an error if ua is empty.
func WithUserAgent(ua string) Option {
	return func(o *options) error {
		if ua == "" {
			return errors.New("user agent must not be empty")
		}
		o.config.userAgent = ua
		return nil
	}
}

// protectedHeaders is the set of header keys that WithDefaultHeaders silently
// drops to prevent callers from inadvertently overriding security-sensitive or
// protocol-critical headers.
var protectedHeaders = map[string]struct{}{
	"authorization": {},
	"content-type":  {},
	"accept":        {},
	"user-agent":    {},
}

// WithDefaultHeaders adds the given headers to every API request. It is a no-op
// when headers is nil or empty. Keys matching Authorization, Content-Type,
// Accept, or User-Agent (case-insensitive) are rejected with an error to prevent
// callers from inadvertently overriding security-sensitive or protocol-critical
// headers.
func WithDefaultHeaders(headers map[string]string) Option {
	return func(o *options) error {
		if len(headers) == 0 {
			return nil
		}
		for k := range headers {
			if _, protected := protectedHeaders[strings.ToLower(k)]; protected {
				return fmt.Errorf("WithDefaultHeaders: %q is a protected header and cannot be overridden; use WithBasicAuth, WithBearerToken, or WithUserAgent instead", k)
			}
		}
		filtered := make(map[string]string, len(headers))
		for k, v := range headers {
			filtered[k] = v
		}
		o.config.defaultHeaders = filtered
		return nil
	}
}

// WithAPIFlavor selects which Neo4j HTTP API endpoint the client targets.
// Use FlavorLegacyHTTP to target the older Cypher HTTP Transaction API
// (/db/{db}/tx/commit) for comparison testing. Defaults to FlavorQueryV2.
func WithAPIFlavor(flavor APIFlavor) Option {
	return func(o *options) error {
		o.config.flavor = flavor
		return nil
	}
}

// WithAccessMode sets the accessMode sent with every API request. Use
// AccessModeRead for read-only workloads to allow the server to route the
// request to a read replica, or AccessModeWrite to force leader routing.
// Defaults to AccessModeUnset, which sends no accessMode at all and lets the
// server-side router forward the request to whichever cluster member can
// handle it.
func WithAccessMode(mode AccessMode) Option {
	return func(o *options) error {
		if mode != AccessModeUnset && mode != AccessModeWrite && mode != AccessModeRead {
			return fmt.Errorf("invalid access mode: %d", mode)
		}
		o.config.accessMode = mode
		return nil
	}
}

// WithStreamingSupport enables the Query API's streaming (JSON Lines)
// response format, which unlocks QueryService.ExecuteStream for incremental
// record consumption instead of buffering the entire response. Execute is
// unaffected either way. Defaults to disabled.
//
// Not supported together with WithAPIFlavor(FlavorLegacyHTTP): the legacy
// Cypher HTTP Transaction API does not support streaming. NewClient returns
// an error if both are set.
func WithStreamingSupport(enabled bool) Option {
	return func(o *options) error {
		o.config.streaming = enabled
		return nil
	}
}

// Close drains idle HTTP connections held by the underlying transport. It
// should be called via defer when the client is no longer needed to avoid
// leaking file descriptors.
//
//	client, err := query.NewClient(query.WithBasicAuth("neo4j", "password"))
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close()
func (c *QueryAPIClient) Close() {
	c.api.Close()
}

// minNeo4jVersion is the minimum Neo4j server version this client supports.
const minNeo4jVersion = "2026.04.0"

// CheckVersion calls the Neo4j discovery endpoint and returns a *VersionError
// if the server version is older than 2026.04.0. Call this after NewClient
// when you want to validate the connected server before executing queries.
//
//	client, err := query.NewClient(query.WithBasicAuth("neo4j", "password"))
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close()
//
//	if err := client.CheckVersion(ctx); err != nil {
//	    var verErr *query.VersionError
//	    if errors.As(err, &verErr) {
//	        log.Fatalf("server too old: got %s, need %s", verErr.Got, verErr.Required)
//	    }
//	    log.Fatal(err)
//	}
func (c *QueryAPIClient) CheckVersion(ctx context.Context) error {
	discovery, err := c.api.Discover(ctx)
	if err != nil {
		return fmt.Errorf("CheckVersion: %w", err)
	}

	if discovery.Neo4jVersion == "" {
		return fmt.Errorf("CheckVersion: neo4j_version missing from discovery response")
	}

	got, err := utils.ParseCalVer(discovery.Neo4jVersion)
	if err != nil {
		return fmt.Errorf("CheckVersion: %w", err)
	}

	min, _ := utils.ParseCalVer(minNeo4jVersion) // constant — cannot fail

	if utils.CompareCalVer(got, min) < 0 {
		return &VersionError{Required: minNeo4jVersion, Got: discovery.Neo4jVersion}
	}

	c.logger.Info("neo4j version check passed", slog.String("version", discovery.Neo4jVersion))
	return nil
}

// NewClient creates a new Query API client with functional options.
func NewClient(opts ...Option) (*QueryAPIClient, error) {
	// set the default options.  These will be overridden where this is a supplied option
	o := defaultOptions()

	for _, opt := range opts {
		if err := opt(o); err != nil {
			o.logger.Error("option application failed", slog.String("error", err.Error()))
			return nil, err
		}
	}

	if o.config.authHeader == nil {
		o.logger.Error("validation failed", slog.String("reason", "username/ password or Token must be given"))
		return nil, errors.New("username must not be empty")
	}

	if o.config.baseURL == "" {
		o.logger.Error("validation failed", slog.String("reason", "base URL must not be empty"))
		return nil, errors.New("base URL must not be empty")
	}
	if o.config.apiTimeout <= 0 {
		o.logger.Error("validation failed", slog.String("reason", "API timeout must be greater than zero"), slog.Duration("timeout", o.config.apiTimeout))
		return nil, errors.New("API timeout must be greater than zero")
	}

	if o.config.streaming && o.config.flavor == FlavorLegacyHTTP {
		o.logger.Error("validation failed", slog.String("reason", "WithStreamingSupport is not supported with FlavorLegacyHTTP"))
		return nil, errors.New("WithStreamingSupport is not supported with FlavorLegacyHTTP")
	}

	// User-Agent is required for usage analysis. WithUserAgent can override the default.
	if o.config.userAgent == "" {
		o.logger.Error("validation failed", slog.String("reason", "User agent cannot be empty"))
		return nil, errors.New("user agent cannot be empty")
	}

	o.logger.Debug("configuration validated",
		slog.String("baseURL", o.config.baseURL),
		slog.String("apiVersion", ClientVersion),
		slog.Duration("apiTimeout", o.config.apiTimeout),
	)

	var apiAccessMode api.AccessMode
	switch o.config.accessMode {
	case AccessModeRead:
		apiAccessMode = api.AccessModeRead
	case AccessModeWrite:
		apiAccessMode = api.AccessModeWrite
	default:
		apiAccessMode = api.AccessModeUnset
	}

	apiSvc := api.NewRequestService(api.Config{
		AuthHeader:      o.config.authHeader,
		BaseURL:         o.config.baseURL,
		Database:        o.config.database,
		ClientVersion:   ClientVersion,
		Timeout:         o.config.apiTimeout,
		MaxRetry:        o.config.apiRetryMax,
		UserAgent:       o.config.userAgent,
		HTTPClient:      o.config.httpClient,
		DefaultHeaders:  o.config.defaultHeaders,
		MaxResponseSize: o.config.maxResponseSize,
		UseLegacyHTTP:   o.config.flavor == FlavorLegacyHTTP,
		AccessMode:      apiAccessMode,
	}, o.logger)

	clientLogger := o.logger.With(slog.String("component", "QueryAPIClient"))

	service := &QueryAPIClient{
		api:    apiSvc,
		logger: clientLogger,
	}

	service.Query = &queryService{
		api:              apiSvc,
		timeout:          o.config.apiTimeout,
		logger:           clientLogger.With(slog.String("service", "queryService")),
		useLegacyHTTP:    o.config.flavor == FlavorLegacyHTTP,
		accessMode:       o.config.accessMode,
		streamingEnabled: o.config.streaming,
		maxResponseSize:  o.config.maxResponseSize,
	}

	service.logger.Info("Query API client initialized successfully",
		slog.String("sdk version", ClientVersion),
	)

	return service, nil
}
