package fake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/theocourtois/ecoplus/internal/cloud"
	clocktesting "k8s.io/utils/clock/testing"
)

func newFake() (*Cloud, *clocktesting.FakeClock) {
	clk := clocktesting.NewFakeClock(time.Unix(1_000_000, 0))
	return New(clk), clk
}

func create(t *testing.T, c *Cloud, name string, labels map[string]string) cloud.Server {
	t.Helper()
	s, err := c.CreateServer(context.Background(), cloud.CreateSpec{Name: name, ServerType: "cx22", Location: "nbg1", Image: "img", Labels: labels})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCreateAssignsIdentityAndClock(t *testing.T) {
	c, clk := newFake()
	a := create(t, c, "a", map[string]string{"k": "v"})
	clk.Step(time.Minute)
	b := create(t, c, "b", nil)
	if a.ID == b.ID || !b.Created.After(a.Created) || a.IPv6 == "" || a.IPv4 == "" {
		t.Fatalf("bad servers %+v %+v", a, b)
	}
}

func TestNameUniqueness(t *testing.T) {
	c, _ := newFake()
	create(t, c, "a", nil)
	if _, err := c.CreateServer(context.Background(), cloud.CreateSpec{Name: "a"}); !cloud.IsConflict(err) {
		t.Fatalf("expected conflict, got %v", err)
	}
	b := create(t, c, "b", nil)
	if _, err := c.UpdateServer(context.Background(), b.ID, cloud.UpdateSpec{Name: "a"}); !cloud.IsConflict(err) {
		t.Fatalf("expected conflict on rename, got %v", err)
	}
}

func TestUpdateReplacesWholeLabelMap(t *testing.T) {
	c, _ := newFake()
	s := create(t, c, "a", map[string]string{"x": "1", "y": "2"})
	got, err := c.UpdateServer(context.Background(), s.ID, cloud.UpdateSpec{Labels: map[string]string{"z": "3"}})
	if err != nil || len(got.Labels) != 1 || got.Labels["z"] != "3" {
		t.Fatalf("labels not replaced: %+v %v", got, err)
	}
	same, _ := c.UpdateServer(context.Background(), s.ID, cloud.UpdateSpec{})
	if same.Labels["z"] != "3" {
		t.Fatal("nil labels must keep the map")
	}
}

func TestDeleteThenNotFound(t *testing.T) {
	c, _ := newFake()
	s := create(t, c, "a", nil)
	ctx := context.Background()
	if err := c.DeleteServer(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetServer(ctx, s.ID); !cloud.IsNotFound(err) {
		t.Fatalf("expected not found, got %v", err)
	}
	if err := c.DeleteServer(ctx, s.ID); !cloud.IsNotFound(err) {
		t.Fatalf("second delete must report not found, got %v", err)
	}
	if _, err := c.UpdateServer(ctx, s.ID, cloud.UpdateSpec{}); !cloud.IsNotFound(err) {
		t.Fatal("update after delete must be not found")
	}
	if err := c.RebuildServer(ctx, s.ID, "x"); !cloud.IsNotFound(err) {
		t.Fatal("rebuild after delete must be not found")
	}
	if err := c.AwaitCreate(ctx, s.ID); !cloud.IsNotFound(err) {
		t.Fatal("await after delete must be not found")
	}
	if _, err := c.GetServerByName(ctx, "a"); !cloud.IsNotFound(err) {
		t.Fatal("name must be free after delete")
	}
}

func TestRebuildWipesDiskAndSetsImage(t *testing.T) {
	c, _ := newFake()
	s := create(t, c, "a", nil)
	c.WriteDisk(s.ID, "secret")
	if err := c.RebuildServer(context.Background(), s.ID, "other"); err != nil {
		t.Fatal(err)
	}
	got, _ := c.GetServer(context.Background(), s.ID)
	if c.Disk(s.ID) != "pristine" || got.Image != "other" {
		t.Fatalf("disk %q image %q", c.Disk(s.ID), got.Image)
	}
}

func TestListSelectorAndSSHKeys(t *testing.T) {
	c, _ := newFake()
	create(t, c, "a", map[string]string{"ecoplus/owner": "ecoplus"})
	create(t, c, "b", map[string]string{"other": "1"})
	ctx := context.Background()
	if got, _ := c.ListServers(ctx, "ecoplus/owner=ecoplus"); len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("selector broken: %+v", got)
	}
	if got, _ := c.ListServers(ctx, ""); len(got) != 2 {
		t.Fatal("empty selector lists all")
	}
	id := c.AddSSHKey("k")
	if keys, _ := c.ListSSHKeys(ctx); len(keys) != 1 || keys[0].ID != id {
		t.Fatalf("keys %+v", keys)
	}
}

func TestFailureInjectionByOperationAndCall(t *testing.T) {
	c, _ := newFake()
	boom := cloud.Wrap(cloud.KindRateLimited, errors.New("429"))
	c.FailOn("create", 2, boom)
	ctx := context.Background()
	if _, err := c.CreateServer(ctx, cloud.CreateSpec{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateServer(ctx, cloud.CreateSpec{Name: "b"}); !errors.Is(err, boom) {
		t.Fatalf("second call must fail, got %v", err)
	}
	if _, err := c.CreateServer(ctx, cloud.CreateSpec{Name: "b"}); err != nil {
		t.Fatalf("third call must pass, got %v", err)
	}
	if c.Calls("create") != 3 || len(c.Log()) != 3 || len(c.All()) != 2 {
		t.Fatalf("calls=%d log=%v all=%d", c.Calls("create"), c.Log(), len(c.All()))
	}
}

func TestErrorKinds(t *testing.T) {
	if cloud.IsTransient(cloud.Wrap(cloud.KindNotFound, errors.New("x"))) || cloud.IsTransient(nil) ||
		!cloud.IsTransient(cloud.Wrap(cloud.KindRateLimited, errors.New("x"))) ||
		!cloud.IsTransient(cloud.Wrap(cloud.KindUnavailable, errors.New("x"))) ||
		!cloud.IsTransient(errors.New("plain")) || cloud.Wrap(cloud.KindInvalid, nil).Error() == "" {
		t.Fatal("transient classification wrong")
	}
	if cloud.PriorityOf(context.Background()) != cloud.High ||
		cloud.PriorityOf(cloud.WithPriority(context.Background(), cloud.Low)) != cloud.Low {
		t.Fatal("priority context broken")
	}
}
