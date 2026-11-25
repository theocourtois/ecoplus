package fleet

import "time"

type EffectKind int

const (
	EffectNone EffectKind = iota
	EffectAwaitCreate
	EffectRebuild
	EffectDelete
)

type Effect struct {
	Kind  EffectKind
	Image string
}

type Plan struct {
	Intent  *Record
	Effect  Effect
	Commit  *Record
	Name    string
	Requeue time.Duration
}

func (p Plan) Noop() bool {
	return p.Intent == nil && p.Effect.Kind == EffectNone && p.Commit == nil
}
