package labels

import (
	"strconv"

	"github.com/theocourtois/hetzner-ecoplus/internal/fleet"
)

func Encode(r fleet.Record) map[string]string {
	out := make(map[string]string, len(r.Tenant)+12)
	for k, v := range r.Tenant {
		out[k] = v
	}
	out[KeyOwner] = OwnerValue
	out[KeyState] = string(r.State)
	out[KeyGen] = strconv.FormatUint(r.Gen, 10)
	out[KeyPool] = r.Pool
	setString(out, KeyReq, r.Req)
	setString(out, KeyImage, r.Image)
	setString(out, KeyLease, r.Lease)
	setInt(out, KeyLeased, r.LeasedAt)
	setInt(out, KeyExpires, r.Expires)
	if r.Reuse > 0 {
		out[KeyReuse] = strconv.FormatUint(r.Reuse, 10)
	}
	setString(out, KeyAct, string(r.Activity))
	setInt(out, KeyActAt, r.ActivityAt)
	return out
}

func setString(out map[string]string, key, value string) {
	if value != "" {
		out[key] = value
	}
}

func setInt(out map[string]string, key string, value int64) {
	if value != 0 {
		out[key] = strconv.FormatInt(value, 10)
	}
}
