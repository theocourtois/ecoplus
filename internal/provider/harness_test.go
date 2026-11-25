package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/theocourtois/ecoplus/internal/api"
	"github.com/theocourtois/ecoplus/internal/cloud/fake"
	"github.com/theocourtois/ecoplus/internal/engine"
	"github.com/theocourtois/ecoplus/internal/fleet"
	"github.com/theocourtois/ecoplus/internal/server"
	clocktesting "k8s.io/utils/clock/testing"
)

const testToken = "test-token"

type controller struct {
	t       *testing.T
	url     string
	cloud   *fake.Cloud
	failGet atomic.Bool
	mu      sync.Mutex
	created []api.Server
}

func newController(t *testing.T) *controller {
	t.Helper()
	clk := clocktesting.NewFakeClock(time.Unix(10_000_000, 0))
	c := &controller{t: t, cloud: fake.New(clk)}
	eng := engine.New(engine.Options{Cloud: c.cloud, Clock: clk, Fleet: fleet.DefaultConfig(), Workers: 4})
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(eng.Stop)
	inner := server.New(server.Options{Engine: eng, Token: testToken}).APIHandler()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.serve(inner, w, r)
	}))
	t.Cleanup(ts.Close)
	c.url = ts.URL
	return c
}

func (c *controller) serve(inner http.Handler, w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && c.failGet.Load() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"code":"upstream_unavailable","message":"injected"}`))
		return
	}
	rec := httptest.NewRecorder()
	inner.ServeHTTP(rec, r)
	if r.Method == http.MethodPost && rec.Code < 300 {
		var srv api.Server
		if json.Unmarshal(rec.Body.Bytes(), &srv) == nil {
			c.mu.Lock()
			c.created = append(c.created, srv)
			c.mu.Unlock()
		}
	}
	for k, v := range rec.Header() {
		w.Header()[k] = v
	}
	w.WriteHeader(rec.Code)
	_, _ = io.Copy(w, bytes.NewReader(rec.Body.Bytes()))
}

func (c *controller) last() api.Server {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.created) == 0 {
		c.t.Fatal("no server created yet")
	}
	return c.created[len(c.created)-1]
}

func (c *controller) releaseOutOfBand() {
	c.t.Helper()
	srv := c.last()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodDelete, fmt.Sprintf("%s/v1/servers/%d", c.url, srv.ID), nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set(api.HeaderLease, srv.Lease)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		c.t.Fatalf("out-of-band release returned %d", resp.StatusCode)
	}
}

func (c *controller) factories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"ecoplus": providerserver.NewProtocol6WithError(New("test", WithBackoff(time.Millisecond))()),
	}
}

func (c *controller) config(body string) string {
	return fmt.Sprintf(`
provider "ecoplus" {
  endpoint = %q
  token    = %q
}

%s
`, c.url, testToken, body)
}
