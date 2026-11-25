package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/theocourtois/ecoplus/internal/api"
	"github.com/theocourtois/ecoplus/internal/cloud"
	"github.com/theocourtois/ecoplus/internal/fleet"
	"github.com/theocourtois/ecoplus/internal/labels"
)

const period = time.Minute

func (h *harness) guard(id string, pause func(context.Context) error) *Guard {
	return NewGuard(GuardOptions{
		Store: h.cloud, Clock: h.clk, InstanceID: id, Period: period, Halt: h.eng.Halt, Pause: pause,
	})
}

func noPause(context.Context) error { return nil }

func TestSentinelStartupRefusesFreshForeignWriter(t *testing.T) {
	h := newHarness(t)
	_ = h.cloud.WriteSentinel(context.Background(), cloud.Sentinel{Writer: "other", Heartbeat: h.clk.Now().Unix() - 30})
	err := h.guard("me", noPause).Check(context.Background())
	var foreign *ForeignWriterError
	if !errors.As(err, &foreign) || foreign.Writer != "other" || foreign.Error() == "" {
		t.Fatalf("expected foreign writer error, got %v", err)
	}
}

func TestSentinelStartupTakesOverStaleWriter(t *testing.T) {
	h := newHarness(t)
	_ = h.cloud.WriteSentinel(context.Background(), cloud.Sentinel{Writer: "other", Heartbeat: h.clk.Now().Unix() - int64((3 * period).Seconds())})
	if err := h.guard("me", noPause).Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s, _ := h.cloud.ReadSentinel(context.Background()); s.Writer != "me" || s.Heartbeat != h.clk.Now().Unix() {
		t.Fatalf("heartbeat not written: %+v", s)
	}
}

func TestSentinelStartupWithNoSentinelAndOwnWriter(t *testing.T) {
	h := newHarness(t)
	g := h.guard("me", noPause)
	if err := g.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.clk.Step(10 * time.Second)
	if err := g.Check(context.Background()); err != nil {
		t.Fatalf("own heartbeat must not count as foreign: %v", err)
	}
}

func TestForeignWriteDuringPauseHaltsEngine(t *testing.T) {
	h := newHarness(t)
	g := h.guard("me", func(ctx context.Context) error {
		return h.cloud.WriteSentinel(ctx, cloud.Sentinel{Writer: "intruder", Heartbeat: h.clk.Now().Unix()})
	})
	g.Tick(context.Background())
	if !h.eng.Halted() || h.eng.Ready() {
		t.Fatal("engine must halt and report not ready")
	}
}

func TestForeignWriterBetweenTicksHaltsEngine(t *testing.T) {
	h := newHarness(t)
	g := h.guard("me", noPause)
	g.Tick(context.Background())
	if h.eng.Halted() {
		t.Fatal("a lone writer must not halt")
	}
	_ = h.cloud.WriteSentinel(context.Background(), cloud.Sentinel{Writer: "intruder", Heartbeat: h.clk.Now().Unix()})
	g.Tick(context.Background())
	if !h.eng.Halted() {
		t.Fatal("foreign heartbeat must halt")
	}
}

func TestHaltedEngineStopsAllWrites(t *testing.T) {
	h := newHarness(t)
	rec := h.mustAcquire(request("a", "ubuntu"))
	h.eng.Halt()
	before := len(h.cloud.Log())
	h.eng.setDesired(rec.ID, fleet.Desired{Kind: fleet.DesiredRelease, Lease: rec.Lease})
	h.advance(10 * time.Hour)
	if len(h.cloud.Log()) != before {
		t.Fatalf("halted engine touched the cloud: %v", h.cloud.Log()[before:])
	}
	_, _, _, err := h.eng.Allocate(context.Background(), request("b", "ubuntu"))
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeNotReady {
		t.Fatalf("expected not_ready, got %v", err)
	}
}

func TestSentinelErrorsAreReportedNotFatal(t *testing.T) {
	h := newHarness(t)
	h.cloud.FailOn("readsentinel", 1, cloud.Wrap(cloud.KindUnavailable, errors.New("down")))
	g := h.guard("me", noPause)
	g.Tick(context.Background())
	if h.eng.Halted() {
		t.Fatal("transient sentinel errors must not halt")
	}
	h.cloud.FailOn("writesentinel", h.cloud.Calls("writesentinel")+1, cloud.Wrap(cloud.KindUnavailable, errors.New("down")))
	g.Tick(context.Background())
	if err := h.guard("me", func(context.Context) error { return context.Canceled }).Check(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("pause error must surface: %v", err)
	}
	h.cloud.FailOn("readsentinel", h.cloud.Calls("readsentinel")+2, cloud.Wrap(cloud.KindUnavailable, errors.New("down")))
	if err := g.Check(context.Background()); err == nil {
		t.Fatal("re-read failure must surface")
	}
}

func TestGuardRunTicksOnSchedule(t *testing.T) {
	h := newHarness(t)
	g := h.guard("me", noPause)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { g.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for h.cloud.Calls("writesentinel") == 0 && time.Now().Before(deadline) {
		h.clk.Step(period)
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if h.cloud.Calls("writesentinel") == 0 {
		t.Fatal("guard never wrote a heartbeat")
	}
}

type recorder struct {
	reused, created int
	transitions     map[[2]fleet.State]int
}

func (r *recorder) Acquired(reused bool) {
	if reused {
		r.reused++
	} else {
		r.created++
	}
}

func (r *recorder) Transition(from, to fleet.State) { r.transitions[[2]fleet.State{from, to}]++ }

func TestObserverAndStats(t *testing.T) {
	h := newHarness(t)
	obs := &recorder{transitions: map[[2]fleet.State]int{}}
	h.eng.opts.Observer = obs
	h.eng.opts.LeaseAlert = time.Hour
	first := h.mustAcquire(request("a", "ubuntu"))
	h.mustAcquire(request("b", "ubuntu"))
	h.release(first)
	h.mustAcquire(request("c", "ubuntu"))
	if obs.created != 2 || obs.reused != 1 || obs.transitions[[2]fleet.State{fleet.StateProvisioning, fleet.StateBusy}] != 2 {
		t.Fatalf("observer saw %+v", obs)
	}
	st := h.eng.Stats()
	if st.States[fleet.StateBusy] != 2 || st.LongLeases != 0 || st.Halted {
		t.Fatalf("stats %+v", st)
	}
	h.clk.Step(2 * time.Hour)
	if st := h.eng.Stats(); st.LongLeases != 2 {
		t.Fatalf("expected 2 long leases, got %+v", st)
	}
	if err := h.eng.Resync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.eng.alerted) != 2 {
		t.Fatal("long leases must be alerted once")
	}
}

func TestStatsCountQuarantine(t *testing.T) {
	h := newHarness(t)
	_, _ = h.cloud.CreateServer(context.Background(), cloud.CreateSpec{
		Name: "weird", Labels: map[string]string{labels.KeyOwner: labels.OwnerValue, labels.KeyState: "zombie"},
	})
	h.restart()
	if st := h.eng.Stats(); st.Quarantined != 1 {
		t.Fatalf("stats %+v", st)
	}
}

func TestAPIBudgetPriorities(t *testing.T) {
	h := newHarness(t)
	h.cloud.AddSSHKey("k")
	req := request("a", "ubuntu")
	req.SSHKeys = []string{"k"}
	rec := h.mustAcquire(req)
	h.release(rec)
	if err := h.eng.Resync(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.advance(2 * time.Hour)
	for _, op := range []string{"list", "listsshkeys"} {
		for _, p := range h.cloud.Priorities(op) {
			if p != cloud.Low {
				t.Fatalf("%s must be low priority, got %v", op, p)
			}
		}
	}
	for _, op := range []string{"create", "update", "rebuild", "delete", "awaitcreate"} {
		if len(h.cloud.Priorities(op)) == 0 {
			t.Fatalf("%s never called", op)
		}
		for _, p := range h.cloud.Priorities(op) {
			if p != cloud.High {
				t.Fatalf("%s must be high priority, got %v", op, p)
			}
		}
	}
}
