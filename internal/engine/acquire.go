package engine

import (
	"context"

	"github.com/theocourtois/ecoplus/internal/api"
	"github.com/theocourtois/ecoplus/internal/fleet"
)

func (e *Engine) Acquire(ctx context.Context, req api.ServerRequest) (api.Server, bool, error) {
	for attempt := 0; ; attempt++ {
		id, lease, replay, err := e.Allocate(ctx, req)
		if err != nil {
			return api.Server{}, false, err
		}
		rec, ok, err := e.index.Await(ctx, id, func(r fleet.Record, ok bool) bool {
			return !ok || acquireSettled(r, lease)
		})
		if err != nil {
			return api.Server{}, false, upstream(err)
		}
		if ok && rec.Lease == lease && rec.State == fleet.StateBusy {
			return toAPI(rec), replay, nil
		}
		if replay || attempt >= 1 {
			return api.Server{}, false, api.NewError(api.CodeUpstream, "server could not be acquired")
		}
	}
}

func acquireSettled(r fleet.Record, lease string) bool {
	switch {
	case r.State == fleet.StateDeleting || r.State == fleet.StateQuarantine:
		return true
	case r.Lease != "" && r.Lease != lease:
		return true
	}
	return r.State == fleet.StateBusy && r.Lease == lease
}
