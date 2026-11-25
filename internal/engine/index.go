package engine

import (
	"context"
	"reflect"
	"sync"

	"github.com/theocourtois/ecoplus/internal/fleet"
)

type Index struct {
	mu      sync.RWMutex
	recs    map[int64]fleet.Record
	maxGen  map[int64]uint64
	wrote   map[int64]uint64
	seq     uint64
	changed chan struct{}
}

func NewIndex() *Index {
	return &Index{
		recs: map[int64]fleet.Record{}, maxGen: map[int64]uint64{}, wrote: map[int64]uint64{},
		changed: make(chan struct{}),
	}
}

func (x *Index) notify() {
	close(x.changed)
	x.changed = make(chan struct{})
}

func (x *Index) Put(r fleet.Record) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.putLocked(r)
}

type AbsorbOutcome int

const (
	AbsorbSkipped AbsorbOutcome = iota
	AbsorbUnchanged
	AbsorbStored
	AbsorbRegressed
)

func (x *Index) Absorb(rec fleet.Record, mark uint64, decoded bool) (fleet.Record, AbsorbOutcome) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.wrote[rec.ID] > mark {
		return rec, AbsorbSkipped
	}
	outcome := AbsorbStored
	if decoded && rec.Gen < x.maxGen[rec.ID] {
		rec.State = fleet.StateQuarantine
		outcome = AbsorbRegressed
	}
	if prev, ok := x.recs[rec.ID]; ok && reflect.DeepEqual(prev, rec) {
		return rec, AbsorbUnchanged
	}
	x.putLocked(rec)
	return rec, outcome
}

func (x *Index) DeleteIfStale(id int64, mark uint64) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.wrote[id] > mark {
		return false
	}
	if _, ok := x.recs[id]; !ok {
		return false
	}
	x.seq++
	delete(x.recs, id)
	delete(x.maxGen, id)
	x.wrote[id] = x.seq
	x.notify()
	return true
}

func (x *Index) putLocked(r fleet.Record) {
	x.seq++
	x.wrote[r.ID] = x.seq
	if r.Gen > x.maxGen[r.ID] {
		x.maxGen[r.ID] = r.Gen
	}
	x.recs[r.ID] = r.Clone()
	x.notify()
}

func (x *Index) Delete(id int64) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.seq++
	delete(x.recs, id)
	delete(x.maxGen, id)
	x.wrote[id] = x.seq
	x.notify()
}

func (x *Index) Get(id int64) (fleet.Record, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	r, ok := x.recs[id]
	return r.Clone(), ok
}

func (x *Index) ByName(name string) (fleet.Record, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	for _, r := range x.recs {
		if r.Name == name {
			return r.Clone(), true
		}
	}
	return fleet.Record{}, false
}

func (x *Index) Snapshot() []fleet.Record {
	x.mu.RLock()
	defer x.mu.RUnlock()
	out := make([]fleet.Record, 0, len(x.recs))
	for _, r := range x.recs {
		out = append(out, r.Clone())
	}
	return out
}

func (x *Index) Mark() uint64 {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.seq
}

func (x *Index) WroteSince(id int64, mark uint64) bool {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.wrote[id] > mark
}

func (x *Index) MaxGen(id int64) uint64 {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.maxGen[id]
}

func (x *Index) changes() <-chan struct{} {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.changed
}

func (x *Index) Await(ctx context.Context, id int64, done func(fleet.Record, bool) bool) (fleet.Record, bool, error) {
	for {
		ch := x.changes()
		rec, ok := x.Get(id)
		if done(rec, ok) {
			return rec, ok, nil
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return fleet.Record{}, false, ctx.Err()
		}
	}
}
