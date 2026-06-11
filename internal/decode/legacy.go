package decode

import (
	"fmt"

	"github.com/goccy/go-json"
)

// legacyWireResponse is the top-level shape returned by the Cypher HTTP
// Transaction API (POST /db/{db}/tx/commit).
type legacyWireResponse struct {
	Results []legacyResult `json:"results"`
	Errors  []QueryError   `json:"errors"`
}

type legacyResult struct {
	Columns []string         `json:"columns"`
	Data    []legacyDataItem `json:"data"`
}

type legacyDataItem struct {
	Row []json.RawMessage `json:"row"`
}

// DecodeLegacyResponse decodes a response body from the legacy Cypher HTTP
// Transaction API into the same *Response type used by the Query API v2 decoder.
//
// Row values are plain JSON — strings, numbers, booleans, nulls, arrays, and
// objects. Neo4j graph types (Node, Relationship) are returned as plain maps
// because the /tx/commit endpoint does not emit typed envelopes.
//
// If Neo4j returned errors a *QueryErrors is returned and Response is nil.
func DecodeLegacyResponse(body []byte) (*Response, error) {
	var wire legacyWireResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("decode legacy: unmarshal response body: %w", err)
	}

	if len(wire.Errors) > 0 {
		return nil, &QueryErrors{Errors: wire.Errors}
	}

	if len(wire.Results) == 0 {
		return &Response{}, nil
	}

	result := wire.Results[0]

	rows := make([][]any, len(result.Data))
	for i, item := range result.Data {
		row := make([]any, len(item.Row))
		for j, raw := range item.Row {
			v, err := DecodeValue(raw)
			if err != nil {
				col := ""
				if j < len(result.Columns) {
					col = result.Columns[j]
				}
				return nil, fmt.Errorf("decode legacy: row %d col %d (%s): %w", i, j, col, err)
			}
			row[j] = v
		}
		rows[i] = row
	}

	return &Response{
		Fields: result.Columns,
		Rows:   rows,
	}, nil
}
