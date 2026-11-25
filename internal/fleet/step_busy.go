package fleet

import "time"

func stepBusy(r Record, d Desired, now time.Time, cfg Config) Plan {
	if d.Lease != r.Lease {
		return Plan{}
	}
	switch d.Kind {
	case DesiredRelease:
		return release(r, now, cfg)
	case DesiredPatch:
		return patch(r, d)
	}
	return Plan{}
}

func release(r Record, now time.Time, cfg Config) Plan {
	left := remaining(r, now, cfg)
	if left < cfg.WipeGuard {
		return deletePlan(r)
	}
	intent := advance(r, StateWiping, 1)
	fence(intent)
	commit := advance(*intent, StateIdle, 1)
	commit.Expires = now.Add(left).Unix()
	return Plan{Intent: intent, Effect: Effect{Kind: EffectRebuild, Image: r.Image}, Commit: commit, Name: IdleName(r.ID)}
}

func sameTenant(a, b map[string]string) bool {
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

func patch(r Record, d Desired) Plan {
	next := r.Clone()
	if d.Req != "" {
		next.Req = d.Req
	}
	if d.Tenant != nil {
		next.Tenant = cloneMap(d.Tenant)
	}
	name := ""
	if d.Name != "" && d.Name != r.Name {
		name = d.Name
	}
	imageChange := d.Image != "" && d.Image != r.Image
	unchanged := next.Req == r.Req && sameTenant(next.Tenant, r.Tenant)
	if !imageChange && name == "" && unchanged {
		return Plan{}
	}
	if !imageChange {
		next.Gen++
		return Plan{Commit: &next, Name: name}
	}
	next.Image = d.Image
	next.State = StateClaiming
	next.Gen++
	commit := advance(next, StateBusy, 1)
	return Plan{Intent: &next, Effect: Effect{Kind: EffectRebuild, Image: d.Image}, Commit: commit, Name: name}
}

func cloneMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
