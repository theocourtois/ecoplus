package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/theocourtois/hetzner-ecoplus/internal/api"
	"github.com/theocourtois/hetzner-ecoplus/internal/cloud"
	"github.com/theocourtois/hetzner-ecoplus/internal/fleet"
	"github.com/theocourtois/hetzner-ecoplus/internal/labels"
)

func (h *harness) seed(name string, rec fleet.Record) int64 {
	h.t.Helper()
	rec.Pool = "aaaaaaaaaaaaaaaa"
	if rec.Image == "" {
		rec.Image = "ubuntu"
	}
	s, err := h.cloud.CreateServer(context.Background(), cloud.CreateSpec{Name: name, Image: "ubuntu", Labels: labels.Encode(rec)})
	if err != nil {
		h.t.Fatal(err)
	}
	return s.ID
}

func TestRecoveryFromEveryIntermediateState(t *testing.T) {
	lease := "00000000000000aa"
	cases := map[string]struct {
		rec  fleet.Record
		want fleet.State
		gone bool
	}{
		"provisioning": {fleet.Record{State: fleet.StateProvisioning, Gen: 1, Lease: lease}, fleet.StateBusy, false},
		"claiming":     {fleet.Record{State: fleet.StateClaiming, Gen: 5, Lease: lease, Image: "debian"}, fleet.StateBusy, false},
		"wiping":       {fleet.Record{State: fleet.StateWiping, Gen: 5}, fleet.StateIdle, false},
		"deleting":     {fleet.Record{State: fleet.StateDeleting, Gen: 5}, "", true},
		"busy":         {fleet.Record{State: fleet.StateBusy, Gen: 5, Lease: lease}, fleet.StateBusy, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			id := h.seed("srv", tc.rec)
			h.cloud.WriteDisk(id, "dirty")
			h.restart()
			h.settle()
			if tc.gone {
				if h.exists(id) {
					t.Fatal("deleting server must be removed")
				}
				return
			}
			got := h.labelsOf(id)
			if got.State != tc.want || got.Gen < tc.rec.Gen {
				t.Fatalf("got %+v want state %s", got, tc.want)
			}
			if (tc.rec.State == fleet.StateWiping || tc.rec.State == fleet.StateClaiming) && h.cloud.Disk(id) != "pristine" {
				t.Fatal("rebuild must be redone")
			}
		})
	}
}

func TestRecoveryIdleIsScheduledForReap(t *testing.T) {
	h := newHarness(t)
	rec := h.mustAcquire(request("a", "ubuntu"))
	h.release(rec)
	h.restart()
	h.settle()
	if !h.exists(rec.ID) {
		t.Fatal("idle server must survive restart")
	}
	h.advance(2 * time.Hour)
	if h.exists(rec.ID) {
		t.Fatal("idle server must be reaped after restart")
	}
}

func TestQuarantineIsNeverTouched(t *testing.T) {
	h := newHarness(t)
	bad, _ := h.cloud.CreateServer(context.Background(), cloud.CreateSpec{
		Name: "weird", Labels: map[string]string{labels.KeyOwner: labels.OwnerValue, labels.KeyState: "zombie"},
	})
	h.restart()
	h.settle()
	h.advance(5 * time.Hour)
	got, err := h.cloud.GetServer(context.Background(), bad.ID)
	if err != nil || got.Name != "weird" || got.Labels[labels.KeyState] != "zombie" {
		t.Fatalf("quarantined server touched: %+v %v", got, err)
	}
	rec, _ := h.eng.Index().Get(bad.ID)
	if rec.State != fleet.StateQuarantine {
		t.Fatalf("expected quarantine, got %s", rec.State)
	}
	if _, _, err := h.acquire(request("fresh", "ubuntu")); err != nil {
		t.Fatal(err)
	}
}

func TestGenRegressionQuarantines(t *testing.T) {
	h := newHarness(t)
	rec := h.mustAcquire(request("a", "ubuntu"))
	old := h.labelsOf(rec.ID)
	old.Gen = 1
	if _, err := h.cloud.UpdateServer(context.Background(), rec.ID, cloud.UpdateSpec{Labels: labels.Encode(old)}); err != nil {
		t.Fatal(err)
	}
	if err := h.eng.Resync(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := h.eng.Index().Get(rec.ID)
	if got.State != fleet.StateQuarantine {
		t.Fatalf("expected quarantine, got %s", got.State)
	}
}

func TestResyncDropsVanishedServers(t *testing.T) {
	h := newHarness(t)
	rec := h.mustAcquire(request("a", "ubuntu"))
	if err := h.cloud.DeleteServer(context.Background(), rec.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.eng.Resync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.eng.Index().Get(rec.ID); ok {
		t.Fatal("vanished server must leave the index")
	}
}

func TestNotReadyBeforeBootstrap(t *testing.T) {
	h := newHarness(t)
	e := New(Options{Cloud: h.cloud, Clock: h.clk, Fleet: fleet.DefaultConfig()})
	_, _, _, err := e.Allocate(context.Background(), request("a", "ubuntu"))
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeNotReady {
		t.Fatalf("expected not_ready, got %v", err)
	}
	if _, err := e.Get(1, "x"); err == nil {
		t.Fatal("get must fail before ready")
	}
	e.Stop()
}

func TestUnknownSSHKeyRejected(t *testing.T) {
	h := newHarness(t)
	req := request("a", "ubuntu")
	req.SSHKeys = []string{"missing"}
	_, _, _, err := h.eng.Allocate(context.Background(), req)
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeInvalidRequest {
		t.Fatalf("expected invalid_request, got %v", err)
	}
	h.cloud.AddSSHKey("mine")
	req.SSHKeys = []string{"mine"}
	if _, _, _, err := h.eng.Allocate(context.Background(), req); err != nil {
		t.Fatal(err)
	}
}

func TestSSHKeyOrderSharesPool(t *testing.T) {
	h := newHarness(t)
	h.cloud.AddSSHKey("a")
	h.cloud.AddSSHKey("b")
	r1 := request("one", "ubuntu")
	r1.SSHKeys = []string{"a", "b"}
	first := h.mustAcquire(r1)
	h.release(first)
	r2 := request("two", "ubuntu")
	r2.SSHKeys = []string{"b", "a", "a"}
	if second := h.mustAcquire(r2); second.ID != first.ID {
		t.Fatal("ssh key order and duplicates must not split the pool")
	}
}

func TestConcurrentAcquiresNeverShareAServer(t *testing.T) {
	h := newHarness(t)
	var seed []fleet.Record
	for i := 0; i < 5; i++ {
		seed = append(seed, h.mustAcquire(request("seed-"+string(rune('a'+i)), "ubuntu")))
	}
	for _, r := range seed {
		h.release(r)
	}
	h.restart()
	if err := h.eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer h.eng.Stop()
	var wg sync.WaitGroup
	results := make([]api.Server, 20)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv, _, err := h.eng.Acquire(context.Background(), request(fmt.Sprintf("c-%d", i), "ubuntu"))
			if err != nil {
				t.Error(err)
			}
			results[i] = srv
		}()
	}
	wg.Wait()
	ids, leases := map[int64]bool{}, map[string]bool{}
	for _, r := range results {
		if ids[r.ID] || leases[r.Lease] {
			t.Fatalf("server or lease shared: %+v", r)
		}
		ids[r.ID], leases[r.Lease] = true, true
	}
	reused := 0
	for _, r := range results {
		if r.Reused {
			reused++
		}
	}
	if reused != 5 {
		t.Fatalf("expected the 5 idle servers reused, got %d", reused)
	}
}

func TestStartedEngineServesLifecycle(t *testing.T) {
	h := newHarness(t)
	if err := h.eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer h.eng.Stop()
	ctx := context.Background()
	srv, replay, err := h.eng.Acquire(ctx, request("a", "ubuntu"))
	if err != nil || replay {
		t.Fatal(err)
	}
	img := "debian"
	patched, err := h.eng.Patch(ctx, srv.ID, srv.Lease, api.ServerPatch{Image: &img})
	if err != nil || patched.Image != "debian" || patched.ID != srv.ID {
		t.Fatalf("patch: %+v %v", patched, err)
	}
	name := "renamed"
	labelsPatch := map[string]string{"team": "x"}
	patched, err = h.eng.Patch(ctx, srv.ID, srv.Lease, api.ServerPatch{Name: &name, Labels: &labelsPatch})
	if err != nil || patched.Name != "renamed" || patched.Labels["team"] != "x" {
		t.Fatalf("patch: %+v %v", patched, err)
	}
	if got, err := h.eng.Get(srv.ID, srv.Lease); err != nil || got.Name != "renamed" {
		t.Fatalf("get: %+v %v", got, err)
	}
	if err := h.eng.Release(ctx, srv.ID, srv.Lease); err != nil {
		t.Fatal(err)
	}
	if _, err := h.eng.Get(srv.ID, srv.Lease); err == nil {
		t.Fatal("released lease must be fenced")
	}
}
