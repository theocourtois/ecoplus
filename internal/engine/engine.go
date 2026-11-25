package engine

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/theocourtois/hetzner-ecoplus/internal/cloud"
	"github.com/theocourtois/hetzner-ecoplus/internal/fleet"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/clock"
)

type Options struct {
	Cloud      cloud.Cloud
	Clock      clock.WithTicker
	Fleet      fleet.Config
	Workers    int
	Resync     time.Duration
	Logger     *slog.Logger
	NewLease   func() string
	SSHKeyTTL  time.Duration
	LeaseAlert time.Duration
	Observer   Observer
}

type Engine struct {
	opts    Options
	index   *Index
	queue   workqueue.TypedRateLimitingInterface[int64]
	ready   atomic.Bool
	halted  atomic.Bool
	alerted map[int64]struct{}
	alloc   sync.Mutex
	dmu     sync.Mutex
	desired map[int64]fleet.Desired
	keys    sshKeyCache
	cancel  func()
	wg      sync.WaitGroup
}

func New(opts Options) *Engine {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.NewLease == nil {
		opts.NewLease = randomLease
	}
	if opts.Workers < 1 {
		opts.Workers = 1
	}
	if opts.Observer == nil {
		opts.Observer = noopObserver{}
	}
	if opts.SSHKeyTTL == 0 {
		opts.SSHKeyTTL = 10 * time.Minute
	}
	queue := workqueue.NewTypedRateLimitingQueueWithConfig(
		workqueue.DefaultTypedControllerRateLimiter[int64](),
		workqueue.TypedRateLimitingQueueConfig[int64]{Name: "ecoplus", Clock: opts.Clock},
	)
	return &Engine{opts: opts, index: NewIndex(), queue: queue, desired: map[int64]fleet.Desired{}, alerted: map[int64]struct{}{}}
}

func randomLease() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(buf)
}

func (e *Engine) Index() *Index { return e.index }

func (e *Engine) Ready() bool { return e.ready.Load() && !e.halted.Load() }

func (e *Engine) Halt() { e.halted.Store(true) }

func (e *Engine) Halted() bool { return e.halted.Load() }

func (e *Engine) setDesired(id int64, d fleet.Desired) {
	e.dmu.Lock()
	defer e.dmu.Unlock()
	e.desired[id] = d
}

func (e *Engine) desiredFor(id int64) fleet.Desired {
	e.dmu.Lock()
	defer e.dmu.Unlock()
	return e.desired[id]
}

func (e *Engine) clearDesired(id int64) {
	e.dmu.Lock()
	defer e.dmu.Unlock()
	delete(e.desired, id)
}

func (e *Engine) hasDesired(id int64) bool {
	e.dmu.Lock()
	defer e.dmu.Unlock()
	return e.desired[id].Kind != fleet.DesiredNone
}
