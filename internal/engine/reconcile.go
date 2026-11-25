package engine

import (
	"context"
	"fmt"

	"github.com/theocourtois/hetzner-ecoplus/internal/cloud"
	"github.com/theocourtois/hetzner-ecoplus/internal/fleet"
	"github.com/theocourtois/hetzner-ecoplus/internal/labels"
)

func (e *Engine) write(ctx context.Context, next fleet.Record, name string) (fleet.Record, error) {
	prev, _ := e.index.Get(next.ID)
	srv, err := e.opts.Cloud.UpdateServer(ctx, next.ID, cloud.UpdateSpec{Name: name, Labels: labels.Encode(next)})
	if err != nil {
		return fleet.Record{}, fmt.Errorf("update server %d: %w", next.ID, err)
	}
	next.Name = srv.Name
	e.index.Put(next)
	e.opts.Observer.Transition(prev.State, next.State)
	e.opts.Logger.Info("transition", "server_id", next.ID, "from", prev.State, "to", next.State, "gen", next.Gen, "lease", next.Lease)
	return next, nil
}

func (e *Engine) effect(ctx context.Context, id int64, eff fleet.Effect) error {
	switch eff.Kind {
	case fleet.EffectAwaitCreate:
		return e.opts.Cloud.AwaitCreate(ctx, id)
	case fleet.EffectRebuild:
		return e.opts.Cloud.RebuildServer(ctx, id, eff.Image)
	case fleet.EffectDelete:
		err := e.opts.Cloud.DeleteServer(ctx, id)
		if err != nil && !cloud.IsNotFound(err) {
			return err
		}
		e.index.Delete(id)
		e.clearDesired(id)
	}
	return nil
}

func consumes(state fleet.State, d fleet.Desired) bool {
	if d.Kind == fleet.DesiredNone {
		return false
	}
	return d.Kind == fleet.DesiredAbort || state == fleet.StateBusy || state == fleet.StateIdle
}

func (e *Engine) Process(ctx context.Context, id int64) error {
	if e.halted.Load() {
		return nil
	}
	rec, ok := e.index.Get(id)
	if !ok {
		return nil
	}
	d := e.desiredFor(id)
	plan := fleet.Step(rec, d, e.opts.Clock.Now(), e.opts.Fleet)
	if plan.Noop() {
		if consumes(rec.State, d) {
			e.clearDesired(id)
		}
		if plan.Requeue > 0 {
			e.queue.AddAfter(id, plan.Requeue)
		}
		return nil
	}
	if err := e.execute(ctx, rec, d, plan); err != nil {
		return e.handleFailure(id, rec, err)
	}
	e.queue.Add(id)
	return nil
}

func (e *Engine) execute(ctx context.Context, rec fleet.Record, d fleet.Desired, plan fleet.Plan) error {
	name := plan.Name
	consumed := consumes(rec.State, d)
	if plan.Intent != nil {
		if _, err := e.write(ctx, *plan.Intent, name); err != nil {
			return err
		}
		name = ""
		e.clearDesired(rec.ID)
	}
	if err := e.effect(ctx, rec.ID, plan.Effect); err != nil {
		return fmt.Errorf("effect on server %d: %w", rec.ID, err)
	}
	if plan.Commit != nil {
		if _, err := e.write(ctx, *plan.Commit, name); err != nil {
			return err
		}
	}
	if consumed {
		e.clearDesired(rec.ID)
	}
	return nil
}

func (e *Engine) handleFailure(id int64, rec fleet.Record, err error) error {
	switch {
	case cloud.IsNotFound(err):
		e.index.Delete(id)
		e.clearDesired(id)
		return nil
	case cloud.IsTransient(err):
		e.queue.AddRateLimited(id)
		return err
	}
	e.opts.Logger.Error("permanent failure, deleting server", "server_id", id, "state", rec.State, "error", err)
	e.setDesired(id, fleet.Desired{Kind: fleet.DesiredAbort})
	e.queue.Add(id)
	return err
}
