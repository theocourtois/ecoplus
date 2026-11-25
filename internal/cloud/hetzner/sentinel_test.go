package hetzner

import (
	"context"
	"strings"
	"testing"

	"github.com/theocourtois/ecoplus/internal/cloud"
)

func TestGeneratedSentinelKeyIsValidAuthorizedKey(t *testing.T) {
	key, err := generatePublicKey()
	if err != nil || !strings.HasPrefix(key, "ssh-ed25519 ") {
		t.Fatalf("got %q %v", key, err)
	}
}

func TestSentinelRoundTrip(t *testing.T) {
	m := &mock{}
	c := newClient(t, m)
	ctx := context.Background()
	if s, err := c.ReadSentinel(ctx); err != nil || s.Writer != "" {
		t.Fatalf("absent sentinel must read empty: %+v %v", s, err)
	}
	if err := c.WriteSentinel(ctx, cloud.Sentinel{Writer: "me", Heartbeat: 42}); err != nil {
		t.Fatal(err)
	}
	var create string
	for i, r := range m.requests {
		if r == "POST /ssh_keys" {
			create = m.bodies[i]
		}
	}
	if !strings.Contains(create, `"name":"ecoplus-sentinel"`) || !strings.Contains(create, `"ecoplus/writer":"me"`) ||
		!strings.Contains(create, `"ecoplus/heartbeat":"42"`) {
		t.Fatalf("sentinel create body: %q", create)
	}
}

func TestSentinelUpdateAndRead(t *testing.T) {
	m := &mock{sentinel: true}
	c := newClient(t, m)
	ctx := context.Background()
	s, err := c.ReadSentinel(ctx)
	if err != nil || s.Writer != "other" || s.Heartbeat != 77 {
		t.Fatalf("%+v %v", s, err)
	}
	if err := c.WriteSentinel(ctx, cloud.Sentinel{Writer: "me", Heartbeat: 99}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.requests[len(m.requests)-1], "PUT /ssh_keys/21") {
		t.Fatalf("expected update, got %v", m.requests)
	}
}
