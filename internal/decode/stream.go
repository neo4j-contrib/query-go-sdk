package decode

import (
	"fmt"
	"time"

	"github.com/goccy/go-json"
)

// ============================================================================
// Wire types — unexported, used only during JSON unmarshalling
// ============================================================================

// wireStreamEnvelope is the shape of every line in a streamed response:
//
//	{"$event": "Header|Record|Summary|Error", "_body": ...}
//
// See https://neo4j.com/docs/query-api/current/streaming/.
type wireStreamEnvelope struct {
	Event string          `json:"$event"`
	Body  json.RawMessage `json:"_body"`
}

// wireStreamHeaderBody is the _body shape of a Header event.
type wireStreamHeaderBody struct {
	Fields []string `json:"fields"`
}

// wireStreamSummaryBody is the _body shape of a Summary event. Timings are
// reported in milliseconds on the wire.
type wireStreamSummaryBody struct {
	Bookmarks            []string          `json:"bookmarks,omitempty"`
	Notifications        []Notification    `json:"notifications,omitempty"`
	QueryPlan            *wirePlanOperator `json:"queryPlan,omitempty"`
	ProfiledQueryPlan    *wirePlanOperator `json:"profiledQueryPlan,omitempty"`
	QueryType            string            `json:"queryType,omitempty"`
	ResultAvailableAfter int64             `json:"resultAvailableAfter,omitempty"`
	ResultConsumedAfter  int64             `json:"resultConsumedAfter,omitempty"`
}

// ============================================================================
// Stream event type constants
// ============================================================================

const (
	StreamEventHeader  = "Header"
	StreamEventRecord  = "Record"
	StreamEventSummary = "Summary"
	StreamEventError   = "Error"
)

// ============================================================================
// Public entry points
// ============================================================================

// DecodeStreamEnvelope parses one line of a streamed response into its event
// type and raw body. line must not include the trailing newline.
func DecodeStreamEnvelope(line []byte) (event string, body json.RawMessage, err error) {
	var env wireStreamEnvelope
	if err := json.Unmarshal(line, &env); err != nil {
		return "", nil, fmt.Errorf("decode stream: unmarshal event: %w", err)
	}
	if env.Event == "" {
		return "", nil, fmt.Errorf("decode stream: missing $event in %s", string(line))
	}
	return env.Event, env.Body, nil
}

// DecodeStreamHeader decodes a Header event body into its ordered field names.
func DecodeStreamHeader(body json.RawMessage) ([]string, error) {
	var h wireStreamHeaderBody
	if err := json.Unmarshal(body, &h); err != nil {
		return nil, fmt.Errorf("decode stream: unmarshal Header: %w", err)
	}
	return h.Fields, nil
}

// DecodeStreamRecord decodes a Record event body — a JSON array of typed
// value envelopes — into one fully decoded row, reusing DecodeValue for each
// element exactly as the buffered response decoder does.
func DecodeStreamRecord(body json.RawMessage) ([]any, error) {
	var raws []json.RawMessage
	if err := json.Unmarshal(body, &raws); err != nil {
		return nil, fmt.Errorf("decode stream: unmarshal Record: %w", err)
	}
	row := make([]any, len(raws))
	for i, r := range raws {
		v, err := DecodeValue(r)
		if err != nil {
			return nil, fmt.Errorf("decode stream: record[%d]: %w", i, err)
		}
		row[i] = v
	}
	return row, nil
}

// DecodeStreamSummary decodes a Summary event body into a StreamSummary.
func DecodeStreamSummary(body json.RawMessage) (*StreamSummary, error) {
	var s wireStreamSummaryBody
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, fmt.Errorf("decode stream: unmarshal Summary: %w", err)
	}

	summary := &StreamSummary{
		Bookmarks:            s.Bookmarks,
		Notifications:        s.Notifications,
		QueryType:            s.QueryType,
		ResultAvailableAfter: time.Duration(s.ResultAvailableAfter) * time.Millisecond,
		ResultConsumedAfter:  time.Duration(s.ResultConsumedAfter) * time.Millisecond,
	}

	if s.QueryPlan != nil {
		plan, err := decodePlanOperator(*s.QueryPlan)
		if err != nil {
			return nil, fmt.Errorf("decode stream: Summary queryPlan: %w", err)
		}
		summary.QueryPlan = plan
	}

	if s.ProfiledQueryPlan != nil {
		plan, err := decodePlanOperator(*s.ProfiledQueryPlan)
		if err != nil {
			return nil, fmt.Errorf("decode stream: Summary profiledQueryPlan: %w", err)
		}
		summary.ProfiledQueryPlan = plan
	}

	return summary, nil
}

// DecodeStreamError decodes an Error event body — a JSON array of
// {code, message} objects — into a *QueryErrors, mirroring how the buffered
// response decoder surfaces Neo4j errors.
func DecodeStreamError(body json.RawMessage) (*QueryErrors, error) {
	var errs []QueryError
	if err := json.Unmarshal(body, &errs); err != nil {
		return nil, fmt.Errorf("decode stream: unmarshal Error: %w", err)
	}
	return &QueryErrors{Errors: errs}, nil
}
