package engine

import (
	"context"

	"github.com/theocourtois/ecoplus/internal/api"
	"github.com/theocourtois/ecoplus/internal/fleet"
)

func (e *Engine) owned(id int64, lease string) (fleet.Record, error) {
	if !e.Ready() {
		return fleet.Record{}, notReady()
	}
	rec, ok := e.index.Get(id)
	if !ok || lease == "" || rec.Lease != lease {
		return fleet.Record{}, notFound()
	}
	return rec, nil
}

func (e *Engine) Get(id int64, lease string) (api.Server, error) {
	rec, err := e.owned(id, lease)
	if err != nil {
		return api.Server{}, err
	}
	return toAPI(rec), nil
}

func (e *Engine) Release(ctx context.Context, id int64, lease string) error {
	if _, err := e.owned(id, lease); err != nil {
		return err
	}
	e.setDesired(id, fleet.Desired{Kind: fleet.DesiredRelease, Lease: lease})
	e.queue.Add(id)
	_, _, err := e.index.Await(ctx, id, func(r fleet.Record, ok bool) bool { return !ok || r.Lease != lease })
	if err != nil {
		return upstream(err)
	}
	return nil
}

func (e *Engine) Patch(ctx context.Context, id int64, lease string, p api.ServerPatch) (api.Server, error) {
	rec, err := e.owned(id, lease)
	if err != nil {
		return api.Server{}, err
	}
	if verr := p.Validate(); verr != nil {
		return api.Server{}, verr
	}
	d := patchDesired(rec, lease, p)
	if other, ok := e.index.ByName(d.Name); ok && other.ID != id {
		return api.Server{}, api.NewError(api.CodeConflict, "name %q is already used", d.Name)
	}
	e.setDesired(id, d)
	e.queue.Add(id)
	done, ok, err := e.index.Await(ctx, id, func(r fleet.Record, ok bool) bool {
		return !ok || r.Lease != lease || (r.State == fleet.StateBusy && r.Image == d.Image && r.Name == d.Name && r.Req == d.Req &&
			(p.Labels == nil || tenantEqual(r.Tenant, d.Tenant)))
	})
	if err != nil {
		return api.Server{}, upstream(err)
	}
	if !ok || done.Lease != lease {
		return api.Server{}, notFound()
	}
	return toAPI(done), nil
}

func patchDesired(rec fleet.Record, lease string, p api.ServerPatch) fleet.Desired {
	d := fleet.Desired{Kind: fleet.DesiredPatch, Lease: lease, Name: rec.Name, Image: rec.Image}
	if p.Name != nil {
		d.Name = *p.Name
	}
	if p.Image != nil {
		d.Image = *p.Image
	}
	if p.Labels != nil {
		d.Tenant = *p.Labels
		if d.Tenant == nil {
			d.Tenant = map[string]string{}
		}
	}
	d.Req = fleet.RequestHash(rec.Pool, d.Image, d.Name)
	return d
}

func tenantEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}
