package engine

import "github.com/theocourtois/hetzner-ecoplus/internal/fleet"

type Stats struct {
	States      map[fleet.State]int
	LongLeases  int
	QueueDepth  int
	Halted      bool
	Quarantined int
}

func (e *Engine) leaseExceeded(r fleet.Record) bool {
	if e.opts.LeaseAlert <= 0 || r.State != fleet.StateBusy || r.LeasedAt == 0 {
		return false
	}
	return e.opts.Clock.Now().Unix()-r.LeasedAt > int64(e.opts.LeaseAlert.Seconds())
}

func (e *Engine) Stats() Stats {
	st := Stats{States: map[fleet.State]int{}, QueueDepth: e.queue.Len(), Halted: e.halted.Load()}
	for _, r := range e.index.Snapshot() {
		st.States[r.State]++
		if r.State == fleet.StateQuarantine {
			st.Quarantined++
		}
		if e.leaseExceeded(r) {
			st.LongLeases++
		}
	}
	return st
}

func (e *Engine) alertLongLeases() {
	for _, r := range e.index.Snapshot() {
		if !e.leaseExceeded(r) {
			continue
		}
		if _, done := e.alerted[r.ID]; done {
			continue
		}
		e.alerted[r.ID] = struct{}{}
		e.opts.Logger.Warn("alert: lease older than the configured limit", "server_id", r.ID, "lease", r.Lease, "leased_at", r.LeasedAt)
	}
}
