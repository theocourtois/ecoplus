package fleet

import "time"

func stepIdle(r Record, d Desired, now time.Time, cfg Config) Plan {
	expires := time.Unix(r.Expires, 0)
	if !now.Before(expires) {
		return deletePlan(r)
	}
	if d.Kind != DesiredClaim {
		return Plan{Requeue: expires.Sub(now)}
	}
	if expires.Sub(now) < cfg.ClaimGuard {
		return deletePlan(r)
	}
	return claim(r, d, now)
}

func claim(r Record, d Desired, now time.Time) Plan {
	base := r.Clone()
	fence(&base)
	base.Lease = d.Lease
	base.LeasedAt = now.Unix()
	base.Req = d.Req
	base.Reuse++
	base.Tenant = cloneMap(d.Tenant)
	if d.Image == "" || d.Image == r.Image {
		base.State = StateBusy
		base.Gen++
		return Plan{Commit: &base, Name: d.Name}
	}
	base.State = StateClaiming
	base.Image = d.Image
	base.Gen++
	commit := advance(base, StateBusy, 1)
	return Plan{Intent: &base, Effect: Effect{Kind: EffectRebuild, Image: d.Image}, Commit: commit, Name: d.Name}
}
