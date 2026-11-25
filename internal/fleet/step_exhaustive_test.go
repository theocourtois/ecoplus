package fleet

import (
	"testing"
	"time"
)

func allStates() []State {
	return []State{StateProvisioning, StateBusy, StateClaiming, StateWiping, StateIdle, StateDeleting, StateQuarantine}
}

func allDesired() []Desired {
	other := "ffffffffffffffff"
	return []Desired{
		{}, {Kind: DesiredAbort},
		{Kind: DesiredClaim, Lease: lease}, {Kind: DesiredClaim, Lease: lease, Image: "other"},
		{Kind: DesiredRelease, Lease: lease}, {Kind: DesiredRelease, Lease: other},
		{Kind: DesiredPatch, Lease: lease}, {Kind: DesiredPatch, Lease: lease, Image: "other", Name: "x"},
		{Kind: DesiredPatch, Lease: other, Image: "other"},
		{Kind: DesiredPatch, Lease: lease, Tenant: map[string]string{"a": "b"}},
	}
}

func windows() []time.Duration {
	var out []time.Duration
	for _, minutes := range []int{0, 1, 44, 45, 46, 47, 48, 49, 50, 51, 59, 60, 61, 105, 106, 110, 111, 120} {
		out = append(out, time.Duration(minutes)*time.Minute, time.Duration(minutes)*time.Minute+time.Second,
			time.Duration(minutes)*time.Minute-time.Second)
	}
	return out
}

func TestStepStructuralProperties(t *testing.T) {
	for _, state := range allStates() {
		for _, d := range allDesired() {
			for _, w := range windows() {
				if w < 0 {
					continue
				}
				r := rec(state, nil)
				p := Step(r, d, at(w), cfg())
				checkPlan(t, r, d, w, p)
			}
		}
	}
}

func checkPlan(t *testing.T, r Record, d Desired, w time.Duration, p Plan) {
	t.Helper()
	ctx := func() string { return string(r.State) + " " + time.Duration(w).String() }
	prev := r.Gen
	for _, rec := range []*Record{p.Intent, p.Commit} {
		if rec == nil {
			continue
		}
		if rec.Gen != prev+1 {
			t.Fatalf("%s %+v: gen %d after %d", ctx(), d, rec.Gen, prev)
		}
		prev = rec.Gen
		if !rec.State.Known() || rec.State == StateQuarantine {
			t.Fatalf("%s: plan writes forbidden state %q", ctx(), rec.State)
		}
	}
	if r.State == StateQuarantine && !p.Noop() {
		t.Fatalf("quarantine must never be touched: %+v", p)
	}
	if p.Commit != nil && p.Commit.State == StateIdle {
		passedWiping := (p.Intent != nil && p.Intent.State == StateWiping) || r.State == StateWiping
		if !passedWiping || p.Effect.Kind != EffectRebuild {
			t.Fatalf("%s %+v: idle committed without a wipe: %+v", ctx(), d, p)
		}
		if p.Commit.Lease != "" || p.Commit.Tenant != nil || p.Commit.Expires == 0 {
			t.Fatalf("%s: idle commit carries tenant data or no expiry: %+v", ctx(), p.Commit)
		}
	}
	if p.Intent != nil && p.Intent.State == StateDeleting {
		if p.Effect.Kind != EffectDelete || p.Commit != nil || p.Intent.Lease != "" || p.Intent.Tenant != nil {
			t.Fatalf("%s: bad delete plan %+v", ctx(), p)
		}
	}
	if p.Effect.Kind == EffectRebuild && p.Effect.Image == "" {
		t.Fatalf("%s: rebuild without an image", ctx())
	}
	if p.Commit != nil && p.Commit.State == StateBusy && p.Commit.Lease == "" {
		t.Fatalf("%s %+v: busy commit without lease: %+v", ctx(), d, p.Commit)
	}
	if p.Commit != nil && p.Commit.State != StateIdle && p.Commit.Expires != 0 {
		t.Fatalf("%s: only idle may carry expires", ctx())
	}
}
