package hetzner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/theocourtois/ecoplus/internal/cloud"
)

func TestListPaginatesAndSendsSelector(t *testing.T) {
	m := &mock{}
	c := newClient(t, m)
	got, err := c.ListServers(context.Background(), "ecoplus/owner=ecoplus")
	if err != nil || len(got) != 2 || got[0].ID != 1 || got[1].Name != "two" {
		t.Fatalf("got %+v %v", got, err)
	}
	if !strings.Contains(m.requests[0], "label_selector=ecoplus%2Fowner%3Decoplus") || !strings.Contains(m.requests[0], "per_page=50") {
		t.Fatalf("request mapping wrong: %v", m.requests)
	}
	if len(m.requests) != 2 {
		t.Fatalf("expected two pages, got %v", m.requests)
	}
}

func TestServerMapping(t *testing.T) {
	c := newClient(t, &mock{})
	s, err := c.GetServer(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != 5 || s.Name != "five" || s.ServerType != "cx22" || s.Location != "nbg1" || s.Image != "ubuntu-24.04" ||
		s.IPv4 != "192.0.2.7" || s.IPv6 != "2001:db8:1::1" || s.Created.Year() != 2026 {
		t.Fatalf("bad mapping %+v", s)
	}
}

func TestNotFoundMapping(t *testing.T) {
	c := newClient(t, &mock{})
	if _, err := c.GetServer(context.Background(), 404); !cloud.IsNotFound(err) {
		t.Fatalf("expected not found, got %v", err)
	}
	if err := c.DeleteServer(context.Background(), 404); !cloud.IsNotFound(err) {
		t.Fatalf("delete of missing server must be not found, got %v", err)
	}
}

func TestRateLimitMappingAfterRetries(t *testing.T) {
	m := &mock{}
	c := newClient(t, m)
	_, err := c.GetServer(context.Background(), 429)
	if cloud.KindOf(err) != cloud.KindRateLimited {
		t.Fatalf("expected rate limited, got %v", err)
	}
	if m.limited < 2 {
		t.Fatalf("retry options must retry 429s, saw %d", m.limited)
	}
}

func TestCreateMapsRequestAndConflict(t *testing.T) {
	m := &mock{}
	c := newClient(t, m)
	s, err := c.CreateServer(context.Background(), cloud.CreateSpec{
		Name: "new", ServerType: "cx22", Location: "nbg1", Image: "ubuntu-24.04", SSHKeyIDs: []int64{11},
		UserData: "#cloud-config", Labels: map[string]string{"a": "b"},
	})
	if err != nil || s.ID != 7 {
		t.Fatalf("got %+v %v", s, err)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(m.bodies[0]), &sent); err != nil {
		t.Fatal(err)
	}
	if sent["server_type"] != "cx22" || sent["location"] != "nbg1" || sent["image"] != "ubuntu-24.04" ||
		sent["user_data"] != "#cloud-config" || fmt.Sprint(sent["ssh_keys"]) != "[11]" {
		t.Fatalf("bad create body: %v", sent)
	}
	if err := c.AwaitCreate(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateServer(context.Background(), cloud.CreateSpec{Name: "dup", ServerType: "x", Location: "y", Image: "z"}); !cloud.IsConflict(err) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestAwaitCreateWithoutActionPollsStatus(t *testing.T) {
	c := newClient(t, &mock{})
	if err := c.AwaitCreate(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateRebuildDeleteAndKeys(t *testing.T) {
	m := &mock{}
	c := newClient(t, m)
	ctx := context.Background()
	s, err := c.UpdateServer(ctx, 5, cloud.UpdateSpec{Name: "renamed", Labels: map[string]string{"k": "v"}})
	if err != nil || s.Name != "renamed" || s.Labels["k"] != "v" {
		t.Fatalf("%+v %v", s, err)
	}
	if err := c.RebuildServer(ctx, 5, "debian-12"); err != nil {
		t.Fatal(err)
	}
	if err := c.RebuildServer(ctx, 5, "123"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.bodies[1], `"image":"debian-12"`) || !strings.Contains(m.bodies[2], `"image":123`) {
		t.Fatalf("rebuild body: %v", m.bodies)
	}
	if err := c.DeleteServer(ctx, 5); err != nil {
		t.Fatal(err)
	}
	keys, err := c.ListSSHKeys(ctx)
	if err != nil || len(keys) != 1 || keys[0].Name != "mine" || keys[0].ID != 11 {
		t.Fatalf("%+v %v", keys, err)
	}
}

func TestLowPriorityWaitsForBudgetHighDoesNot(t *testing.T) {
	m := &mock{remaining: "5"}
	c := newClient(t, m)
	if _, err := c.GetServer(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetServer(context.Background(), 5); err != nil {
		t.Fatalf("high priority must pass below the watermark: %v", err)
	}
	ctx, cancel := context.WithTimeout(cloud.WithPriority(context.Background(), cloud.Low), 100*time.Millisecond)
	defer cancel()
	if _, err := c.ListServers(ctx, ""); err == nil {
		t.Fatal("low priority must wait while the budget is below the watermark")
	}
}

func TestLowPriorityPassesWithBudget(t *testing.T) {
	m := &mock{remaining: "3000"}
	c := newClient(t, m)
	if _, err := c.GetServer(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListServers(cloud.WithPriority(context.Background(), cloud.Low), ""); err != nil {
		t.Fatal(err)
	}
}

func TestClientSatisfiesCloudInterface(_ *testing.T) {
	var _ cloud.Cloud = (*Client)(nil)
}

func TestRemainingTracksResponseHeader(t *testing.T) {
	m := &mock{remaining: "321"}
	c := newClient(t, m)
	if c.Remaining() != -1 {
		t.Fatal("unknown before first response")
	}
	if _, err := c.GetServer(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	if c.Remaining() != 321 {
		t.Fatalf("got %d", c.Remaining())
	}
}
