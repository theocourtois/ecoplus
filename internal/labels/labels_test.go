package labels

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/theocourtois/hetzner-ecoplus/internal/fleet"
	"pgregory.net/rapid"
)

func hexString(t *rapid.T, name string) string {
	return rapid.StringMatching(`[0-9a-f]{16}`).Draw(t, name)
}

func recordGen() *rapid.Generator[fleet.Record] {
	return rapid.Custom(func(t *rapid.T) fleet.Record {
		states := []fleet.State{fleet.StateProvisioning, fleet.StateBusy, fleet.StateClaiming,
			fleet.StateWiping, fleet.StateIdle, fleet.StateDeleting}
		r := fleet.Record{
			State: rapid.SampledFrom(states).Draw(t, "state"),
			Gen:   rapid.Uint64().Draw(t, "gen"),
			Pool:  hexString(t, "pool"),
			Reuse: rapid.Uint64().Draw(t, "reuse"),
			Image: rapid.StringMatching(`([a-z0-9]([a-z0-9._-]{0,20}[a-z0-9])?)?`).Draw(t, "image"),
		}
		switch r.State {
		case fleet.StateProvisioning, fleet.StateClaiming, fleet.StateBusy:
			r.Lease = hexString(t, "lease")
			r.LeasedAt = rapid.Int64Range(1, 1<<40).Draw(t, "leasedAt")
			r.Req = hexString(t, "req")
		case fleet.StateIdle:
			r.Expires = rapid.Int64Range(1, 1<<40).Draw(t, "expires")
		}
		r.Activity = rapid.SampledFrom([]fleet.Activity{"", fleet.ActivityBusy, fleet.ActivityIdle}).Draw(t, "act")
		if r.Activity != "" {
			r.ActivityAt = rapid.Int64Range(1, 1<<40).Draw(t, "actAt")
		}
		r.Tenant = rapid.MapOfN(rapid.StringMatching(`[a-z][a-z0-9-]{0,10}`), rapid.StringMatching(`[A-Za-z0-9]{0,10}`), 0, 4).Draw(t, "tenant")
		if len(r.Tenant) == 0 {
			r.Tenant = nil
		}
		return r
	})
}

func TestRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		r := recordGen().Draw(t, "record")
		encoded := Encode(r)
		for k, v := range encoded {
			if !ValidValue(v) {
				t.Fatalf("value of %s invalid: %q", k, v)
			}
		}
		got, err := Decode(encoded)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !reflect.DeepEqual(got, r) {
			t.Fatalf("got %+v want %+v", got, r)
		}
	})
}

func valid() map[string]string {
	return Encode(fleet.Record{State: fleet.StateBusy, Gen: 3, Pool: strings.Repeat("a", 16), Lease: strings.Repeat("b", 16)})
}

func TestDecodeRejects(t *testing.T) {
	mutations := map[string]func(map[string]string){
		"no owner":        func(m map[string]string) { delete(m, KeyOwner) },
		"foreign owner":   func(m map[string]string) { m[KeyOwner] = "other" },
		"unknown state":   func(m map[string]string) { m[KeyState] = "zombie" },
		"missing state":   func(m map[string]string) { delete(m, KeyState) },
		"missing gen":     func(m map[string]string) { delete(m, KeyGen) },
		"negative gen":    func(m map[string]string) { m[KeyGen] = "-1" },
		"padded gen":      func(m map[string]string) { m[KeyGen] = "03" },
		"missing pool":    func(m map[string]string) { delete(m, KeyPool) },
		"short pool":      func(m map[string]string) { m[KeyPool] = "abc" },
		"upper pool":      func(m map[string]string) { m[KeyPool] = strings.Repeat("A", 16) },
		"busy no lease":   func(m map[string]string) { delete(m, KeyLease) },
		"unknown key":     func(m map[string]string) { m[Prefix+"mystery"] = "x" },
		"bad activity":    func(m map[string]string) { m[KeyAct] = "sleeping" },
		"bad time":        func(m map[string]string) { m[KeyLeased] = "yesterday" },
		"zero time":       func(m map[string]string) { m[KeyLeased] = "0" },
		"huge time":       func(m map[string]string) { m[KeyExpires] = "9223372036854775807" },
		"busy expires":    func(m map[string]string) { m[KeyExpires] = "100" },
		"bad reuse":       func(m map[string]string) { m[KeyReuse] = "x" },
		"bad value":       func(m map[string]string) { m["tenant"] = "-bad-" },
		"long value":      func(m map[string]string) { m["tenant"] = strings.Repeat("a", 64) },
		"idle with lease": func(m map[string]string) { m[KeyState] = "idle"; m[KeyExpires] = "5" },
		"idle no expires": func(m map[string]string) { m[KeyState] = "idle"; delete(m, KeyLease) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			m := valid()
			mutate(m)
			if _, err := Decode(m); err == nil {
				t.Fatal("expected decode error")
			}
		})
	}
}

func TestDecodeError(t *testing.T) {
	_, err := Decode(map[string]string{})
	var de *DecodeError
	if !errors.As(err, &de) || de.Key != KeyOwner || !strings.Contains(de.Error(), KeyOwner) {
		t.Fatalf("unexpected %v", err)
	}
}

func TestTenantLabelsPassThrough(t *testing.T) {
	m := valid()
	m["team"] = "infra"
	m["env"] = "ci"
	r, err := Decode(m)
	if err != nil || len(r.Tenant) != 2 || r.Tenant["team"] != "infra" {
		t.Fatalf("got %+v %v", r, err)
	}
	if enc := Encode(r); enc["team"] != "infra" || enc["env"] != "ci" {
		t.Fatalf("tenant labels lost: %v", enc)
	}
}

func TestValidValue(t *testing.T) {
	good := []string{"", "a", "a-b", "A.b_c", strings.Repeat("a", 63)}
	bad := []string{"-a", "a-", ".a", "a b", "a/b", strings.Repeat("a", 64), "é"}
	for _, v := range good {
		if !ValidValue(v) {
			t.Errorf("%q should be valid", v)
		}
	}
	for _, v := range bad {
		if ValidValue(v) {
			t.Errorf("%q should be invalid", v)
		}
	}
}

func FuzzDecode(f *testing.F) {
	f.Add("busy", "1", strings.Repeat("a", 16), strings.Repeat("b", 16), "x", "y")
	f.Add("idle", "99", "zz", "", "k", "")
	f.Fuzz(func(t *testing.T, state, gen, pool, lease, key, value string) {
		in := map[string]string{KeyOwner: OwnerValue, KeyState: state, KeyGen: gen, KeyPool: pool, key: value}
		if lease != "" {
			in[KeyLease] = lease
		}
		r, err := Decode(in)
		if err != nil {
			return
		}
		again, err := Decode(Encode(r))
		if err != nil || !reflect.DeepEqual(again, r) {
			t.Fatalf("round trip broke: %+v vs %+v (%v)", r, again, err)
		}
		for k, v := range Encode(r) {
			if !ValidValue(v) {
				t.Fatalf("encoded %s=%q invalid", k, v)
			}
		}
	})
}
