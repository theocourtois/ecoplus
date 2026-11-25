package fleet

type State string

const (
	StateProvisioning State = "provisioning"
	StateBusy         State = "busy"
	StateClaiming     State = "claiming"
	StateWiping       State = "wiping"
	StateIdle         State = "idle"
	StateDeleting     State = "deleting"
	StateQuarantine   State = "quarantine"
)

func (s State) Known() bool {
	switch s {
	case StateProvisioning, StateBusy, StateClaiming, StateWiping, StateIdle, StateDeleting, StateQuarantine:
		return true
	}
	return false
}

type Activity string

const (
	ActivityNone Activity = ""
	ActivityBusy Activity = "busy"
	ActivityIdle Activity = "idle"
)

type Record struct {
	ID         int64
	Name       string
	Created    int64
	ServerType string
	Location   string
	IPv4       string
	IPv6       string
	State      State
	Gen        uint64
	Pool       string
	Req        string
	Image      string
	Lease      string
	LeasedAt   int64
	Expires    int64
	Reuse      uint64
	Activity   Activity
	ActivityAt int64
	Tenant     map[string]string
}

func (r Record) Clone() Record {
	if r.Tenant != nil {
		tenant := make(map[string]string, len(r.Tenant))
		for k, v := range r.Tenant {
			tenant[k] = v
		}
		r.Tenant = tenant
	}
	return r
}
