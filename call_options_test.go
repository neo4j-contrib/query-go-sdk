package query

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goccy/go-json"

	"github.com/neo4j-contrib/query-go-sdk/internal/api"
)

const emptyOK = `{"data":{"fields":[],"values":[]},"bookmarks":[]}`

func execBody(t *testing.T, svc *queryService, mock *mockRequestService, qry string, opts ...CallOption) map[string]any {
	t.Helper()
	mock.response = &api.Response{StatusCode: 200, Body: []byte(emptyOK)}
	if _, err := svc.Execute(context.Background(), qry, nil, opts...); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(mock.lastBody), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	return got
}

func TestCallOptions_RequestBody(t *testing.T) {
	mock := &mockRequestService{}
	svc := createTestQueryService(mock)

	got := execBody(t, svc, mock, "RETURN 1",
		WithIncludeCounters(),
		WithTxMetadata(map[string]any{"appName": "People"}),
		WithMaxExecutionTime(1500*time.Millisecond),
		WithBookmarks("bm1", "bm2"),
		WithNotificationsFilter(NotificationsFilter{MinimumSeverityLevel: "WARNING", DisabledCategories: []string{"HINT"}}),
		WithCallAccessMode(AccessModeRead),
	)

	if got["includeCounters"] != true {
		t.Errorf("includeCounters = %v", got["includeCounters"])
	}
	if md, _ := got["txMetadata"].(map[string]any); md["appName"] != "People" {
		t.Errorf("txMetadata = %v", got["txMetadata"])
	}
	if got["maxExecutionTime"] != float64(2) { // 1.5s rounds up to 2 whole seconds
		t.Errorf("maxExecutionTime = %v, want 2", got["maxExecutionTime"])
	}
	if bm, _ := got["bookmarks"].([]any); len(bm) != 2 || bm[0] != "bm1" {
		t.Errorf("bookmarks = %v", got["bookmarks"])
	}
	nf, _ := got["notificationsFilter"].(map[string]any)
	if nf["minimumSeverityLevel"] != "WARNING" {
		t.Errorf("notificationsFilter = %v", got["notificationsFilter"])
	}
	if got["accessMode"] != "Read" {
		t.Errorf("accessMode = %v", got["accessMode"])
	}
}

func TestCallOptions_OmittedByDefault(t *testing.T) {
	mock := &mockRequestService{}
	got := execBody(t, createTestQueryService(mock), mock, "RETURN 1")
	for _, k := range []string{"includeCounters", "txMetadata", "maxExecutionTime", "bookmarks", "notificationsFilter", "accessMode"} {
		if _, ok := got[k]; ok {
			t.Errorf("field %q should be omitted by default", k)
		}
	}
}

func TestCallOptions_AccessModeOverridesClient(t *testing.T) {
	mock := &mockRequestService{}
	svc := createTestQueryService(mock)
	svc.accessMode = AccessModeRead

	got := execBody(t, svc, mock, "RETURN 1", WithCallAccessMode(AccessModeWrite))
	if got["accessMode"] != "Write" {
		t.Errorf("accessMode = %v, want Write", got["accessMode"])
	}
	got = execBody(t, svc, mock, "RETURN 1")
	if got["accessMode"] != "Read" {
		t.Errorf("client default accessMode = %v, want Read", got["accessMode"])
	}
}

func TestCallOptions_Invalid(t *testing.T) {
	tests := map[string]CallOption{
		"zero timeout":           WithCallTimeout(0),
		"negative retry":         WithRetry(RetryPolicy{Transient: -1}),
		"bad access mode":        WithCallAccessMode(AccessMode(99)),
		"zero max exec time":     WithMaxExecutionTime(0),
		"negative network retry": WithRetry(RetryPolicy{Network: -1}),
	}
	for name, opt := range tests {
		t.Run(name, func(t *testing.T) {
			mock := &mockRequestService{response: &api.Response{StatusCode: 200, Body: []byte(emptyOK)}}
			_, err := createTestQueryService(mock).Execute(context.Background(), "RETURN 1", nil, opt)
			if err == nil {
				t.Fatal("expected error")
			}
			if mock.lastBody != "" {
				t.Error("API must not be called when options are invalid")
			}
		})
	}
}

func TestCallOptions_LegacyRejectsV2OnlyOptions(t *testing.T) {
	for name, opt := range map[string]CallOption{
		"counters":      WithIncludeCounters(),
		"txMetadata":    WithTxMetadata(map[string]any{"a": 1}),
		"maxExecution":  WithMaxExecutionTime(time.Second),
		"bookmarks":     WithBookmarks("x"),
		"notifications": WithNotificationsFilter(NotificationsFilter{}),
	} {
		t.Run(name, func(t *testing.T) {
			mock := &mockRequestService{}
			svc := createTestQueryService(mock)
			svc.useLegacyHTTP = true
			_, err := svc.Execute(context.Background(), "RETURN 1", nil, opt)
			if err == nil || !strings.Contains(err.Error(), "FlavorLegacyHTTP") {
				t.Fatalf("expected legacy error, got %v", err)
			}
		})
	}
}

func TestCallOptions_LegacyAcceptsTimeoutRetryAndAccessMode(t *testing.T) {
	mock := &mockRequestService{response: &api.Response{StatusCode: 200, Body: []byte(`{"results":[],"errors":[]}`)}}
	svc := createTestQueryService(mock)
	svc.useLegacyHTTP = true
	if _, err := svc.Execute(context.Background(), "RETURN 1", nil,
		WithCallTimeout(time.Second), WithRetry(RetryPolicy{}), WithCallAccessMode(AccessModeRead)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallOptions_PerCallTimeout(t *testing.T) {
	// Client timeout is generous; the call timeout is what must trigger.
	mock := &mockRequestServiceWithDelay{
		response: &api.Response{StatusCode: 200, Body: []byte(emptyOK)},
		delay:    500 * time.Millisecond,
	}
	svc := createTestQueryService(mock)
	svc.timeout = 30 * time.Second

	start := time.Now()
	_, err := svc.Execute(context.Background(), "RETURN 1", nil, WithCallTimeout(20*time.Millisecond))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
	if time.Since(start) > 400*time.Millisecond {
		t.Error("call timeout was not honoured")
	}

	// A longer call timeout than the client's lets a slow statement finish.
	svc.timeout = 20 * time.Millisecond
	if _, err := svc.Execute(context.Background(), "RETURN 1", nil, WithCallTimeout(5*time.Second)); err != nil {
		t.Fatalf("expected success with longer call timeout, got %v", err)
	}
}

func TestUsesInTransactions(t *testing.T) {
	yes := []string{
		"CALL { MATCH (n) DELETE n } IN TRANSACTIONS",
		"call { x } in transactions of 100 rows",
		"CALL { x } IN 4 CONCURRENT TRANSACTIONS",
		"CALL { x } IN CONCURRENT TRANSACTIONS OF 10 ROWS",
		"CALL {\n x\n}\nIN\nTRANSACTIONS",
	}
	no := []string{"MATCH (n) RETURN n", "CREATE (n:Transaction)", "MATCH (t) WHERE t.x IN [1,2] RETURN t"}
	for _, q := range yes {
		if !usesInTransactions(q) {
			t.Errorf("expected IN TRANSACTIONS detected in %q", q)
		}
	}
	for _, q := range no {
		if usesInTransactions(q) {
			t.Errorf("did not expect IN TRANSACTIONS in %q", q)
		}
	}
}

func deadlock() postResult {
	return postResult{resp: &api.Response{StatusCode: 200, Body: errBody(deadlockCode)}}
}

func TestRetry_InTransactionsDefaultsOff(t *testing.T) {
	m := &seqMock{posts: []postResult{deadlock()}}
	svc := newRetryService(m, 3)
	_, err := svc.Execute(context.Background(), "CALL { CREATE (n) } IN TRANSACTIONS", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := m.pCalls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 (no retry for IN TRANSACTIONS)", got)
	}
}

func TestRetry_InTransactionsExplicitOptIn(t *testing.T) {
	m := &seqMock{posts: []postResult{deadlock()}}
	svc := newRetryService(m, 3)
	_, _ = svc.Execute(context.Background(), "CALL { CREATE (n) } IN TRANSACTIONS", nil, WithRetry(RetryPolicy{Transient: 2}))
	if got := m.pCalls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3", got)
	}
}

func TestRetry_PerCallPolicyOverridesClient(t *testing.T) {
	m := &seqMock{posts: []postResult{deadlock()}}
	svc := newRetryService(m, 3)

	_, _ = svc.Execute(context.Background(), "RETURN 1", nil, WithRetry(RetryPolicy{})) // all off
	if got := m.pCalls.Load(); got != 1 {
		t.Errorf("RetryPolicy{}: calls = %d, want 1", got)
	}

	m.pCalls.Store(0)
	_, _ = svc.Execute(context.Background(), "RETURN 1", nil, WithRetry(RetryPolicy{Transient: 1}))
	if got := m.pCalls.Load(); got != 2 {
		t.Errorf("Transient 1: calls = %d, want 2", got)
	}
}

func TestRetry_TransientAndNetworkAreIndependent(t *testing.T) {
	// Transient retries are on but network retries are off: a transient error
	// is still retried. (Network retry is enforced in the HTTP layer; see
	// httpclient tests.)
	m := &seqMock{posts: []postResult{deadlock(), {resp: &api.Response{StatusCode: 200, Body: []byte(emptyOK)}}}}
	svc := newRetryService(m, 0)
	svc.maxTransientRetry = 3
	svc.maxNetworkRetry = 0
	if _, err := svc.Execute(context.Background(), "RETURN 1", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := m.pCalls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2", got)
	}
}

func TestExecute_BodyMarshalledOnceAcrossRetries(t *testing.T) {
	var bodies [][]byte
	var mu sync.Mutex
	cb := &bodyRecorder{onPost: func(b []byte) { mu.Lock(); bodies = append(bodies, b); mu.Unlock() },
		results: []postResult{deadlock(), {resp: &api.Response{StatusCode: 200, Body: []byte(emptyOK)}}}}
	svc := newRetryService(cb, 3)
	if _, err := svc.Execute(context.Background(), "RETURN 1", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bodies) != 2 || !bytes.Equal(bodies[0], bodies[1]) {
		t.Errorf("expected identical bodies across attempts, got %q", bodies)
	}
}

type bodyRecorder struct {
	onPost  func([]byte)
	results []postResult
	i       int
}

func (b *bodyRecorder) Post(_ context.Context, body []byte) (*api.Response, error) {
	b.onPost(body)
	r := b.results[b.i]
	if b.i < len(b.results)-1 {
		b.i++
	}
	return r.resp, r.err
}
func (b *bodyRecorder) PostStream(context.Context, []byte) (*api.StreamResponse, error) {
	return nil, errors.New("unused")
}
func (b *bodyRecorder) Discover(context.Context) (*api.DiscoveryResponse, error) { return nil, nil }
func (b *bodyRecorder) Close()                                                   {}

func TestExecute_ReturnsCounters(t *testing.T) {
	body := `{"data":{"fields":[],"values":[]},"counters":{"nodesCreated":7,"containsUpdates":true},"bookmarks":[]}`
	mock := &mockRequestService{response: &api.Response{StatusCode: 200, Body: []byte(body)}}
	resp, err := createTestQueryService(mock).Execute(context.Background(), "CREATE (n)", nil, WithIncludeCounters())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Counters == nil || resp.Counters.NodesCreated != 7 {
		t.Errorf("counters = %+v", resp.Counters)
	}

	er, err := EagerResultTransformer(resp)
	if err != nil {
		t.Fatal(err)
	}
	if er.Counters == nil || er.Counters.NodesCreated != 7 {
		t.Errorf("EagerResult counters = %+v", er.Counters)
	}
}

func TestWithTransformer_PassesCallOptions(t *testing.T) {
	mock := &mockRequestService{response: &api.Response{StatusCode: 200, Body: []byte(emptyOK)}}
	svc := createTestQueryService(mock)
	if _, err := WithTransformer(svc, context.Background(), "RETURN 1", nil, EagerResultTransformer, WithIncludeCounters()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mock.lastBody, `"includeCounters":true`) {
		t.Errorf("call option not forwarded: %s", mock.lastBody)
	}
}

func TestExecuteStream_CallOptionsAndCounters(t *testing.T) {
	hdr := `{"$event":"Header","_body":{"fields":["n"]}}`
	sum := `{"$event":"Summary","_body":{"counters":{"nodesCreated":3}}}`
	mock := &mockRequestService{streamResponse: newStreamResponse(hdr, sum)}
	svc := createTestQueryService(mock)
	svc.streamingEnabled = true
	svc.maxResponseSize = 1 << 20

	res, err := svc.ExecuteStream(context.Background(), "CREATE (n)", nil, WithIncludeCounters(), WithBookmarks("bm"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for range res.Records() {
	}
	if !strings.Contains(mock.lastBody, `"includeCounters":true`) || !strings.Contains(mock.lastBody, `"bookmarks":["bm"]`) {
		t.Errorf("stream body missing options: %s", mock.lastBody)
	}
	s := res.Summary()
	if s == nil || s.Counters == nil || s.Counters.NodesCreated != 3 {
		t.Errorf("summary counters = %+v", s)
	}
}

// ─── Logging ──────────────────────────────────────────────────────────────────

type captureHandler struct {
	mu     sync.Mutex
	levels []slog.Level
	msgs   []string
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.levels = append(h.levels, r.Level)
	h.msgs = append(h.msgs, r.Message)
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func (h *captureHandler) hasLevel(l slog.Level) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, x := range h.levels {
		if x == l {
			return true
		}
	}
	return false
}

func TestLogging_ExpectedQueryErrorsNotLoggedAtError(t *testing.T) {
	for name, res := range map[string]postResult{
		"query errors in 2xx body": {resp: &api.Response{StatusCode: 200, Body: errBody(syntaxCode)}},
		"api error":                {err: &api.Error{StatusCode: 401, Message: "Unauthorized"}},
	} {
		t.Run(name, func(t *testing.T) {
			h := &captureHandler{}
			svc := newRetryService(&seqMock{posts: []postResult{res}}, 0)
			svc.logger = slog.New(h)
			if _, err := svc.Execute(context.Background(), "RETURN 1", nil); err == nil {
				t.Fatal("expected error")
			}
			if h.hasLevel(slog.LevelError) {
				t.Errorf("expected error must not be logged at ERROR; got %v", h.msgs)
			}
		})
	}
}

func TestLogging_UnexpectedFailureStillLoggedAtError(t *testing.T) {
	h := &captureHandler{}
	svc := newRetryService(&seqMock{posts: []postResult{{err: errors.New("connection refused")}}}, 0)
	svc.logger = slog.New(h)
	if _, err := svc.Execute(context.Background(), "RETURN 1", nil); err == nil {
		t.Fatal("expected error")
	}
	if !h.hasLevel(slog.LevelError) {
		t.Error("transport failures should still be logged at ERROR")
	}
}

func TestRetryOptions_Client(t *testing.T) {
	o := defaultOptions()
	if o.config.apiRetryMax != 3 || o.config.transientMax != 3 {
		t.Fatalf("defaults = %d/%d, want 3/3", o.config.apiRetryMax, o.config.transientMax)
	}
	if err := WithMaxRetry(5)(o); err != nil || o.config.apiRetryMax != 5 || o.config.transientMax != 5 {
		t.Errorf("WithMaxRetry should set both: %+v err=%v", o.config, err)
	}
	if err := WithMaxNetworkRetry(0)(o); err != nil || o.config.apiRetryMax != 0 || o.config.transientMax != 5 {
		t.Errorf("WithMaxNetworkRetry should set only network: %+v err=%v", o.config, err)
	}
	if err := WithMaxTransientRetry(1)(o); err != nil || o.config.transientMax != 1 || o.config.apiRetryMax != 0 {
		t.Errorf("WithMaxTransientRetry should set only transient: %+v err=%v", o.config, err)
	}
	if WithMaxNetworkRetry(-1)(o) == nil || WithMaxTransientRetry(-1)(o) == nil {
		t.Error("negative values must be rejected")
	}
}
