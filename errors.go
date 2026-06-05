package query

import (
	"errors"
	"fmt"

	"github.com/neo4j-contrib/query-go-sdk/internal/api"
)

// Error represents an HTTP error response from the Neo4j Query API.
type Error = api.Error

// ErrorDetail represents individual error details within an Error.
type ErrorDetail = api.ErrorDetail

// VersionError is returned by CheckVersion when the connected Neo4j server is
// older than the minimum supported version.
type VersionError struct {
	// Required is the minimum acceptable CalVer version (e.g. "2026.04.0").
	Required string
	// Got is the version reported by the connected server.
	Got string
}

func (e *VersionError) Error() string {
	return fmt.Sprintf("neo4j version %s is below the minimum required version %s", e.Got, e.Required)
}

// IsNotFound reports whether err is a 404 Not Found API error.
func IsNotFound(err error) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.IsNotFound()
}
