package query

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/neo4j-contrib/query-go-sdk/internal/api"
	"github.com/neo4j-contrib/query-go-sdk/internal/decode"
)

// seqMock returns the queued post/stream results in order, repeating the last.
type seqMock struct {
	posts   []postResult
	streams []streamResult
	pCalls  atomic.Int32
	sCalls  atomic.Int32
}

type postResult struct {
	resp *api.Response
	err  error
}

type streamResult struct {
	resp *api.StreamResponse
	err  error
}

func (m *seqMock) Post(_ context.Context, _ string) (*api.Response, error) {
	i := int(m.pCalls.Add(1)) - 1
	if i >= len(m.posts) {
		i = len(m.posts) - 1
	}
	return m.posts[i].resp, m.posts[i].err
}

func (m *seqMock) PostStream(_ context.Context, _ string) (*api.StreamResponse, error) {
	i := int(m.sCalls.Add(1)) - 1
	if i >= len(m.streams) {
		i = len(m.streams) - 1
	}
	return m.streams[i].resp, m.streams[i].err
}
func (m *seqMock) Discover(context.Context) (*api.DiscoveryResponse, error) { return nil, nil }
func (m *seqMock) Close()                                                   {}

const (
	deadlockCode   = "Neo.TransientError.Transaction.DeadlockDetected"
	terminatedCode = "Neo.TransientError.Transaction.Terminated"
	syntaxCode     = "Neo.ClientError.Statement.SyntaxError"
)

func errBody(code string) []byte {
	return []byte(`{"errors":[{"code":"` + code + `","message":"boom"}]}`)
}

var okBody = []byte(`{"data":{"fields":["n"],"values":[[{"$type":"Integer","_value":"1"}]]},"bookmarks":[]}`)

func newRetryService(m api.RequestService, maxRetry int) *queryService {
	return &queryService{
		api: m, timeout: 10 * time.Second, logger: testLogger(),
		maxRetry: maxRetry, retryBackoff: time.Millisecond,
	}
}

func TestIsRetryableTransient(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"deadlock QueryErrors", &decode.QueryErrors{Errors: []decode.QueryError{{Code: deadlockCode}}}, true},
		{"deadlock api.Error", &api.Error{StatusCode: 500, Details: []api.ErrorDetail{{Code: deadlockCode}}}, true},
		{"wrapped", errors.Join(errors.New("x"), &decode.QueryErrors{Errors: []decode.QueryError{{Code: deadlockCode}}}), true},
		{"terminated", &decode.QueryErrors{Errors: []decode.QueryError{{Code: terminatedCode}}}, false},
		{"lock client stopped", &decode.QueryErrors{Errors: []decode.QueryError{{Code: "Neo.TransientError.Transaction.LockClientStopped"}}}, false},
		{"client error", &decode.QueryErrors{Errors: []decode.QueryError{{Code: syntaxCode}}}, false},
		{"api error no code", &api.Error{StatusCode: 500}, false},
		{"plain error", errors.New("nope"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableTransient(tt.err); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExecute_RetriesTransientThenSucceeds(t *testing.T) {
	for _, tc := range []struct {
		name string
		fail postResult
	}{
		{"v2 http error", postResult{err: &api.Error{StatusCode: 500, Details: []api.ErrorDetail{{Code: deadlockCode}}}}},
		{"2xx body error", postResult{resp: &api.Response{StatusCode: 200, Body: errBody(deadlockCode)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &seqMock{posts: []postResult{tc.fail, tc.fail, {resp: &api.Response{StatusCode: 200, Body: okBody}}}}
			res, err := newRetryService(m, 3).Execute(context.Background(), "RETURN 1", nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res == nil {
				t.Fatal("expected response")
			}
			if got := m.pCalls.Load(); got != 3 {
				t.Errorf("calls = %d, want 3", got)
			}
		})
	}
}

func TestExecute_TransientExhaustsRetries(t *testing.T) {
	m := &seqMock{posts: []postResult{{resp: &api.Response{StatusCode: 200, Body: errBody(deadlockCode)}}}}
	_, err := newRetryService(m, 3).Execute(context.Background(), "RETURN 1", nil)
	var qErrs *decode.QueryErrors
	if !errors.As(err, &qErrs) {
		t.Fatalf("expected *QueryErrors, got %v", err)
	}
	if got := m.pCalls.Load(); got != 4 { // 1 attempt + 3 retries
		t.Errorf("calls = %d, want 4", got)
	}
}

func TestExecute_ZeroRetryDisablesTransientRetry(t *testing.T) {
	m := &seqMock{posts: []postResult{{resp: &api.Response{StatusCode: 200, Body: errBody(deadlockCode)}}}}
	_, err := newRetryService(m, 0).Execute(context.Background(), "RETURN 1", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := m.pCalls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

func TestExecute_NonRetryableNotRetried(t *testing.T) {
	for _, code := range []string{syntaxCode, terminatedCode} {
		m := &seqMock{posts: []postResult{{resp: &api.Response{StatusCode: 200, Body: errBody(code)}}}}
		_, err := newRetryService(m, 3).Execute(context.Background(), "RETURN 1", nil)
		if err == nil {
			t.Fatalf("%s: expected error", code)
		}
		if got := m.pCalls.Load(); got != 1 {
			t.Errorf("%s: calls = %d, want 1", code, got)
		}
	}
}

func TestExecute_LegacyTransientRetried(t *testing.T) {
	legacyErr := []byte(`{"results":[],"errors":[{"code":"` + deadlockCode + `","message":"boom"}]}`)
	legacyOK := []byte(`{"results":[{"columns":["n"],"data":[{"row":[1],"meta":[null]}]}],"errors":[]}`)
	m := &seqMock{posts: []postResult{
		{resp: &api.Response{StatusCode: 200, Body: legacyErr}},
		{resp: &api.Response{StatusCode: 200, Body: legacyOK}},
	}}
	svc := newRetryService(m, 3)
	svc.useLegacyHTTP = true
	if _, err := svc.Execute(context.Background(), "RETURN 1", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := m.pCalls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2", got)
	}
}

func TestExecute_RetryStopsOnContextCancel(t *testing.T) {
	m := &seqMock{posts: []postResult{{resp: &api.Response{StatusCode: 200, Body: errBody(deadlockCode)}}}}
	svc := newRetryService(m, 10)
	svc.retryBackoff = time.Hour // would hang if the wait ignored ctx
	svc.timeout = 50 * time.Millisecond

	start := time.Now()
	_, err := svc.Execute(context.Background(), "RETURN 1", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if time.Since(start) > 2*time.Second {
		t.Error("backoff did not honour context deadline")
	}
	if got := m.pCalls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

func TestExecuteStream_RetriesTransientFirstEvent(t *testing.T) {
	errLine := `{"$event":"Error","_body":[{"code":"` + deadlockCode + `","message":"boom"}]}`
	hdr := `{"$event":"Header","_body":{"fields":["n"]}}`
	m := &seqMock{streams: []streamResult{
		{resp: newStreamResponse(errLine)},
		{resp: newStreamResponse(hdr)},
	}}
	svc := newRetryService(m, 3)
	svc.streamingEnabled = true
	svc.maxResponseSize = 1 << 20
	res, err := svc.ExecuteStream(context.Background(), "RETURN 1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = res.Close()
	if got := m.sCalls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2", got)
	}
}
