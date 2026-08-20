package decode

import (
	"encoding/base64"
	"fmt"
	"github.com/goccy/go-json"
	"math"
	"strconv"
	"strings"
	"time"
)

// ============================================================================
// Wire types — unexported, used only during JSON unmarshalling
// ============================================================================

// typedValue is the envelope wrapping every value in a typed JSON response:
//
//	{"$type": "String", "_value": "hello"}
//	{"$type": "Node",   "_value": { ... }}
type typedValue struct {
	Type  string          `json:"$type"`
	Value json.RawMessage `json:"_value"`
}

// wireResponse is the raw top-level response shape before decoding. QueryType
// and the timing fields mirror wireStreamSummaryBody (stream.go) — Neo4j sends
// the same execution metadata on both the buffered and streaming response
// formats.
type wireResponse struct {
	Data struct {
		Fields []string            `json:"fields"`
		Values [][]json.RawMessage `json:"values"`
	} `json:"data"`
	QueryPlan            *wirePlanOperator `json:"queryPlan,omitempty"`
	Notifications        []Notification    `json:"notifications,omitempty"`
	Bookmarks            []string          `json:"bookmarks"`
	Errors               []QueryError      `json:"errors,omitempty"`
	QueryType            string            `json:"queryType,omitempty"`
	ResultAvailableAfter int64             `json:"resultAvailableAfter,omitempty"`
	ResultConsumedAfter  int64             `json:"resultConsumedAfter,omitempty"`
}

// wirePlanOperator is the raw shape of one node in the query plan tree.
type wirePlanOperator struct {
	OperatorType string                     `json:"operatorType"`
	Arguments    map[string]json.RawMessage `json:"arguments"`
	Identifiers  []string                   `json:"identifiers"`
	Children     []wirePlanOperator         `json:"children"`
}

// wireNode is the raw shape of a Node _value.
type wireNode struct {
	ElementID  string                     `json:"_element_id"`
	Labels     []string                   `json:"_labels"`
	Properties map[string]json.RawMessage `json:"_properties"`
}

// wireRelationship is the raw shape of a Relationship _value.
type wireRelationship struct {
	ElementID          string                     `json:"_element_id"`
	StartNodeElementID string                     `json:"_start_node_element_id"`
	EndNodeElementID   string                     `json:"_end_node_element_id"`
	Type               string                     `json:"_type"`
	Properties         map[string]json.RawMessage `json:"_properties"`
}

// wireVector is the raw shape of a Vector _value.
type wireVector struct {
	CoordinatesType CoordinateType `json:"coordinatesType"`
	Coordinates     []string       `json:"coordinates"`
}

// ============================================================================
// Public entry points
// ============================================================================

// Response decodes a raw Query API response body into a Response.
//
// If Neo4j returned errors (syntax errors, schema errors, etc.) a *QueryErrors
// is returned and the Response is nil. Callers should check with errors.As:
//
//	resp, err := decode.Response(body)
//	if err != nil {
//	    var qErr *decode.QueryErrors
//	    if errors.As(err, &qErr) {
//	        // inspect qErr.Errors
//	    }
//	    return err
//	}
func DecodeResponse(body []byte) (*Response, error) {
	var wire wireResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("decode: unmarshal response body: %w", err)
	}

	// Neo4j errors are a distinct response shape — surface immediately.
	if len(wire.Errors) > 0 {
		return nil, &QueryErrors{Errors: wire.Errors}
	}

	// Decode data rows.
	rows := make([][]any, len(wire.Data.Values))
	for i, rawRow := range wire.Data.Values {
		row := make([]any, len(rawRow))
		for j, rawCell := range rawRow {
			v, err := DecodeValue(rawCell)
			if err != nil {
				col := ""
				if j < len(wire.Data.Fields) {
					col = wire.Data.Fields[j]
				}
				return nil, fmt.Errorf("decode: row %d col %d (%s): %w", i, j, col, err)
			}
			row[j] = v
		}
		rows[i] = row
	}

	resp := &Response{
		Fields:               wire.Data.Fields,
		Rows:                 rows,
		Notifications:        wire.Notifications,
		Bookmarks:            wire.Bookmarks,
		QueryType:            wire.QueryType,
		ResultAvailableAfter: time.Duration(wire.ResultAvailableAfter) * time.Millisecond,
		ResultConsumedAfter:  time.Duration(wire.ResultConsumedAfter) * time.Millisecond,
	}

	// Decode query plan if present (EXPLAIN / PROFILE responses).
	if wire.QueryPlan != nil {
		plan, err := decodePlanOperator(*wire.QueryPlan)
		if err != nil {
			return nil, fmt.Errorf("decode: query plan: %w", err)
		}
		resp.QueryPlan = plan
	}

	return resp, nil
}

// DecodeValue decodes a single typed JSON envelope into the appropriate Go type.
//
// The returned value will be one of:
//
//	nil                   (Null)
//	bool                  (Boolean)
//	int64                 (Integer)
//	float64               (Float, including math.NaN / math.Inf)
//	string                (String)
//	[]byte                (Base64)
//	[]any                 (List — elements recursively decoded)
//	map[string]any        (Map  — values recursively decoded)
//	time.Time             (Date, Time, LocalTime, OffsetDateTime, LocalDateTime)
//	Duration              (Duration)
//	Point                 (Point)
//	*Node                 (Node)
//	*Relationship         (Relationship)
//	Path                  (Path)
//	Vector                (Vector)
//	Unsupported           (Unsupported)
func DecodeValue(raw json.RawMessage) (any, error) {
	// Bare JSON null — emitted in plain JSON mode or as a property value.
	if isNull(raw) {
		return nil, nil
	}

	var tv typedValue
	if err := json.Unmarshal(raw, &tv); err != nil {
		// Not a typed envelope — fall back to plain JSON scalar.
		// This handles plain JSON mode responses.
		var plain any
		if err2 := json.Unmarshal(raw, &plain); err2 != nil {
			return nil, fmt.Errorf("decode value: %w", err2)
		}
		return plain, nil
	}

	switch tv.Type {
	case "Null":
		return nil, nil

	case "Boolean":
		var b bool
		if err := json.Unmarshal(tv.Value, &b); err != nil {
			return nil, fmt.Errorf("decode Boolean: %w", err)
		}
		return b, nil

	case "Integer":
		s, err := unquoteString(tv.Value, "Integer")
		if err != nil {
			return nil, err
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("decode Integer %q: %w", s, err)
		}
		return n, nil

	case "Float":
		s, err := unquoteString(tv.Value, "Float")
		if err != nil {
			return nil, err
		}
		// The API emits NaN and ±Infinity as strings; strconv does not handle them.
		switch s {
		case "NaN":
			return math.NaN(), nil
		case "Infinity":
			return math.Inf(1), nil
		case "-Infinity":
			return math.Inf(-1), nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("decode Float %q: %w", s, err)
		}
		return f, nil

	case "String":
		return unquoteString(tv.Value, "String")

	case "Base64":
		s, err := unquoteString(tv.Value, "Base64")
		if err != nil {
			return nil, err
		}
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("decode Base64 bytes: %w", err)
		}
		return b, nil

	case "List":
		return decodeList(tv.Value)

	case "Map":
		return decodeMap(tv.Value)

	case "Date":
		return parseTime(tv.Value, layoutDate, "Date")

	case "Time":
		return parseTime(tv.Value, layoutTime, "Time")

	case "LocalTime":
		return parseTime(tv.Value, layoutLocalTime, "LocalTime")

	case "OffsetDateTime":
		return parseTime(tv.Value, time.RFC3339Nano, "OffsetDateTime")

	case "LocalDateTime":
		return parseTime(tv.Value, layoutLocalDateTime, "LocalDateTime")

	case "Duration":
		s, err := unquoteString(tv.Value, "Duration")
		if err != nil {
			return nil, err
		}
		return parseDuration(s)

	case "Point":
		s, err := unquoteString(tv.Value, "Point")
		if err != nil {
			return nil, err
		}
		return parsePoint(s)

	case "Node":
		return decodeNode(tv.Value)

	case "Relationship":
		return decodeRelationship(tv.Value)

	case "Path":
		return decodePath(tv.Value)

	case "Vector":
		return decodeVector(tv.Value)

	case "Unsupported":
		s, err := unquoteString(tv.Value, "Unsupported")
		if err != nil {
			return nil, err
		}
		return Unsupported{Message: s}, nil

	default:
		return nil, fmt.Errorf("decode: unknown $type %q", tv.Type)
	}
}

// ============================================================================
// Collection decoders
// ============================================================================

func decodeList(raw json.RawMessage) ([]any, error) {
	var raws []json.RawMessage
	if err := json.Unmarshal(raw, &raws); err != nil {
		return nil, fmt.Errorf("decode List: %w", err)
	}
	result := make([]any, len(raws))
	for i, r := range raws {
		v, err := DecodeValue(r)
		if err != nil {
			return nil, fmt.Errorf("decode List[%d]: %w", i, err)
		}
		result[i] = v
	}
	return result, nil
}

func decodeMap(raw json.RawMessage) (map[string]any, error) {
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawMap); err != nil {
		return nil, fmt.Errorf("decode Map: %w", err)
	}
	result := make(map[string]any, len(rawMap))
	for k, r := range rawMap {
		v, err := DecodeValue(r)
		if err != nil {
			return nil, fmt.Errorf("decode Map[%q]: %w", k, err)
		}
		result[k] = v
	}
	return result, nil
}

// ============================================================================
// Graph entity decoders
// ============================================================================

func decodeNode(raw json.RawMessage) (*Node, error) {
	var w wireNode
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, fmt.Errorf("decode Node: %w", err)
	}
	props, err := decodeProperties(w.Properties)
	if err != nil {
		return nil, fmt.Errorf("decode Node properties: %w", err)
	}
	return &Node{
		ElementID:  w.ElementID,
		Labels:     w.Labels,
		Properties: props,
	}, nil
}

func decodeRelationship(raw json.RawMessage) (*Relationship, error) {
	var w wireRelationship
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, fmt.Errorf("decode Relationship: %w", err)
	}
	props, err := decodeProperties(w.Properties)
	if err != nil {
		return nil, fmt.Errorf("decode Relationship properties: %w", err)
	}
	return &Relationship{
		ElementID:          w.ElementID,
		StartNodeElementID: w.StartNodeElementID,
		EndNodeElementID:   w.EndNodeElementID,
		Type:               w.Type,
		Properties:         props,
	}, nil
}

// decodePath decodes a PATH value, which is a JSON array of alternating
// Node and Relationship typed envelopes. Direction is recoverable only from
// StartNodeElementID / EndNodeElementID on each relationship.
func decodePath(raw json.RawMessage) (Path, error) {
	var segments []json.RawMessage
	if err := json.Unmarshal(raw, &segments); err != nil {
		return Path{}, fmt.Errorf("decode Path: %w", err)
	}

	var path Path
	for i, seg := range segments {
		v, err := DecodeValue(seg)
		if err != nil {
			return Path{}, fmt.Errorf("decode Path[%d]: %w", i, err)
		}
		switch t := v.(type) {
		case *Node:
			path.Nodes = append(path.Nodes, t)
		case *Relationship:
			path.Relationships = append(path.Relationships, t)
		default:
			return Path{}, fmt.Errorf("decode Path[%d]: unexpected segment type %T", i, v)
		}
	}
	return path, nil
}

func decodeVector(raw json.RawMessage) (Vector, error) {
	var w wireVector
	if err := json.Unmarshal(raw, &w); err != nil {
		return Vector{}, fmt.Errorf("decode Vector: %w", err)
	}
	return Vector(w), nil
}

func decodeProperties(raw map[string]json.RawMessage) (map[string]any, error) {
	result := make(map[string]any, len(raw))
	for k, v := range raw {
		decoded, err := DecodeValue(v)
		if err != nil {
			return nil, fmt.Errorf("property %q: %w", k, err)
		}
		result[k] = decoded
	}
	return result, nil
}

// ============================================================================
// Plan operator decoder
// ============================================================================

func decodePlanOperator(w wirePlanOperator) (*PlanOperator, error) {
	args := make(map[string]any, len(w.Arguments))
	for k, raw := range w.Arguments {
		v, err := DecodeValue(raw)
		if err != nil {
			return nil, fmt.Errorf("plan operator %q argument %q: %w", w.OperatorType, k, err)
		}
		args[k] = v
	}

	children := make([]*PlanOperator, len(w.Children))
	for i, child := range w.Children {
		decoded, err := decodePlanOperator(child)
		if err != nil {
			return nil, fmt.Errorf("plan operator %q child[%d]: %w", w.OperatorType, i, err)
		}
		children[i] = decoded
	}

	return &PlanOperator{
		OperatorType: w.OperatorType,
		Arguments:    args,
		Identifiers:  w.Identifiers,
		Children:     children,
	}, nil
}

// ============================================================================
// Temporal parsers
// ============================================================================

func parseTime(raw json.RawMessage, layout, typeName string) (time.Time, error) {
	s, err := unquoteString(raw, typeName)
	if err != nil {
		return time.Time{}, err
	}
	t, err := time.Parse(layout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("decode %s %q: %w", typeName, s, err)
	}
	return t, nil
}

// parseDuration parses an ISO 8601 duration string such as "P14DT16H12M"
// or "P1Y2M3DT4H5M6.789S" into a Duration.
//
// Grammar:  P[nY][nM][nW][nD][T[nH][nM][n[.n]S]]
func parseDuration(s string) (Duration, error) {
	if len(s) == 0 || s[0] != 'P' {
		return Duration{}, fmt.Errorf("decode Duration: expected leading 'P' in %q", s)
	}
	rem := s[1:] // strip leading P

	var d Duration
	inTime := false

	for len(rem) > 0 {
		if rem[0] == 'T' {
			inTime = true
			rem = rem[1:]
			continue
		}

		// Read a number (possibly with decimal point for seconds).
		numEnd := 0
		for numEnd < len(rem) && (rem[numEnd] == '.' || (rem[numEnd] >= '0' && rem[numEnd] <= '9')) {
			numEnd++
		}
		if numEnd == 0 || numEnd >= len(rem) {
			return Duration{}, fmt.Errorf("decode Duration: unexpected char in %q at %q", s, rem)
		}

		numStr := rem[:numEnd]
		unit := rem[numEnd]
		rem = rem[numEnd+1:]

		switch unit {
		case 'Y':
			n, err := strconv.ParseInt(numStr, 10, 64)
			if err != nil {
				return Duration{}, fmt.Errorf("decode Duration years %q: %w", numStr, err)
			}
			d.Months += n * 12
		case 'M':
			if !inTime {
				n, err := strconv.ParseInt(numStr, 10, 64)
				if err != nil {
					return Duration{}, fmt.Errorf("decode Duration months %q: %w", numStr, err)
				}
				d.Months += n
			} else {
				n, err := strconv.ParseInt(numStr, 10, 64)
				if err != nil {
					return Duration{}, fmt.Errorf("decode Duration minutes %q: %w", numStr, err)
				}
				d.Seconds += n * 60
			}
		case 'W':
			n, err := strconv.ParseInt(numStr, 10, 64)
			if err != nil {
				return Duration{}, fmt.Errorf("decode Duration weeks %q: %w", numStr, err)
			}
			d.Days += n * 7
		case 'D':
			n, err := strconv.ParseInt(numStr, 10, 64)
			if err != nil {
				return Duration{}, fmt.Errorf("decode Duration days %q: %w", numStr, err)
			}
			d.Days += n
		case 'H':
			n, err := strconv.ParseInt(numStr, 10, 64)
			if err != nil {
				return Duration{}, fmt.Errorf("decode Duration hours %q: %w", numStr, err)
			}
			d.Seconds += n * 3600
		case 'S':
			if integer, frac, hasFrac := strings.Cut(numStr, "."); hasFrac {
				secs, err := strconv.ParseInt(integer, 10, 64)
				if err != nil {
					return Duration{}, fmt.Errorf("decode Duration seconds %q: %w", numStr, err)
				}
				// Pad or truncate fractional part to 9 digits (nanoseconds).
				for len(frac) < 9 {
					frac += "0"
				}
				frac = frac[:9]
				nanos, err := strconv.ParseInt(frac, 10, 32)
				if err != nil {
					return Duration{}, fmt.Errorf("decode Duration nanos %q: %w", frac, err)
				}
				d.Seconds += secs
				d.Nanos = int32(nanos)
			} else {
				n, err := strconv.ParseInt(numStr, 10, 64)
				if err != nil {
					return Duration{}, fmt.Errorf("decode Duration seconds %q: %w", numStr, err)
				}
				d.Seconds += n
			}
		default:
			return Duration{}, fmt.Errorf("decode Duration: unknown unit %q in %q", string(unit), s)
		}
	}

	return d, nil
}

// parsePoint parses a WKT string with SRID prefix, e.g.:
//
//	"SRID=4326;POINT (12.3 45.6)"
//	"SRID=9157;POINT Z (1.0 2.0 3.0)"
func parsePoint(s string) (Point, error) {
	parts := strings.SplitN(s, ";", 2)
	if len(parts) != 2 {
		return Point{}, fmt.Errorf("decode Point: invalid format %q", s)
	}

	sridStr := strings.TrimPrefix(strings.TrimSpace(parts[0]), "SRID=")
	srid, err := strconv.Atoi(sridStr)
	if err != nil {
		return Point{}, fmt.Errorf("decode Point SRID %q: %w", sridStr, err)
	}

	wkt := strings.TrimSpace(parts[1])
	is3D := strings.HasPrefix(wkt, "POINT Z")

	coordStr := wkt
	if is3D {
		coordStr = strings.TrimPrefix(coordStr, "POINT Z (")
		coordStr = strings.TrimPrefix(coordStr, "POINT Z(")
	} else {
		coordStr = strings.TrimPrefix(coordStr, "POINT (")
		coordStr = strings.TrimPrefix(coordStr, "POINT(")
	}
	coordStr = strings.TrimSuffix(coordStr, ")")
	coordStr = strings.TrimSpace(coordStr)

	coords := strings.Fields(coordStr)
	expected := 2
	if is3D {
		expected = 3
	}
	if len(coords) != expected {
		return Point{}, fmt.Errorf("decode Point: expected %d coordinates, got %d in %q", expected, len(coords), s)
	}

	x, err := strconv.ParseFloat(coords[0], 64)
	if err != nil {
		return Point{}, fmt.Errorf("decode Point X: %w", err)
	}
	y, err := strconv.ParseFloat(coords[1], 64)
	if err != nil {
		return Point{}, fmt.Errorf("decode Point Y: %w", err)
	}

	p := Point{SRID: srid, X: x, Y: y, Is3D: is3D}
	if is3D {
		z, err := strconv.ParseFloat(coords[2], 64)
		if err != nil {
			return Point{}, fmt.Errorf("decode Point Z: %w", err)
		}
		p.Z = z
	}
	return p, nil
}

// ============================================================================
// Helpers
// ============================================================================

// isNull reports whether raw is a JSON null token.
func isNull(raw json.RawMessage) bool {
	return len(raw) == 4 && string(raw) == "null"
}

// unquoteString unmarshals a JSON string value from raw.
// typeName is used only for error messages.
func unquoteString(raw json.RawMessage, typeName string) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("decode %s: expected JSON string, got %s", typeName, string(raw))
	}
	return s, nil
}
