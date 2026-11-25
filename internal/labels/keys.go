package labels

const (
	Prefix     = "ecoplus/"
	KeyOwner   = Prefix + "owner"
	KeyState   = Prefix + "state"
	KeyGen     = Prefix + "gen"
	KeyPool    = Prefix + "pool"
	KeyReq     = Prefix + "req"
	KeyImage   = Prefix + "image"
	KeyLease   = Prefix + "lease"
	KeyLeased  = Prefix + "leased-at"
	KeyExpires = Prefix + "expires"
	KeyReuse   = Prefix + "reuse"
	KeyAct     = Prefix + "activity"
	KeyActAt   = Prefix + "activity-at"
	OwnerValue = "ecoplus"
	Selector   = KeyOwner + "=" + OwnerValue
	MaxValue   = 63
)

func ValidValue(v string) bool {
	if v == "" {
		return true
	}
	if len(v) > MaxValue || !alnum(v[0]) || !alnum(v[len(v)-1]) {
		return false
	}
	for i := 0; i < len(v); i++ {
		if c := v[i]; !alnum(c) && c != '.' && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func alnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
