package labels

import "github.com/theocourtois/hetzner-ecoplus/internal/fleet"

func checkConsistency(r fleet.Record) error {
	switch r.State {
	case fleet.StateProvisioning, fleet.StateClaiming, fleet.StateBusy:
		if r.Lease == "" {
			return bad(KeyLease, "state %s requires a lease", r.State)
		}
	case fleet.StateIdle, fleet.StateWiping, fleet.StateDeleting:
		if r.Lease != "" {
			return bad(KeyLease, "state %s forbids a lease", r.State)
		}
	}
	if r.State == fleet.StateIdle && r.Expires == 0 {
		return bad(KeyExpires, "idle requires expires")
	}
	if r.State != fleet.StateIdle && r.State != fleet.StateQuarantine && r.Expires != 0 {
		return bad(KeyExpires, "only idle may carry expires")
	}
	return nil
}
