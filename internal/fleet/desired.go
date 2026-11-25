package fleet

type DesiredKind int

const (
	DesiredNone DesiredKind = iota
	DesiredClaim
	DesiredRelease
	DesiredPatch
	DesiredAbort
)

type Desired struct {
	Kind   DesiredKind
	Lease  string
	Name   string
	Image  string
	Req    string
	Tenant map[string]string
}
