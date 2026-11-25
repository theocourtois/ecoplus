package engine

import (
	"context"
	"sync"
	"time"

	"github.com/theocourtois/ecoplus/internal/api"
	"github.com/theocourtois/ecoplus/internal/cloud"
	"github.com/theocourtois/ecoplus/internal/cloud/fake"
	"github.com/theocourtois/ecoplus/internal/fleet"
	"github.com/theocourtois/ecoplus/internal/labels"
	clocktesting "k8s.io/utils/clock/testing"
)

type tester interface {
	Helper()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
}

type harness struct {
	t     tester
	cloud *fake.Cloud
	clk   *clocktesting.FakeClock
	eng   *Engine
	n     int
	mu    sync.Mutex
}

func newHarness(t tester) *harness {
	t.Helper()
	clk := clocktesting.NewFakeClock(time.Unix(10_000_000, 0))
	h := &harness{t: t, cloud: fake.New(clk), clk: clk}
	h.restart()
	return h
}

func (h *harness) restart() {
	if h.eng != nil {
		h.eng.Stop()
	}
	h.eng = New(Options{Cloud: h.cloud, Clock: h.clk, Fleet: fleet.DefaultConfig(), Workers: 4, Resync: 0, NewLease: h.lease})
	if err := h.eng.Bootstrap(context.Background()); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) lease() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.n++
	return leaseOf(h.n)
}

func leaseOf(n int) string {
	const digits = "0123456789abcdef"
	out := []byte("0000000000000000")
	for i := 15; n > 0 && i >= 0; i-- {
		out[i] = digits[n%16]
		n /= 16
	}
	return string(out)
}

func (h *harness) settle() {
	h.t.Helper()
	ctx := context.Background()
	for pass := 0; pass < 200; pass++ {
		before := h.eng.Index().Mark()
		failed := false
		for _, r := range h.eng.Index().Snapshot() {
			if err := h.eng.Process(ctx, r.ID); err != nil {
				failed = true
			}
		}
		if !failed && h.eng.Index().Mark() == before {
			return
		}
	}
	h.t.Fatal("engine did not settle")
}

func request(name, image string) api.ServerRequest {
	return api.ServerRequest{Name: name, ServerType: "cx22", Location: "nbg1", Image: image}
}

func (h *harness) acquire(req api.ServerRequest) (fleet.Record, bool, error) {
	h.t.Helper()
	id, lease, replay, err := h.eng.Allocate(context.Background(), req)
	if err != nil {
		return fleet.Record{}, false, err
	}
	h.settle()
	rec, ok := h.eng.Index().Get(id)
	if !ok || rec.Lease != lease || rec.State != fleet.StateBusy {
		h.t.Fatalf("server %d not busy for lease %s: %+v", id, lease, rec)
	}
	return rec, replay, nil
}

func (h *harness) mustAcquire(req api.ServerRequest) fleet.Record {
	h.t.Helper()
	rec, _, err := h.acquire(req)
	if err != nil {
		h.t.Fatal(err)
	}
	return rec
}

func (h *harness) release(rec fleet.Record) {
	h.t.Helper()
	h.eng.setDesired(rec.ID, fleet.Desired{Kind: fleet.DesiredRelease, Lease: rec.Lease})
	h.settle()
}

func (h *harness) advance(d time.Duration) {
	h.clk.Step(d)
	h.settle()
}

func (h *harness) labelsOf(id int64) fleet.Record {
	h.t.Helper()
	s, err := h.cloud.GetServer(context.Background(), id)
	if err != nil {
		h.t.Fatal(err)
	}
	rec, err := labels.Decode(s.Labels)
	if err != nil {
		h.t.Fatal(err)
	}
	return rec
}

func (h *harness) exists(id int64) bool {
	_, err := h.cloud.GetServer(context.Background(), id)
	return !cloud.IsNotFound(err)
}
