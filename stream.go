package query

import (
	"bufio"
	"fmt"
	"io"
	"iter"

	"github.com/neo4j-contrib/query-go-sdk/internal/decode"
)

// StreamSummary is the execution metadata delivered once a streamed query
// result completes successfully. Available from StreamResult.Summary() only
// after Records() has been fully drained (naturally or via early return).
type StreamSummary = decode.StreamSummary

// StreamResult represents an in-progress streamed query result, obtained from
// QueryService.ExecuteStream. Records are decoded incrementally as they
// arrive over the wire instead of being buffered up front.
//
// Records() must be consumed to completion (or the caller must call Close)
// to release the underlying HTTP connection; ranging over Records() with a
// `break` still releases it, since the iterator closes the stream on early
// return. Call Records() at most once per StreamResult.
type StreamResult struct {
	fields  []string
	body    io.ReadCloser
	scanner *bufio.Scanner
	cancel  func()
	summary *StreamSummary
	drained bool
	closed  bool
}

// Fields returns the ordered column names for this result, available
// immediately after ExecuteStream returns (decoded from the stream's leading
// Header event).
func (s *StreamResult) Fields() []string {
	out := make([]string, len(s.fields))
	copy(out, s.fields)
	return out
}

// Records returns an iterator over the result rows, for use with:
//
//	for rec, err := range result.Records() {
//	    if err != nil {
//	        return err
//	    }
//	    // use rec
//	}
//
// Iteration stops after the first error — a transport/decode error, or a
// *QueryErrors surfaced by a mid-stream Error event — or once the terminating
// Summary event has been consumed. The underlying HTTP connection is released
// when iteration ends for any reason, including an early `break`.
func (s *StreamResult) Records() iter.Seq2[*Record, error] {
	return func(yield func(*Record, error) bool) {
		if s.closed || s.drained {
			return
		}
		defer func() { _ = s.Close() }()

		for s.scanner.Scan() {
			line := s.scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			eventType, body, err := decode.DecodeStreamEnvelope(line)
			if err != nil {
				yield(nil, err)
				return
			}

			switch eventType {
			case decode.StreamEventHeader:
				continue

			case decode.StreamEventRecord:
				row, err := decode.DecodeStreamRecord(body)
				if err != nil {
					yield(nil, err)
					return
				}
				if !yield(newRecord(s.fields, row), nil) {
					return
				}

			case decode.StreamEventSummary:
				summary, err := decode.DecodeStreamSummary(body)
				if err != nil {
					yield(nil, err)
					return
				}
				s.summary = summary
				s.drained = true
				return

			case decode.StreamEventError:
				queryErrs, err := decode.DecodeStreamError(body)
				if err != nil {
					yield(nil, err)
					return
				}
				s.drained = true
				yield(nil, queryErrs)
				return

			default:
				yield(nil, fmt.Errorf("query: stream: unknown event %q", eventType))
				return
			}
		}

		if err := s.scanner.Err(); err != nil {
			yield(nil, fmt.Errorf("query: stream: read: %w", err))
			return
		}

		yield(nil, fmt.Errorf("query: stream: connection closed before a Summary or Error event was received"))
	}
}

// Summary returns the execution metadata delivered by the stream's Summary
// event. It returns nil until Records() has been fully drained, and remains
// nil if the stream ended in error instead of a Summary event.
func (s *StreamResult) Summary() *StreamSummary {
	return s.summary
}

// Close releases the underlying HTTP connection. Safe to call multiple times
// and after Records() has already drained the stream (no-op in that case).
func (s *StreamResult) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	return s.body.Close()
}
