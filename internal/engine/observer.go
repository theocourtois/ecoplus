package engine

import "github.com/theocourtois/hetzner-ecoplus/internal/fleet"

type Observer interface {
	Acquired(reused bool)
	Transition(from, to fleet.State)
}

type noopObserver struct{}

func (noopObserver) Acquired(bool)               {}
func (noopObserver) Transition(_, _ fleet.State) {}
