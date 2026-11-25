package fleet

import (
	"strconv"
	"time"
)

func Step(r Record, d Desired, now time.Time, cfg Config) Plan {
	if r.State == StateQuarantine {
		return Plan{}
	}
	if d.Kind == DesiredAbort {
		return deletePlan(r)
	}
	switch r.State {
	case StateProvisioning:
		return Plan{Effect: Effect{Kind: EffectAwaitCreate}, Commit: advance(r, StateBusy, 1)}
	case StateBusy:
		return stepBusy(r, d, now, cfg)
	case StateClaiming:
		return Plan{Effect: Effect{Kind: EffectRebuild, Image: r.Image}, Commit: advance(r, StateBusy, 1)}
	case StateWiping:
		return stepWiping(r, now, cfg)
	case StateIdle:
		return stepIdle(r, d, now, cfg)
	case StateDeleting:
		return Plan{Effect: Effect{Kind: EffectDelete}}
	}
	return Plan{}
}

func advance(r Record, to State, gens uint64) *Record {
	next := r.Clone()
	next.State = to
	next.Gen += gens
	return &next
}

func fence(r *Record) {
	r.Lease = ""
	r.LeasedAt = 0
	r.Req = ""
	r.Expires = 0
	r.Activity = ActivityNone
	r.ActivityAt = 0
	r.Tenant = nil
}

func IdleName(id int64) string { return "ecoplus-idle-" + strconv.FormatInt(id, 10) }

func GoneName(id int64) string { return "ecoplus-gone-" + strconv.FormatInt(id, 10) }

func deletePlan(r Record) Plan {
	if r.State == StateDeleting {
		return Plan{Effect: Effect{Kind: EffectDelete}}
	}
	intent := advance(r, StateDeleting, 1)
	fence(intent)
	return Plan{Intent: intent, Effect: Effect{Kind: EffectDelete}, Name: GoneName(r.ID)}
}

func remaining(r Record, now time.Time, cfg Config) time.Duration {
	created := time.Unix(r.Created, 0)
	return Expiry(created, now, cfg.Margin).Sub(now)
}

func stepWiping(r Record, now time.Time, cfg Config) Plan {
	left := remaining(r, now, cfg)
	if left < cfg.WipeGuard {
		return deletePlan(r)
	}
	commit := advance(r, StateIdle, 1)
	commit.Expires = now.Add(left).Unix()
	return Plan{Effect: Effect{Kind: EffectRebuild, Image: r.Image}, Commit: commit}
}
