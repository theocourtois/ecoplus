package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/theocourtois/hetzner-ecoplus/internal/fleet"
)

type collector struct {
	src         Source
	budget      func() int64
	servers     *prometheus.Desc
	queueDepth  *prometheus.Desc
	quarantined *prometheus.Desc
	longLeases  *prometheus.Desc
	halted      *prometheus.Desc
	apiBudget   *prometheus.Desc
}

func newCollector(src Source, budget func() int64) *collector {
	return &collector{
		src: src, budget: budget,
		servers:     prometheus.NewDesc("ecoplus_servers", "Servers by state.", []string{"state"}, nil),
		queueDepth:  prometheus.NewDesc("ecoplus_queue_depth", "Keys waiting in the reconcile queue.", nil, nil),
		quarantined: prometheus.NewDesc("ecoplus_quarantined_servers", "Servers in quarantine, which need an operator.", nil, nil),
		longLeases:  prometheus.NewDesc("ecoplus_long_leases", "Busy servers whose lease exceeds the alert age.", nil, nil),
		halted:      prometheus.NewDesc("ecoplus_halted", "1 when a single-writer violation stopped all writes.", nil, nil),
		apiBudget:   prometheus.NewDesc("ecoplus_api_budget_remaining", "Hetzner API requests remaining, or -1 when unknown.", nil, nil),
	}
}

func (c *collector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{c.servers, c.queueDepth, c.quarantined, c.longLeases, c.halted, c.apiBudget} {
		ch <- d
	}
}

func (c *collector) Collect(ch chan<- prometheus.Metric) {
	st := c.src.Stats()
	for _, state := range []fleet.State{
		fleet.StateProvisioning, fleet.StateBusy, fleet.StateClaiming, fleet.StateWiping,
		fleet.StateIdle, fleet.StateDeleting, fleet.StateQuarantine,
	} {
		ch <- prometheus.MustNewConstMetric(c.servers, prometheus.GaugeValue, float64(st.States[state]), string(state))
	}
	ch <- prometheus.MustNewConstMetric(c.queueDepth, prometheus.GaugeValue, float64(st.QueueDepth))
	ch <- prometheus.MustNewConstMetric(c.quarantined, prometheus.GaugeValue, float64(st.Quarantined))
	ch <- prometheus.MustNewConstMetric(c.longLeases, prometheus.GaugeValue, float64(st.LongLeases))
	halted := 0.0
	if st.Halted {
		halted = 1
	}
	ch <- prometheus.MustNewConstMetric(c.halted, prometheus.GaugeValue, halted)
	remaining := int64(-1)
	if c.budget != nil {
		remaining = c.budget()
	}
	ch <- prometheus.MustNewConstMetric(c.apiBudget, prometheus.GaugeValue, float64(remaining))
}
