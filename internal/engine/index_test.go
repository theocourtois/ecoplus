package engine

import (
	"testing"

	"github.com/theocourtois/hetzner-ecoplus/internal/fleet"
)

func TestAbsorbSkipsServersWrittenAfterTheListStarted(t *testing.T) {
	x := NewIndex()
	x.Put(fleet.Record{ID: 1, Gen: 5, State: fleet.StateBusy})
	mark := x.Mark()
	x.Put(fleet.Record{ID: 1, Gen: 7, State: fleet.StateBusy})
	stale := fleet.Record{ID: 1, Gen: 5, State: fleet.StateBusy}
	if _, outcome := x.Absorb(stale, mark, true); outcome != AbsorbSkipped {
		t.Fatalf("a write after the list started must win, got %v", outcome)
	}
	if got, _ := x.Get(1); got.Gen != 7 {
		t.Fatalf("index regressed to gen %d", got.Gen)
	}
}

func TestAbsorbQuarantinesRegressionOnlyWithoutConcurrentWrites(t *testing.T) {
	x := NewIndex()
	x.Put(fleet.Record{ID: 1, Gen: 9, State: fleet.StateIdle, Expires: 5})
	rec, outcome := x.Absorb(fleet.Record{ID: 1, Gen: 3, State: fleet.StateIdle, Expires: 5}, x.Mark(), true)
	if outcome != AbsorbRegressed || rec.State != fleet.StateQuarantine {
		t.Fatalf("expected quarantine, got %v %+v", outcome, rec)
	}
	if got, _ := x.Get(1); got.State != fleet.StateQuarantine {
		t.Fatal("quarantine must be stored")
	}
}

func TestAbsorbUnchangedAndNew(t *testing.T) {
	x := NewIndex()
	r := fleet.Record{ID: 2, Gen: 1, State: fleet.StateBusy, Lease: "l"}
	if _, outcome := x.Absorb(r, x.Mark(), true); outcome != AbsorbStored {
		t.Fatalf("new record must be stored, got %v", outcome)
	}
	if _, outcome := x.Absorb(r, x.Mark(), true); outcome != AbsorbUnchanged {
		t.Fatalf("identical record must be unchanged, got %v", outcome)
	}
}

func TestAbsorbUndecodableIsStoredWithoutGenCheck(t *testing.T) {
	x := NewIndex()
	x.Put(fleet.Record{ID: 3, Gen: 9, State: fleet.StateBusy})
	rec, outcome := x.Absorb(fleet.Record{ID: 3, State: fleet.StateQuarantine}, x.Mark(), false)
	if outcome != AbsorbStored || rec.State != fleet.StateQuarantine {
		t.Fatalf("got %v %+v", outcome, rec)
	}
}

func TestDeleteIfStale(t *testing.T) {
	x := NewIndex()
	x.Put(fleet.Record{ID: 1})
	mark := x.Mark()
	x.Put(fleet.Record{ID: 1, Gen: 2})
	if x.DeleteIfStale(1, mark) {
		t.Fatal("a record written after the mark must survive")
	}
	if !x.DeleteIfStale(1, x.Mark()) {
		t.Fatal("stale record must be deleted")
	}
	if x.DeleteIfStale(1, x.Mark()) {
		t.Fatal("deleting a missing record must report false")
	}
}
