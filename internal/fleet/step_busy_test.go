package fleet

import (
	"testing"
	"time"
)

func TestStepBusy(t *testing.T) {
	str := func(s string) string { return s }
	cases := map[string]struct {
		d      Desired
		now    time.Duration
		noop   bool
		intent State
		effect EffectKind
		commit State
		name   string
	}{
		"no desired":           {Desired{}, 0, true, "", EffectNone, "", ""},
		"replayed claim":       {Desired{Kind: DesiredClaim, Lease: lease}, 0, true, "", EffectNone, "", ""},
		"stale lease release":  {Desired{Kind: DesiredRelease, Lease: "ffffffffffffffff"}, 0, true, "", EffectNone, "", ""},
		"stale lease patch":    {Desired{Kind: DesiredPatch, Lease: "ffffffffffffffff", Image: "x"}, 0, true, "", EffectNone, "", ""},
		"release with time":    {Desired{Kind: DesiredRelease, Lease: lease}, 10 * time.Minute, false, StateWiping, EffectRebuild, StateIdle, "ecoplus-idle-1"},
		"release at wipe edge": {Desired{Kind: DesiredRelease, Lease: lease}, 45 * time.Minute, false, StateWiping, EffectRebuild, StateIdle, "ecoplus-idle-1"},
		"release past edge":    {Desired{Kind: DesiredRelease, Lease: lease}, 45*time.Minute + time.Second, false, StateDeleting, EffectDelete, "", "ecoplus-gone-1"},
		"release on boundary":  {Desired{Kind: DesiredRelease, Lease: lease}, time.Hour, false, StateDeleting, EffectDelete, "", "ecoplus-gone-1"},
		"release in hour two":  {Desired{Kind: DesiredRelease, Lease: lease}, time.Hour + time.Minute, false, StateWiping, EffectRebuild, StateIdle, "ecoplus-idle-1"},
		"patch same":           {Desired{Kind: DesiredPatch, Lease: lease, Image: "img", Name: "n"}, 0, true, "", EffectNone, "", ""},
		"patch rename":         {Desired{Kind: DesiredPatch, Lease: lease, Name: "other"}, 0, false, "", EffectNone, StateBusy, "other"},
		"patch image":          {Desired{Kind: DesiredPatch, Lease: lease, Image: "other"}, 0, false, StateClaiming, EffectRebuild, StateBusy, ""},
		"patch image and name": {Desired{Kind: DesiredPatch, Lease: lease, Image: "other", Name: str("o")}, 0, false, StateClaiming, EffectRebuild, StateBusy, "o"},
		"patch tenant":         {Desired{Kind: DesiredPatch, Lease: lease, Tenant: map[string]string{"a": "b"}}, 0, false, "", EffectNone, StateBusy, ""},
		"patch tenant cleared": {Desired{Kind: DesiredPatch, Lease: lease, Tenant: map[string]string{}}, 0, true, "", EffectNone, "", ""},
		"abort":                {Desired{Kind: DesiredAbort}, 0, false, StateDeleting, EffectDelete, "", "ecoplus-gone-1"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p := Step(rec(StateBusy, nil), tc.d, at(tc.now), cfg())
			if p.Noop() != tc.noop || p.Effect.Kind != tc.effect || p.Name != tc.name {
				t.Fatalf("unexpected plan %+v", p)
			}
			if (p.Intent == nil) != (tc.intent == "") || (p.Intent != nil && p.Intent.State != tc.intent) {
				t.Fatalf("intent %+v want %q", p.Intent, tc.intent)
			}
			if (p.Commit == nil) != (tc.commit == "") || (p.Commit != nil && p.Commit.State != tc.commit) {
				t.Fatalf("commit %+v want %q", p.Commit, tc.commit)
			}
		})
	}
}

func TestReleaseFencesImmediately(t *testing.T) {
	r := rec(StateBusy, func(r *Record) {
		r.Req = "r"
		r.LeasedAt = 5
		r.Activity = ActivityBusy
		r.ActivityAt = 6
		r.Tenant = map[string]string{"a": "b"}
	})
	p := Step(r, Desired{Kind: DesiredRelease, Lease: lease}, at(time.Minute), cfg())
	i := p.Intent
	if i.Lease != "" || i.LeasedAt != 0 || i.Req != "" || i.Activity != ActivityNone || i.ActivityAt != 0 || i.Tenant != nil {
		t.Fatalf("intent must fence the lease and clear tenant data: %+v", i)
	}
	if p.Commit.Expires != at(50*time.Minute).Unix() || p.Commit.State != StateIdle || p.Commit.Lease != "" {
		t.Fatalf("bad commit %+v", p.Commit)
	}
	if r.Tenant["a"] != "b" || r.Lease != lease {
		t.Fatal("input record mutated")
	}
}

func TestImageChangeKeepsLease(t *testing.T) {
	p := Step(rec(StateBusy, nil), Desired{Kind: DesiredPatch, Lease: lease, Image: "new", Req: "rq"}, at(0), cfg())
	if p.Intent.Lease != lease || p.Intent.Image != "new" || p.Commit.Req != "rq" || p.Commit.Lease != lease {
		t.Fatalf("unexpected %+v", p)
	}
}

func TestStepNeverMutatesInput(t *testing.T) {
	r := rec(StateBusy, func(r *Record) { r.Tenant = map[string]string{"a": "b"} })
	before := r.Clone()
	Step(r, Desired{Kind: DesiredPatch, Lease: lease, Tenant: map[string]string{"c": "d"}}, at(0), cfg())
	Step(r, Desired{Kind: DesiredRelease, Lease: lease}, at(0), cfg())
	if r.Gen != before.Gen || len(r.Tenant) != 1 || r.Tenant["a"] != "b" {
		t.Fatal("input mutated")
	}
}

func TestStateKnown(t *testing.T) {
	if State("x").Known() || !StateIdle.Known() || !StateQuarantine.Known() {
		t.Fatal("Known misbehaves")
	}
}

func TestGenStrictlyIncreasesAcrossPlans(t *testing.T) {
	for _, r := range []Record{rec(StateBusy, nil), rec(StateIdle, nil), rec(StateWiping, nil), rec(StateClaiming, nil)} {
		for _, d := range []Desired{{}, {Kind: DesiredRelease, Lease: lease}, {Kind: DesiredClaim, Lease: lease, Image: "z"},
			{Kind: DesiredPatch, Lease: lease, Image: "z"}, {Kind: DesiredAbort}} {
			p := Step(r, d, at(time.Minute), cfg())
			prev := r.Gen
			for _, w := range []*Record{p.Intent, p.Commit} {
				if w == nil {
					continue
				}
				if w.Gen != prev+1 {
					t.Fatalf("gen %d after %d for %v %+v", w.Gen, prev, r.State, d)
				}
				prev = w.Gen
			}
		}
	}
}
