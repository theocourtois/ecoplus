package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/theocourtois/hetzner-ecoplus/internal/api"
	"github.com/theocourtois/hetzner-ecoplus/internal/cloud"
	"github.com/theocourtois/hetzner-ecoplus/internal/fleet"
)

func TestAcquireCreatesWhenPoolEmpty(t *testing.T) {
	h := newHarness(t)
	rec := h.mustAcquire(request("web", "ubuntu"))
	if rec.Reuse != 0 || rec.Name != "web" || rec.Image != "ubuntu" || h.labelsOf(rec.ID).State != fleet.StateBusy {
		t.Fatalf("unexpected %+v", rec)
	}
	if h.cloud.Calls("create") != 1 {
		t.Fatal("expected one create")
	}
}

func TestReleaseWipesAndReuses(t *testing.T) {
	h := newHarness(t)
	first := h.mustAcquire(request("a", "ubuntu"))
	h.cloud.WriteDisk(first.ID, "tenant secret")
	h.release(first)
	idle := h.labelsOf(first.ID)
	if idle.State != fleet.StateIdle || idle.Lease != "" || h.cloud.Disk(first.ID) != "pristine" {
		t.Fatalf("not wiped and idle: %+v disk=%q", idle, h.cloud.Disk(first.ID))
	}
	if _, err := h.eng.Get(first.ID, first.Lease); err == nil {
		t.Fatal("old lease must be fenced")
	}
	second := h.mustAcquire(request("b", "ubuntu"))
	if second.ID != first.ID || second.Reuse != 1 || second.Lease == first.Lease || second.Name != "b" {
		t.Fatalf("expected reuse of %d, got %+v", first.ID, second)
	}
	if h.cloud.Calls("create") != 1 {
		t.Fatal("reuse must not create")
	}
}

func TestIncompatiblePoolCreatesNew(t *testing.T) {
	h := newHarness(t)
	first := h.mustAcquire(request("a", "ubuntu"))
	h.release(first)
	other := request("b", "ubuntu")
	other.ServerType = "cx32"
	if second := h.mustAcquire(other); second.ID == first.ID {
		t.Fatal("different server type must not reuse")
	}
}

func TestReuseWithNewImageRebuilds(t *testing.T) {
	h := newHarness(t)
	first := h.mustAcquire(request("a", "ubuntu"))
	h.release(first)
	rebuilds := h.cloud.Calls("rebuild")
	second := h.mustAcquire(request("b", "debian"))
	if second.ID != first.ID || second.Image != "debian" || h.cloud.Calls("rebuild") != rebuilds+1 {
		t.Fatalf("expected rebuild reuse: %+v", second)
	}
}

func TestReapAtExpiry(t *testing.T) {
	h := newHarness(t)
	rec := h.mustAcquire(request("a", "ubuntu"))
	h.release(rec)
	expires := h.labelsOf(rec.ID).Expires
	h.clk.SetTime(time.Unix(expires-1, 0))
	h.settle()
	if !h.exists(rec.ID) {
		t.Fatal("must not reap before expiry")
	}
	h.advance(time.Second)
	if h.exists(rec.ID) {
		t.Fatal("must reap at expiry")
	}
	if _, ok := h.eng.Index().Get(rec.ID); ok {
		t.Fatal("index must drop reaped server")
	}
}

func TestReleaseTooLateDeletes(t *testing.T) {
	h := newHarness(t)
	rec := h.mustAcquire(request("a", "ubuntu"))
	h.advance(46 * time.Minute)
	h.release(rec)
	if h.exists(rec.ID) {
		t.Fatal("release near the billing boundary must delete")
	}
}

func TestClaimGuardSkipsAlmostExpired(t *testing.T) {
	h := newHarness(t)
	rec := h.mustAcquire(request("a", "ubuntu"))
	h.release(rec)
	h.clk.SetTime(time.Unix(h.labelsOf(rec.ID).Expires-60, 0))
	next := h.mustAcquire(request("b", "ubuntu"))
	if next.ID == rec.ID {
		t.Fatal("server inside the claim guard must not be claimed")
	}
}

func TestIdempotentReplayAndConflicts(t *testing.T) {
	h := newHarness(t)
	first := h.mustAcquire(request("web", "ubuntu"))
	again, replay, err := h.acquire(request("web", "ubuntu"))
	if err != nil || !replay || again.ID != first.ID || again.Lease != first.Lease {
		t.Fatalf("replay failed: %+v %v %v", again, replay, err)
	}
	_, _, err = h.acquire(request("web", "debian"))
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeConflict {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestForeignNameConflictAndUntouched(t *testing.T) {
	h := newHarness(t)
	foreign, _ := h.cloud.CreateServer(context.Background(), cloud.CreateSpec{Name: "taken", Labels: map[string]string{"team": "x"}})
	h.restart()
	_, _, err := h.acquire(request("taken", "ubuntu"))
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeConflict {
		t.Fatalf("expected conflict, got %v", err)
	}
	h.settle()
	got, _ := h.cloud.GetServer(context.Background(), foreign.ID)
	if got.Name != "taken" || len(got.Labels) != 1 {
		t.Fatalf("foreign server touched: %+v", got)
	}
}

func TestLeaseFencing(t *testing.T) {
	h := newHarness(t)
	rec := h.mustAcquire(request("a", "ubuntu"))
	ctx := context.Background()
	stale := "ffffffffffffffff"
	img := "other"
	if _, err := h.eng.Get(rec.ID, stale); err == nil {
		t.Fatal("stale get must fail")
	}
	if err := h.eng.Release(ctx, rec.ID, stale); err == nil {
		t.Fatal("stale release must fail")
	}
	if _, err := h.eng.Patch(ctx, rec.ID, stale, api.ServerPatch{Image: &img}); err == nil {
		t.Fatal("stale patch must fail")
	}
	if _, err := h.eng.Get(rec.ID, ""); err == nil {
		t.Fatal("empty lease must fail")
	}
	if h.cloud.Calls("rebuild") != 0 {
		t.Fatal("stale patch must not rebuild")
	}
}

func TestPatchImageRebuildsInPlace(t *testing.T) {
	h := newHarness(t)
	rec := h.mustAcquire(request("a", "ubuntu"))
	h.cloud.WriteDisk(rec.ID, "old")
	img := "debian"
	d := patchDesired(rec, rec.Lease, api.ServerPatch{Image: &img})
	h.eng.setDesired(rec.ID, d)
	h.settle()
	got := h.labelsOf(rec.ID)
	if got.State != fleet.StateBusy || got.Image != "debian" || got.Lease != rec.Lease || h.cloud.Disk(rec.ID) != "pristine" {
		t.Fatalf("unexpected %+v", got)
	}
}

func TestTransientFailureRetries(t *testing.T) {
	h := newHarness(t)
	h.cloud.FailOn("update", 1, cloud.Wrap(cloud.KindRateLimited, errors.New("429")))
	rec := h.mustAcquire(request("a", "ubuntu"))
	if h.labelsOf(rec.ID).State != fleet.StateBusy {
		t.Fatal("must recover after a 429")
	}
}

func TestPermanentRebuildFailureDeletes(t *testing.T) {
	h := newHarness(t)
	first := h.mustAcquire(request("a", "ubuntu"))
	h.release(first)
	h.cloud.FailOn("rebuild", h.cloud.Calls("rebuild")+1, cloud.Wrap(cloud.KindInvalid, errors.New("bad image")))
	id, lease, _, err := h.eng.Allocate(context.Background(), request("b", "bogus"))
	if err != nil || id != first.ID {
		t.Fatalf("expected claim of %d: %v", first.ID, err)
	}
	h.settle()
	if h.exists(first.ID) {
		t.Fatal("server with a failed claim rebuild must be deleted")
	}
	retry := h.mustAcquire(request("b", "ubuntu"))
	if retry.ID == first.ID || retry.Lease == lease {
		t.Fatal("fallback must create a new server")
	}
}
