package hetzner

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	clocktesting "k8s.io/utils/clock/testing"
)

type mock struct {
	mu        sync.Mutex
	requests  []string
	bodies    []string
	remaining string
	limited   int
	sentinel  bool
}

func serverJSON(id int, name string, labels map[string]string) map[string]any {
	return map[string]any{
		"id": id, "name": name, "status": "running", "created": "2026-01-02T03:04:05+00:00", "labels": labels,
		"public_net": map[string]any{
			"ipv4": map[string]any{"ip": "192.0.2.7", "blocked": false},
			"ipv6": map[string]any{"ip": "2001:db8:1::/64", "blocked": false},
		},
		"server_type": map[string]any{"id": 1, "name": "cx22"},
		"datacenter":  map[string]any{"id": 1, "name": "nbg1-dc3", "location": map[string]any{"id": 1, "name": "nbg1"}},
		"location":    map[string]any{"id": 1, "name": "nbg1"},
		"image":       map[string]any{"id": 9, "name": "ubuntu-24.04", "type": "system"},
	}
}

func action(id int) map[string]any {
	return map[string]any{"id": id, "status": "success", "command": "x", "progress": 100, "started": "2026-01-02T03:04:05+00:00"}
}

func apiError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": code}})
}

func (m *mock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	body := new(strings.Builder)
	if r.Body != nil {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		body.Write(buf[:n])
	}
	m.requests = append(m.requests, r.Method+" "+r.URL.RequestURI())
	m.bodies = append(m.bodies, body.String())
	remaining := m.remaining
	m.mu.Unlock()
	if remaining != "" {
		w.Header().Set("RateLimit-Remaining", remaining)
	}
	w.Header().Set("Content-Type", "application/json")
	reply := func(status int, v any) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/servers":
		m.list(w, r, reply)
	case r.Method == http.MethodGet && r.URL.Path == "/servers/404":
		apiError(w, 404, "not_found")
	case r.Method == http.MethodGet && r.URL.Path == "/servers/429":
		m.mu.Lock()
		m.limited++
		m.mu.Unlock()
		apiError(w, 429, "rate_limit_exceeded")
	case r.Method == http.MethodGet && r.URL.Path == "/servers/5":
		reply(200, map[string]any{"server": serverJSON(5, "five", nil)})
	case r.Method == http.MethodPost && r.URL.Path == "/servers":
		if strings.Contains(body.String(), `"name":"dup"`) {
			apiError(w, 409, "uniqueness_error")
			return
		}
		reply(201, map[string]any{"server": serverJSON(7, "new", map[string]string{"a": "b"}), "action": action(1), "next_actions": []any{action(2)}})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/actions/"):
		id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/actions/"))
		reply(200, map[string]any{"action": action(id)})
	case r.Method == http.MethodPut && r.URL.Path == "/servers/5":
		reply(200, map[string]any{"server": serverJSON(5, "renamed", map[string]string{"k": "v"})})
	case r.Method == http.MethodPost && r.URL.Path == "/servers/5/actions/rebuild":
		reply(201, map[string]any{"action": action(3)})
	case r.Method == http.MethodDelete && r.URL.Path == "/servers/5":
		reply(200, map[string]any{"action": action(4)})
	case r.Method == http.MethodGet && r.URL.Path == "/ssh_keys" && r.URL.Query().Get("name") == "ecoplus-sentinel":
		keys := []any{}
		if m.sentinel {
			keys = append(keys, map[string]any{"id": 21, "name": "ecoplus-sentinel", "fingerprint": "f", "public_key": "k",
				"labels": map[string]string{"ecoplus/writer": "other", "ecoplus/heartbeat": "77"}})
		}
		reply(200, map[string]any{"ssh_keys": keys})
	case r.Method == http.MethodPut && r.URL.Path == "/ssh_keys/21":
		reply(200, map[string]any{"ssh_key": map[string]any{"id": 21, "name": "ecoplus-sentinel", "fingerprint": "f", "public_key": "k"}})
	case r.Method == http.MethodPost && r.URL.Path == "/ssh_keys":
		reply(201, map[string]any{"ssh_key": map[string]any{"id": 22, "name": "ecoplus-sentinel", "fingerprint": "f", "public_key": "k"}})
	case r.Method == http.MethodGet && r.URL.Path == "/ssh_keys":
		reply(200, map[string]any{"ssh_keys": []any{map[string]any{"id": 11, "name": "mine", "fingerprint": "f", "public_key": "k"}}})
	default:
		apiError(w, 404, "not_found")
	}
}

func (m *mock) list(_ http.ResponseWriter, r *http.Request, reply func(int, any)) {
	page := r.URL.Query().Get("page")
	meta := func(next any) map[string]any {
		return map[string]any{"pagination": map[string]any{"page": 1, "per_page": 50, "next_page": next, "last_page": 2, "total_entries": 2}}
	}
	if page == "2" {
		reply(200, map[string]any{"servers": []any{serverJSON(2, "two", nil)}, "meta": meta(nil)})
		return
	}
	reply(200, map[string]any{"servers": []any{serverJSON(1, "one", map[string]string{"ecoplus/owner": "ecoplus"})}, "meta": meta(2)})
}

func newClient(t *testing.T, m *mock) *Client {
	t.Helper()
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)
	clk := clocktesting.NewFakeClock(time.Unix(1_000_000, 0))
	return New(Options{
		Token: "t", Endpoint: srv.URL, Clock: clk, Backoff: time.Millisecond,
		Limit: LimitConfig{Rate: 1000, Burst: 1000, LowWatermark: 600},
	})
}
