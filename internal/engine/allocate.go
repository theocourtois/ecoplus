package engine

import (
	"context"

	"github.com/theocourtois/hetzner-ecoplus/internal/api"
	"github.com/theocourtois/hetzner-ecoplus/internal/cloud"
	"github.com/theocourtois/hetzner-ecoplus/internal/fleet"
	"github.com/theocourtois/hetzner-ecoplus/internal/labels"
)

type allocation struct {
	id     int64
	lease  string
	replay bool
	reused bool
}

type acquireInput struct {
	req   api.ServerRequest
	spec  fleet.PoolSpec
	pool  string
	hash  string
	lease string
}

func (e *Engine) Allocate(ctx context.Context, req api.ServerRequest) (id int64, lease string, replay bool, err error) {
	if !e.Ready() {
		return 0, "", false, notReady()
	}
	if verr := req.Validate(); verr != nil {
		return 0, "", false, verr
	}
	ids, err := e.resolveKeys(ctx, req.SSHKeys)
	if err != nil {
		return 0, "", false, err
	}
	spec := fleet.PoolSpec{ServerType: req.ServerType, Location: req.Location, SSHKeyIDs: ids, UserData: req.UserData}
	pool := fleet.PoolKey(spec)
	in := acquireInput{req: req, spec: spec, pool: pool, hash: fleet.RequestHash(pool, req.Image, req.Name), lease: e.opts.NewLease()}
	a, err := e.allocate(ctx, in)
	if err == nil && !a.replay {
		e.opts.Observer.Acquired(a.reused)
	}
	return a.id, a.lease, a.replay, err
}

func (e *Engine) allocate(ctx context.Context, in acquireInput) (allocation, error) {
	e.alloc.Lock()
	defer e.alloc.Unlock()
	if existing, ok := e.index.ByName(in.req.Name); ok {
		if existing.Req == in.hash && existing.Lease != "" {
			return allocation{id: existing.ID, lease: existing.Lease, replay: true}, nil
		}
		return allocation{}, api.NewError(api.CodeConflict, "name %q is already used", in.req.Name)
	}
	if id, ok := e.claimCandidate(in); ok {
		return allocation{id: id, lease: in.lease, reused: true}, nil
	}
	return e.create(ctx, in)
}

func (e *Engine) claimCandidate(in acquireInput) (int64, bool) {
	now := e.opts.Clock.Now()
	var best *fleet.Record
	for _, r := range e.index.Snapshot() {
		if r.State != fleet.StateIdle || r.Pool != in.pool || e.hasDesired(r.ID) {
			continue
		}
		if expires := r.Expires; expires-now.Unix() < int64(e.opts.Fleet.ClaimGuard.Seconds()) {
			continue
		}
		if best == nil || r.Expires < best.Expires || (r.Expires == best.Expires && r.ID < best.ID) {
			c := r
			best = &c
		}
	}
	if best == nil {
		return 0, false
	}
	e.setDesired(best.ID, fleet.Desired{
		Kind: fleet.DesiredClaim, Lease: in.lease, Name: in.req.Name, Image: in.req.Image, Req: in.hash, Tenant: in.req.Labels,
	})
	e.queue.Add(best.ID)
	return best.ID, true
}

func (e *Engine) create(ctx context.Context, in acquireInput) (allocation, error) {
	now := e.opts.Clock.Now()
	seed := fleet.Record{
		State: fleet.StateProvisioning, Gen: 1, Pool: in.pool, Req: in.hash, Image: in.req.Image,
		Lease: in.lease, LeasedAt: now.Unix(), Tenant: in.req.Labels,
	}
	srv, err := e.opts.Cloud.CreateServer(ctx, cloud.CreateSpec{
		Name: in.req.Name, ServerType: in.req.ServerType, Location: in.req.Location, Image: in.req.Image,
		SSHKeyIDs: in.spec.SSHKeyIDs, UserData: in.req.UserData, Labels: labels.Encode(seed),
	})
	if cloud.IsConflict(err) {
		return e.adopt(ctx, in)
	}
	if err != nil {
		return allocation{}, upstream(err)
	}
	rec, derr := recordFromServer(srv)
	if derr != nil {
		return allocation{}, api.NewError(api.CodeInternal, "created server has undecodable labels: %v", derr)
	}
	e.index.Put(rec)
	e.queue.Add(rec.ID)
	return allocation{id: rec.ID, lease: in.lease}, nil
}

func (e *Engine) adopt(ctx context.Context, in acquireInput) (allocation, error) {
	srv, err := e.opts.Cloud.GetServerByName(ctx, in.req.Name)
	if err != nil {
		return allocation{}, upstream(err)
	}
	rec, derr := recordFromServer(srv)
	if derr != nil || rec.Req != in.hash || rec.Lease == "" {
		return allocation{}, api.NewError(api.CodeConflict, "name %q is already used", in.req.Name)
	}
	e.index.Put(rec)
	e.queue.Add(rec.ID)
	return allocation{id: rec.ID, lease: rec.Lease, replay: true}, nil
}
