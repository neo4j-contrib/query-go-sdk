// Package utils provides shared internal helpers for the query-go-sdk module.
// Nothing in this package is part of the public API.
package utils

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// Base64Encode returns the standard base64 encoding of "s1:s2", suitable for
// use as the credential in an HTTP Basic Authorization header.
func Base64Encode(s1, s2 string) string {
	auth := s1 + ":" + s2
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

// ParseCalVer parses a CalVer string of the form "YEAR.MONTH.PATCH" (e.g.
// "2026.04.0") into a [3]int. Leading zeros in any component are accepted.
func ParseCalVer(v string) ([3]int, error) {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return [3]int{}, fmt.Errorf("invalid calver %q: expected YEAR.MONTH.PATCH", v)
	}
	labels := [3]string{"year", "month", "patch"}
	var result [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return [3]int{}, fmt.Errorf("invalid calver %q: %s component %q is not a non-negative integer", v, labels[i], p)
		}
		result[i] = n
	}
	return result, nil
}

// CompareCalVer compares two parsed CalVer tuples component by component.
// Returns -1 if a < b, 0 if equal, 1 if a > b.
func CompareCalVer(a, b [3]int) int {
	for i := range 3 {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}
