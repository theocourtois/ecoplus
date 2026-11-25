package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/theocourtois/hetzner-ecoplus/internal/api"
	"github.com/theocourtois/hetzner-ecoplus/internal/cloud"
	"github.com/theocourtois/hetzner-ecoplus/internal/fleet"
	"github.com/theocourtois/hetzner-ecoplus/internal/labels"
	"pgregory.net/rapid"
)

type model struct {
	h        *harness
	t        *rapid.T
	foreign  int64
	names    int
	lastGen  map[int64]uint64
	dead     []string
	marker   map[int64]string
	foreignL map[string]string
}

func newModel(t *rapid.T) *model {
	h := newHarness(t)
	f, err := h.cloud.CreateServer(context.Background(), cloud.CreateSpec{Name: "foreign", Labels: map[string]string{"team": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	h.restart()
	return &model{h: h, t: t, foreign: f.ID, lastGen: map[int64]uint64{}, marker: map[int64]string{}, foreignL: f.Labels}
}

func (m *model) busy() []fleet.Record {
	var out []fleet.Record
	for _, s := range m.h.cloud.All() {
		if rec, err := labels.Decode(s.Labels); err == nil && rec.State == fleet.StateBusy {
			rec.ID = s.ID
			rec.Name = s.Name
			out = append(out, rec)
		}
	}
	return out
}

func (m *model) acquire() {
	m.names++
	img := []string{"ubuntu", "debian"}[rapid.IntRange(0, 1).Draw(m.t, "image")]
	req := request(fmt.Sprintf("srv-%d", m.names), img)
	req.ServerType = []string{"cx22", "cx32"}[rapid.IntRange(0, 1).Draw(m.t, "type")]
	id, lease, _, err := m.h.eng.Allocate(context.Background(), req)
	if err != nil {
		return
	}
	m.h.settle()
	rec, ok := m.h.eng.Index().Get(id)
	if !ok || rec.State != fleet.StateBusy || rec.Lease != lease {
		return
	}
	if disk := m.h.cloud.Disk(id); disk != "pristine" {
		m.t.Fatalf("server %d handed out with disk %q", id, disk)
	}
	m.h.cloud.WriteDisk(id, "tenant:"+lease)
}

func (m *model) pickBusy() (fleet.Record, bool) {
	live := m.busy()
	if len(live) == 0 {
		return fleet.Record{}, false
	}
	return live[rapid.IntRange(0, len(live)-1).Draw(m.t, "pick")], true
}

func (m *model) release() {
	rec, ok := m.pickBusy()
	if !ok {
		return
	}
	m.h.eng.setDesired(rec.ID, fleet.Desired{Kind: fleet.DesiredRelease, Lease: rec.Lease})
	m.h.settle()
	m.dead = append(m.dead, rec.Lease)
}

func (m *model) patch() {
	rec, ok := m.pickBusy()
	if !ok {
		return
	}
	img := []string{"ubuntu", "debian", "fedora"}[rapid.IntRange(0, 2).Draw(m.t, "patchImage")]
	m.h.eng.setDesired(rec.ID, patchDesired(rec, rec.Lease, api.ServerPatch{Image: &img}))
	m.h.settle()
	if m.h.exists(rec.ID) && m.h.cloud.Disk(rec.ID) == "pristine" {
		m.h.cloud.WriteDisk(rec.ID, "tenant:"+rec.Lease)
	}
}

func (m *model) advance() {
	m.h.advance(time.Duration(rapid.IntRange(1, 70).Draw(m.t, "minutes")) * time.Minute)
}

func (m *model) inject() {
	op := []string{"update", "rebuild", "delete", "create", "awaitcreate"}[rapid.IntRange(0, 4).Draw(m.t, "op")]
	kind := []cloud.Kind{cloud.KindRateLimited, cloud.KindUnavailable}[rapid.IntRange(0, 1).Draw(m.t, "kind")]
	next := m.h.cloud.Calls(op) + 1 + rapid.IntRange(0, 3).Draw(m.t, "offset")
	m.h.cloud.FailOn(op, next, cloud.Wrap(kind, errors.New("injected")))
}

func (m *model) crash() {
	m.inject()
	m.names++
	_, _, _, _ = m.h.eng.Allocate(context.Background(), request(fmt.Sprintf("crash-%d", m.names), "ubuntu"))
	for _, r := range m.h.eng.Index().Snapshot() {
		_ = m.h.eng.Process(context.Background(), r.ID)
	}
	m.h.restart()
	m.h.settle()
}

func (m *model) staleLeases() {
	for _, lease := range m.dead {
		for _, s := range m.h.cloud.All() {
			if _, err := m.h.eng.Get(s.ID, lease); err == nil {
				m.t.Fatalf("stale lease %s still reads server %d", lease, s.ID)
			}
		}
	}
}

func (m *model) check() {
	m.t.Helper()
	leases := map[string]int64{}
	now := m.h.clk.Now().Unix()
	for _, s := range m.h.cloud.All() {
		if s.ID == m.foreign {
			if s.Name != "foreign" || len(s.Labels) != len(m.foreignL) {
				m.t.Fatalf("foreign server mutated: %+v", s)
			}
			continue
		}
		rec, err := labels.Decode(s.Labels)
		if err != nil {
			m.t.Fatalf("I4: server %d undecodable: %v", s.ID, err)
		}
		if rec.Gen < m.lastGen[s.ID] {
			m.t.Fatalf("I6: gen went backwards on %d: %d < %d", s.ID, rec.Gen, m.lastGen[s.ID])
		}
		m.lastGen[s.ID] = rec.Gen
		m.checkState(s.ID, rec, now, leases)
	}
	m.staleLeases()
}

func (m *model) checkState(id int64, rec fleet.Record, now int64, leases map[string]int64) {
	switch rec.State {
	case fleet.StateBusy:
		if other, dup := leases[rec.Lease]; dup || rec.Lease == "" {
			m.t.Fatalf("I1: lease %q on %d also on %d", rec.Lease, id, other)
		}
		leases[rec.Lease] = id
	case fleet.StateIdle:
		if m.h.cloud.Disk(id) != "pristine" {
			m.t.Fatalf("I2: idle server %d has disk %q", id, m.h.cloud.Disk(id))
		}
		if rec.Expires <= now {
			m.t.Fatalf("idle server %d past expiry %d <= %d", id, rec.Expires, now)
		}
	default:
		m.t.Fatalf("server %d left in transient state %s after settling", id, rec.State)
	}
}

func TestPropertyInvariants(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		m := newModel(t)
		defer m.h.eng.Stop()
		t.Repeat(map[string]func(*rapid.T){
			"acquire": func(*rapid.T) { m.acquire() },
			"release": func(*rapid.T) { m.release() },
			"patch":   func(*rapid.T) { m.patch() },
			"advance": func(*rapid.T) { m.advance() },
			"inject":  func(*rapid.T) { m.inject(); m.h.settle() },
			"crash":   func(*rapid.T) { m.crash() },
			"":        func(*rapid.T) { m.check() },
		})
	})
}
