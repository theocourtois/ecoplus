package hetzner

import (
	"context"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/theocourtois/hetzner-ecoplus/internal/cloud"
	"golang.org/x/time/rate"
	"k8s.io/utils/clock"
)

const unknownRemaining = -1

type LimitConfig struct {
	Rate         float64
	Burst        int
	LowWatermark int
}

type limitedTransport struct {
	next      http.RoundTripper
	limiter   *rate.Limiter
	clk       clock.Clock
	watermark int64
	remaining atomic.Int64
}

func newLimitedTransport(next http.RoundTripper, cfg LimitConfig, clk clock.Clock) *limitedTransport {
	t := &limitedTransport{
		next: next, limiter: rate.NewLimiter(rate.Limit(cfg.Rate), cfg.Burst), clk: clk, watermark: int64(cfg.LowWatermark),
	}
	t.remaining.Store(unknownRemaining)
	return t
}

func (t *limitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	if cloud.PriorityOf(ctx) == cloud.Low {
		if err := t.waitForBudget(ctx); err != nil {
			return nil, err
		}
	}
	if err := t.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	resp, err := t.next.RoundTrip(req)
	if resp != nil {
		t.observe(resp)
	}
	return resp, err
}

func (t *limitedTransport) waitForBudget(ctx context.Context) error {
	for {
		remaining := t.remaining.Load()
		if remaining == unknownRemaining || remaining >= t.watermark {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.clk.After(time.Second):
			t.remaining.Add(1)
		}
	}
}

func (t *limitedTransport) observe(resp *http.Response) {
	if v, err := strconv.ParseInt(resp.Header.Get("RateLimit-Remaining"), 10, 64); err == nil {
		t.remaining.Store(v)
	}
}
