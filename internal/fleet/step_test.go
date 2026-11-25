package fleet

import (
	"testing"
	"time"
)

const lease = "00000000000000aa"

var epoch = time.Unix(2_000_000, 0)

func cfg() Config { return DefaultConfig() }

func rec(state State, mutate func(*Record)) Record {
	r := Record{ID: 1, Name: "n", Created: epoch.Unix(), State: state, Gen: 10, Pool: "p", Image: "img"}
	switch state {
	case StateBusy, StateClaiming, StateProvisioning:
		r.Lease = lease
	case StateIdle:
		r.Expires = epoch.Add(50 * time.Minute).Unix()
	}
	if mutate != nil {
		mutate(&r)
	}
	return r
}

func at(d time.Duration) time.Time { return epoch.Add(d) }

func TestStepNonBusyStates(t *testing.T) {
	cases := map[string]struct {
		r          Record
		d          Desired
		now        time.Duration
		intent     State
		effect     EffectKind
		commit     State
		wantNoop   bool
		wantGenEnd uint64
	}{
		"provisioning awaits":    {rec(StateProvisioning, nil), Desired{}, 0, "", EffectAwaitCreate, StateBusy, false, 11},
		"claiming resumes":       {rec(StateClaiming, nil), Desired{}, 0, "", EffectRebuild, StateBusy, false, 11},
		"wiping resumes":         {rec(StateWiping, nil), Desired{}, 10 * time.Minute, "", EffectRebuild, StateIdle, false, 11},
		"wiping too late":        {rec(StateWiping, nil), Desired{}, 46 * time.Minute, StateDeleting, EffectDelete, "", false, 0},
		"wiping guard edge":      {rec(StateWiping, nil), Desired{}, 45 * time.Minute, "", EffectRebuild, StateIdle, false, 11},
		"deleting redoes":        {rec(StateDeleting, nil), Desired{}, 0, "", EffectDelete, "", false, 0},
		"quarantine untouched":   {rec(StateQuarantine, nil), Desired{Kind: DesiredAbort}, 0, "", EffectNone, "", true, 0},
		"abort provisioning":     {rec(StateProvisioning, nil), Desired{Kind: DesiredAbort}, 0, StateDeleting, EffectDelete, "", false, 0},
		"abort claiming":         {rec(StateClaiming, nil), Desired{Kind: DesiredAbort}, 0, StateDeleting, EffectDelete, "", false, 0},
		"abort wiping":           {rec(StateWiping, nil), Desired{Kind: DesiredAbort}, 0, StateDeleting, EffectDelete, "", false, 0},
		"abort deleting":         {rec(StateDeleting, nil), Desired{Kind: DesiredAbort}, 0, "", EffectDelete, "", false, 0},
		"idle waits":             {rec(StateIdle, nil), Desired{}, 10 * time.Minute, "", EffectNone, "", true, 0},
		"idle at expiry":         {rec(StateIdle, nil), Desired{}, 50 * time.Minute, StateDeleting, EffectDelete, "", false, 0},
		"idle past expiry":       {rec(StateIdle, nil), Desired{}, 51 * time.Minute, StateDeleting, EffectDelete, "", false, 0},
		"idle just before":       {rec(StateIdle, nil), Desired{}, 50*time.Minute - time.Second, "", EffectNone, "", true, 0},
		"claim at guard edge":    {rec(StateIdle, nil), Desired{Kind: DesiredClaim, Lease: lease}, 48 * time.Minute, "", EffectNone, StateBusy, false, 11},
		"claim inside guard":     {rec(StateIdle, nil), Desired{Kind: DesiredClaim, Lease: lease}, 48*time.Minute + time.Second, StateDeleting, EffectDelete, "", false, 0},
		"claim after expiry":     {rec(StateIdle, nil), Desired{Kind: DesiredClaim, Lease: lease}, 50 * time.Minute, StateDeleting, EffectDelete, "", false, 0},
		"claim new image":        {rec(StateIdle, nil), Desired{Kind: DesiredClaim, Lease: lease, Image: "other"}, time.Minute, StateClaiming, EffectRebuild, StateBusy, false, 12},
		"release ignored idle":   {rec(StateIdle, nil), Desired{Kind: DesiredRelease, Lease: lease}, time.Minute, "", EffectNone, "", true, 0},
		"patch ignored idle":     {rec(StateIdle, nil), Desired{Kind: DesiredPatch, Lease: lease, Image: "x"}, time.Minute, "", EffectNone, "", true, 0},
		"claim ignored wiping":   {rec(StateWiping, nil), Desired{Kind: DesiredClaim, Lease: lease}, time.Minute, "", EffectRebuild, StateIdle, false, 11},
		"claim ignored deleting": {rec(StateDeleting, nil), Desired{Kind: DesiredClaim, Lease: lease}, time.Minute, "", EffectDelete, "", false, 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p := Step(tc.r, tc.d, at(tc.now), cfg())
			if p.Noop() != tc.wantNoop {
				t.Fatalf("noop=%v want %v: %+v", p.Noop(), tc.wantNoop, p)
			}
			if p.Effect.Kind != tc.effect {
				t.Fatalf("effect %v want %v", p.Effect.Kind, tc.effect)
			}
			if (p.Intent == nil) != (tc.intent == "") || (p.Intent != nil && p.Intent.State != tc.intent) {
				t.Fatalf("intent %+v want %q", p.Intent, tc.intent)
			}
			if (p.Commit == nil) != (tc.commit == "") || (p.Commit != nil && p.Commit.State != tc.commit) {
				t.Fatalf("commit %+v want %q", p.Commit, tc.commit)
			}
			if tc.wantGenEnd != 0 && p.Commit.Gen != tc.wantGenEnd {
				t.Fatalf("final gen %d want %d", p.Commit.Gen, tc.wantGenEnd)
			}
		})
	}
}

func TestIdleRequeuesAtExpiry(t *testing.T) {
	p := Step(rec(StateIdle, nil), Desired{}, at(20*time.Minute), cfg())
	if p.Requeue != 30*time.Minute {
		t.Fatalf("requeue %v", p.Requeue)
	}
}

func TestClaimFencesAndRecords(t *testing.T) {
	r := rec(StateIdle, func(r *Record) { r.Reuse = 2; r.Activity = ActivityIdle; r.Tenant = map[string]string{"x": "y"} })
	d := Desired{Kind: DesiredClaim, Lease: lease, Name: "new", Req: "req0000000000001", Tenant: map[string]string{"t": "1"}}
	p := Step(r, d, at(time.Minute), cfg())
	c := p.Commit
	if c.Lease != lease || c.LeasedAt != at(time.Minute).Unix() || c.Req != d.Req || c.Reuse != 3 ||
		c.Expires != 0 || c.Activity != ActivityNone || c.Tenant["t"] != "1" || len(c.Tenant) != 1 {
		t.Fatalf("unexpected commit %+v", c)
	}
	if p.Name != "new" || p.Intent != nil {
		t.Fatalf("same image claim writes once with the name: %+v", p)
	}
}

func TestClaimNewImageIntent(t *testing.T) {
	d := Desired{Kind: DesiredClaim, Lease: lease, Image: "other", Name: "new"}
	p := Step(rec(StateIdle, nil), d, at(time.Minute), cfg())
	if p.Intent.Image != "other" || p.Intent.Lease != lease || p.Intent.Expires != 0 || p.Effect.Image != "other" {
		t.Fatalf("intent must carry target image and lease: %+v", p)
	}
	if p.Commit.Gen != p.Intent.Gen+1 || p.Intent.Gen != 11 {
		t.Fatalf("gens %d %d", p.Intent.Gen, p.Commit.Gen)
	}
}
