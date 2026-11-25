package labels

import (
	"strconv"
	"strings"

	"github.com/theocourtois/ecoplus/internal/fleet"
)

func Decode(in map[string]string) (fleet.Record, error) {
	var r fleet.Record
	if in[KeyOwner] != OwnerValue {
		return r, bad(KeyOwner, "not owned by ecoplus")
	}
	for key, value := range in {
		if strings.HasPrefix(key, Prefix) && !knownKey(key) {
			return r, bad(key, "unknown key")
		}
		if !ValidValue(value) {
			return r, bad(key, "invalid value")
		}
	}
	if err := decodeFields(in, &r); err != nil {
		return fleet.Record{}, err
	}
	for key, value := range in {
		if !strings.HasPrefix(key, Prefix) {
			if r.Tenant == nil {
				r.Tenant = make(map[string]string)
			}
			r.Tenant[key] = value
		}
	}
	if err := checkConsistency(r); err != nil {
		return fleet.Record{}, err
	}
	return r, nil
}

func knownKey(key string) bool {
	switch key {
	case KeyOwner, KeyState, KeyGen, KeyPool, KeyReq, KeyImage, KeyLease, KeyLeased, KeyExpires, KeyReuse, KeyAct, KeyActAt:
		return true
	}
	return false
}

func decodeFields(in map[string]string, r *fleet.Record) error {
	var err error
	r.State = fleet.State(in[KeyState])
	if !r.State.Known() {
		return bad(KeyState, "unknown state %q", in[KeyState])
	}
	if r.Gen, err = parseUint(in, KeyGen, true); err != nil {
		return err
	}
	if r.Reuse, err = parseUint(in, KeyReuse, false); err != nil {
		return err
	}
	for _, f := range []struct {
		key string
		dst *int64
	}{{KeyLeased, &r.LeasedAt}, {KeyExpires, &r.Expires}, {KeyActAt, &r.ActivityAt}} {
		if *f.dst, err = parseTime(in, f.key); err != nil {
			return err
		}
	}
	for _, f := range []struct {
		key string
		dst *string
	}{{KeyPool, &r.Pool}, {KeyReq, &r.Req}, {KeyLease, &r.Lease}} {
		if *f.dst, err = parseHex(in, f.key, f.key == KeyPool); err != nil {
			return err
		}
	}
	r.Image = in[KeyImage]
	r.Activity = fleet.Activity(in[KeyAct])
	if r.Activity != fleet.ActivityNone && r.Activity != fleet.ActivityBusy && r.Activity != fleet.ActivityIdle {
		return bad(KeyAct, "unknown activity %q", in[KeyAct])
	}
	return nil
}

func parseUint(in map[string]string, key string, required bool) (uint64, error) {
	raw, ok := in[key]
	if !ok {
		if required {
			return 0, bad(key, "missing")
		}
		return 0, nil
	}
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || strconv.FormatUint(v, 10) != raw {
		return 0, bad(key, "not a canonical unsigned integer")
	}
	return v, nil
}

func parseTime(in map[string]string, key string) (int64, error) {
	v, err := parseUint(in, key, false)
	if err != nil {
		return 0, err
	}
	if v == 0 && in[key] != "" || v > 1<<62 {
		return 0, bad(key, "out of range")
	}
	return int64(v), nil
}

func parseHex(in map[string]string, key string, required bool) (string, error) {
	raw, ok := in[key]
	if !ok {
		if required {
			return "", bad(key, "missing")
		}
		return "", nil
	}
	if len(raw) != 16 {
		return "", bad(key, "must be 16 hex characters")
	}
	for i := 0; i < len(raw); i++ {
		if c := raw[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", bad(key, "must be lowercase hex")
		}
	}
	return raw, nil
}
