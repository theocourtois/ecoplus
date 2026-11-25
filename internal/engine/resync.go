package engine

import (
	"context"

	"github.com/theocourtois/ecoplus/internal/cloud"
	"github.com/theocourtois/ecoplus/internal/fleet"
	"github.com/theocourtois/ecoplus/internal/labels"
)

func (e *Engine) Resync(ctx context.Context) error {
	mark := e.index.Mark()
	servers, err := e.opts.Cloud.ListServers(cloud.WithPriority(ctx, cloud.Low), labels.Selector)
	if err != nil {
		return err
	}
	seen := make(map[int64]struct{}, len(servers))
	for _, s := range servers {
		seen[s.ID] = struct{}{}
		e.absorb(s, mark)
	}
	for _, r := range e.index.Snapshot() {
		if _, ok := seen[r.ID]; ok {
			continue
		}
		if e.index.DeleteIfStale(r.ID, mark) {
			e.clearDesired(r.ID)
		}
	}
	e.alertLongLeases()
	return nil
}

func (e *Engine) absorb(s cloud.Server, mark uint64) {
	rec, err := recordFromServer(s)
	if err != nil {
		e.opts.Logger.Error("quarantine: undecodable labels", "server_id", s.ID, "error", err)
	}
	stored, outcome := e.index.Absorb(rec, mark, err == nil)
	switch outcome {
	case AbsorbRegressed:
		e.opts.Logger.Error("quarantine: gen went backwards", "server_id", s.ID, "gen", rec.Gen, "max_gen", e.index.MaxGen(s.ID))
	case AbsorbSkipped, AbsorbUnchanged:
		return
	}
	if stored.State != fleet.StateQuarantine {
		e.queue.Add(stored.ID)
	}
}
