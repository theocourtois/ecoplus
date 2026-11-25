package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/theocourtois/ecoplus/internal/engine"
	"github.com/theocourtois/ecoplus/internal/fleet"
)

type Source interface {
	Stats() engine.Stats
}

type Metrics struct {
	registry    *prometheus.Registry
	acquires    *prometheus.CounterVec
	transitions *prometheus.CounterVec
	savedHours  prometheus.Counter
}

func New(src Source, budget func() int64) *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		acquires: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ecoplus_acquires_total", Help: "Acquire calls by outcome, reused from the pool or freshly created.",
		}, []string{"result"}),
		transitions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ecoplus_transitions_total", Help: "Server state transitions.",
		}, []string{"from", "to"}),
		savedHours: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ecoplus_saved_hours_total", Help: "Billed hours avoided by reusing a pooled server.",
		}),
	}
	m.registry.MustRegister(m.acquires, m.transitions, m.savedHours, newCollector(src, budget))
	return m
}

func (m *Metrics) Acquired(reused bool) {
	if reused {
		m.acquires.WithLabelValues("reused").Inc()
		m.savedHours.Inc()
		return
	}
	m.acquires.WithLabelValues("created").Inc()
}

func (m *Metrics) Transition(from, to fleet.State) {
	m.transitions.WithLabelValues(string(from), string(to)).Inc()
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
