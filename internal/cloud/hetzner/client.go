package hetzner

import (
	"net/http"
	"sync"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"k8s.io/utils/clock"
)

type Options struct {
	Token      string
	Endpoint   string
	Limit      LimitConfig
	Clock      clock.Clock
	HTTPClient *http.Client
	Version    string
	Backoff    time.Duration
}

type Client struct {
	api     *hcloud.Client
	mu      sync.Mutex
	pending map[int64][]*hcloud.Action
	clk     clock.Clock
	limit   *limitedTransport
}

func New(opts Options) *Client {
	base := opts.HTTPClient
	if base == nil {
		base = &http.Client{Timeout: 60 * time.Second}
	}
	backoff := opts.Backoff
	if backoff == 0 {
		backoff = time.Second
	}
	transport := base.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	limited := newLimitedTransport(transport, opts.Limit, opts.Clock)
	httpClient := &http.Client{Timeout: base.Timeout, Transport: limited}
	clientOpts := []hcloud.ClientOption{
		hcloud.WithToken(opts.Token),
		hcloud.WithHTTPClient(httpClient),
		hcloud.WithApplication("ecoplus-server", opts.Version),
		hcloud.WithPollOpts(hcloud.PollOpts{BackoffFunc: hcloud.ExponentialBackoffWithOpts(hcloud.ExponentialBackoffOpts{
			Base: time.Second, Multiplier: 2, Cap: 10 * time.Second,
		})}),
		hcloud.WithRetryOpts(hcloud.RetryOpts{MaxRetries: 5, BackoffFunc: hcloud.ExponentialBackoffWithOpts(hcloud.ExponentialBackoffOpts{
			Base: backoff, Multiplier: 2, Cap: 30 * backoff, Jitter: true,
		})}),
	}
	if opts.Endpoint != "" {
		clientOpts = append(clientOpts, hcloud.WithEndpoint(opts.Endpoint))
	}
	return &Client{api: hcloud.NewClient(clientOpts...), pending: map[int64][]*hcloud.Action{}, clk: opts.Clock, limit: limited}
}

func (c *Client) Remaining() int64 { return c.limit.remaining.Load() }
