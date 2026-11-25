package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/theocourtois/hetzner-ecoplus/internal/api"
	"github.com/theocourtois/hetzner-ecoplus/internal/cloud/fake"
	"github.com/theocourtois/hetzner-ecoplus/internal/engine"
	"github.com/theocourtois/hetzner-ecoplus/internal/fleet"
	clocktesting "k8s.io/utils/clock/testing"
)

type stub struct {
	err     error
	replay  bool
	ready   bool
	lastReq api.ServerRequest
}

func (s *stub) Acquire(_ context.Context, req api.ServerRequest) (api.Server, bool, error) {
	s.lastReq = req
	return api.Server{ID: 1, Lease: "l"}, s.replay, s.err
}
func (s *stub) Get(int64, string) (api.Server, error)        { return api.Server{ID: 1}, s.err }
func (s *stub) Release(context.Context, int64, string) error { return s.err }
func (s *stub) Patch(context.Context, int64, string, api.ServerPatch) (api.Server, error) {
	return api.Server{ID: 1}, s.err
}
func (s *stub) Ready() bool { return s.ready }

const token = "secret"

func call(t *testing.T, h http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) api.Code {
	t.Helper()
	var e api.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return e.Code
}

const validBody = `{"name":"a","server_type":"cx22","location":"nbg1","image":"ubuntu"}`

func TestErrorCodeMapping(t *testing.T) {
	cases := map[string]struct {
		err    error
		status int
		code   api.Code
		retry  bool
	}{
		"invalid":      {api.NewError(api.CodeInvalidRequest, "x"), 400, api.CodeInvalidRequest, false},
		"not found":    {api.NewError(api.CodeNotFound, "x"), 404, api.CodeNotFound, false},
		"conflict":     {api.NewError(api.CodeConflict, "x"), 409, api.CodeConflict, false},
		"upstream":     {api.NewError(api.CodeUpstream, "x"), 503, api.CodeUpstream, true},
		"rate limited": {api.NewError(api.CodeRateLimited, "x"), 503, api.CodeRateLimited, true},
		"not ready":    {api.NewError(api.CodeNotReady, "x"), 503, api.CodeNotReady, true},
		"typed":        {api.NewError(api.CodeInternal, "x"), 500, api.CodeInternal, false},
		"untyped":      {errors.New("boom"), 500, api.CodeInternal, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := New(Options{Engine: &stub{err: tc.err}, Token: token}).APIHandler()
			for _, req := range []struct{ method, path, body string }{
				{"POST", "/v1/servers", validBody}, {"GET", "/v1/servers/1", ""},
				{"PATCH", "/v1/servers/1", `{"name":"b"}`}, {"DELETE", "/v1/servers/1", ""},
			} {
				rec := call(t, h, req.method, req.path, req.body, nil)
				if rec.Code != tc.status || errCode(t, rec) != tc.code {
					t.Fatalf("%s: got %d %s", req.method, rec.Code, rec.Body.String())
				}
				if (rec.Header().Get("Retry-After") != "") != tc.retry {
					t.Fatalf("%s: retry-after mismatch: %q", req.method, rec.Header().Get("Retry-After"))
				}
			}
		})
	}
}

func TestRateLimitedRetryAfterIsLonger(t *testing.T) {
	h := New(Options{Engine: &stub{err: api.NewError(api.CodeRateLimited, "x")}, Token: token}).APIHandler()
	if got := call(t, h, "GET", "/v1/servers/1", "", nil).Header().Get("Retry-After"); got != "30" {
		t.Fatalf("got %q", got)
	}
}

func TestAuth(t *testing.T) {
	h := New(Options{Engine: &stub{}, Token: token}).APIHandler()
	for name, header := range map[string]string{"missing": "", "wrong": "Bearer nope", "scheme": "Basic secret", "bare": token} {
		req := httptest.NewRequestWithContext(context.Background(), "GET", "/v1/servers/1", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 401 || errCode(t, rec) != api.CodeUnauthorized {
			t.Errorf("%s: got %d", name, rec.Code)
		}
	}
	empty := New(Options{Engine: &stub{}, Token: ""}).APIHandler()
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/v1/servers/1", nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	empty.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatal("empty configured token must never authenticate")
	}
}

func TestStrictDecoding(t *testing.T) {
	h := New(Options{Engine: &stub{}, Token: token}).APIHandler()
	bodies := map[string]string{
		"unknown field": `{"name":"a","bogus":1}`, "trailing": validBody + `{}`, "garbage": `nope`, "empty": ``,
		"wrong type": `{"name":1}`,
	}
	for name, body := range bodies {
		if rec := call(t, h, "POST", "/v1/servers", body, nil); rec.Code != 400 || errCode(t, rec) != api.CodeInvalidRequest {
			t.Errorf("%s: got %d %s", name, rec.Code, rec.Body.String())
		}
	}
	if rec := call(t, h, "PATCH", "/v1/servers/1", `{"nope":1}`, nil); rec.Code != 400 {
		t.Fatalf("patch unknown field: %d", rec.Code)
	}
	if rec := call(t, h, "GET", "/v1/servers/abc", "", nil); rec.Code != 400 {
		t.Fatalf("bad id: %d", rec.Code)
	}
	if rec := call(t, h, "DELETE", "/v1/servers/0", "", nil); rec.Code != 400 {
		t.Fatalf("zero id: %d", rec.Code)
	}
}

func TestCreateStatusCodes(t *testing.T) {
	for replay, want := range map[bool]int{false: 201, true: 200} {
		h := New(Options{Engine: &stub{replay: replay}, Token: token}).APIHandler()
		if rec := call(t, h, "POST", "/v1/servers", validBody, nil); rec.Code != want {
			t.Fatalf("replay=%v got %d", replay, rec.Code)
		}
	}
	if rec := call(t, New(Options{Engine: &stub{}, Token: token}).APIHandler(), "DELETE", "/v1/servers/1", "", nil); rec.Code != 204 || rec.Body.Len() != 0 {
		t.Fatalf("delete: %d", rec.Code)
	}
}

func TestAdminEndpoints(t *testing.T) {
	st := &stub{}
	admin := New(Options{Engine: st}).AdminHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("m")) }))
	if call(t, admin, "GET", "/healthz", "", nil).Code != 200 || call(t, admin, "GET", "/readyz", "", nil).Code != 503 {
		t.Fatal("health/ready wrong")
	}
	st.ready = true
	if call(t, admin, "GET", "/readyz", "", nil).Code != 200 || call(t, admin, "GET", "/metrics", "", nil).Body.String() != "m" {
		t.Fatal("ready/metrics wrong")
	}
	bare := New(Options{Engine: st}).AdminHandler(nil)
	if call(t, bare, "GET", "/metrics", "", nil).Code != 404 {
		t.Fatal("metrics must be absent without a handler")
	}
}

func liveServer(t *testing.T) http.Handler {
	t.Helper()
	clk := clocktesting.NewFakeClock(time.Unix(10_000_000, 0))
	eng := engine.New(engine.Options{Cloud: fake.New(clk), Clock: clk, Fleet: fleet.DefaultConfig(), Workers: 2})
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(eng.Stop)
	return New(Options{Engine: eng, Token: token}).APIHandler()
}

func TestEndToEndLifecycle(t *testing.T) {
	h := liveServer(t)
	rec := call(t, h, "POST", "/v1/servers", validBody, nil)
	if rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created api.Server
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	lease := map[string]string{api.HeaderLease: created.Lease}
	path := "/v1/servers/" + itoa(created.ID)

	if replay := call(t, h, "POST", "/v1/servers", validBody, nil); replay.Code != 200 || !bytes.Contains(replay.Body.Bytes(), []byte(created.Lease)) {
		t.Fatalf("replay: %d %s", replay.Code, replay.Body.String())
	}
	other := strings.Replace(validBody, "ubuntu", "debian", 1)
	if rec := call(t, h, "POST", "/v1/servers", other, nil); rec.Code != 409 || errCode(t, rec) != api.CodeConflict {
		t.Fatalf("same name different spec must conflict: %d", rec.Code)
	}
	if rec := call(t, h, "GET", path, "", lease); rec.Code != 200 {
		t.Fatalf("get: %d", rec.Code)
	}
	if rec := call(t, h, "GET", path, "", map[string]string{api.HeaderLease: "ffffffffffffffff"}); rec.Code != 404 {
		t.Fatalf("lease mismatch must be 404: %d", rec.Code)
	}
	if rec := call(t, h, "GET", path, "", nil); rec.Code != 404 {
		t.Fatalf("missing lease must be 404: %d", rec.Code)
	}
	if rec := call(t, h, "PATCH", path, `{"image":"debian"}`, lease); rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte(`"image":"debian"`)) {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(t, h, "DELETE", path, "", lease); rec.Code != 204 {
		t.Fatalf("delete: %d", rec.Code)
	}
	if rec := call(t, h, "DELETE", path, "", lease); rec.Code != 404 {
		t.Fatalf("second delete: %d", rec.Code)
	}
	if rec := call(t, h, "GET", path, "", lease); rec.Code != 404 {
		t.Fatalf("released lease must be fenced: %d", rec.Code)
	}
	reused := call(t, h, "POST", "/v1/servers", strings.Replace(validBody, `"a"`, `"b"`, 1), nil)
	var again api.Server
	_ = json.Unmarshal(reused.Body.Bytes(), &again)
	if reused.Code != 201 || again.ID != created.ID || !again.Reused || again.Lease == created.Lease {
		t.Fatalf("expected reuse: %d %+v", reused.Code, again)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestNotReadyBeforeStart(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Unix(10_000_000, 0))
	eng := engine.New(engine.Options{Cloud: fake.New(clk), Clock: clk, Fleet: fleet.DefaultConfig()})
	defer eng.Stop()
	h := New(Options{Engine: eng, Token: token}).APIHandler()
	if rec := call(t, h, "POST", "/v1/servers", validBody, nil); rec.Code != 503 || errCode(t, rec) != api.CodeNotReady {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(t, h, "GET", "/v1/servers/1", "", map[string]string{api.HeaderLease: "x"}); rec.Code != 503 {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestValidationErrorsFromEngine(t *testing.T) {
	h := liveServer(t)
	for name, body := range map[string]string{
		"bad name": `{"name":"Bad_Name","server_type":"x","location":"y","image":"z"}`,
		"reserved": `{"name":"a","server_type":"x","location":"y","image":"z","labels":{"ecoplus/state":"x"}}`,
		"ssh key":  `{"name":"a","server_type":"x","location":"y","image":"z","ssh_keys":["missing"]}`,
	} {
		if rec := call(t, h, "POST", "/v1/servers", body, nil); rec.Code != 400 {
			t.Errorf("%s: got %d %s", name, rec.Code, rec.Body.String())
		}
	}
}
