package metrics

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/theocourtois/hetzner-ecoplus/internal/engine"
	"github.com/theocourtois/hetzner-ecoplus/internal/fleet"
)

type src struct{ st engine.Stats }

func (s src) Stats() engine.Stats { return s.st }

func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), "GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Body)
	return string(body)
}

func TestMetricsExposeFleetAndCounters(t *testing.T) {
	m := New(src{engine.Stats{
		States: map[fleet.State]int{fleet.StateIdle: 3, fleet.StateBusy: 2}, QueueDepth: 4, Quarantined: 1, LongLeases: 5, Halted: true,
	}}, func() int64 { return 1234 })
	m.Acquired(true)
	m.Acquired(true)
	m.Acquired(false)
	m.Transition(fleet.StateBusy, fleet.StateWiping)
	out := scrape(t, m)
	for _, want := range []string{
		`ecoplus_servers{state="idle"} 3`, `ecoplus_servers{state="busy"} 2`, `ecoplus_servers{state="quarantine"} 0`,
		`ecoplus_queue_depth 4`, `ecoplus_quarantined_servers 1`, `ecoplus_long_leases 5`, `ecoplus_halted 1`,
		`ecoplus_api_budget_remaining 1234`, `ecoplus_acquires_total{result="reused"} 2`, `ecoplus_acquires_total{result="created"} 1`,
		`ecoplus_saved_hours_total 2`, `ecoplus_transitions_total{from="busy",to="wiping"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output", want)
		}
	}
}

func TestMetricsWithoutBudgetSource(t *testing.T) {
	out := scrape(t, New(src{}, nil))
	if !strings.Contains(out, "ecoplus_api_budget_remaining -1") || !strings.Contains(out, "ecoplus_halted 0") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}
