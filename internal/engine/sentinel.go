package engine

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/theocourtois/ecoplus/internal/cloud"
	"k8s.io/utils/clock"
)

type GuardOptions struct {
	Store      cloud.SentinelStore
	Clock      clock.WithTicker
	InstanceID string
	Period     time.Duration
	Halt       func()
	Logger     *slog.Logger
	Pause      func(ctx context.Context) error
}

type Guard struct {
	opts GuardOptions
}

func NewGuard(opts GuardOptions) *Guard {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Pause == nil {
		opts.Pause = func(ctx context.Context) error {
			delay := 5*time.Second + jitter(10*time.Second)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-opts.Clock.After(delay):
				return nil
			}
		}
	}
	return &Guard{opts: opts}
}

type ForeignWriterError struct {
	Writer string
}

func (e *ForeignWriterError) Error() string {
	return fmt.Sprintf("another controller instance %q is writing to this project", e.Writer)
}

func (g *Guard) foreign(s cloud.Sentinel) bool {
	if s.Writer == "" || s.Writer == g.opts.InstanceID {
		return false
	}
	age := g.opts.Clock.Now().Unix() - s.Heartbeat
	return age < int64((2 * g.opts.Period).Seconds())
}

func (g *Guard) Check(ctx context.Context) error {
	s, err := g.opts.Store.ReadSentinel(cloud.WithPriority(ctx, cloud.Low))
	if err != nil {
		return fmt.Errorf("read sentinel: %w", err)
	}
	if g.foreign(s) {
		return &ForeignWriterError{Writer: s.Writer}
	}
	return g.beat(ctx)
}

func (g *Guard) beat(ctx context.Context) error {
	ctx = cloud.WithPriority(ctx, cloud.Low)
	if err := g.opts.Store.WriteSentinel(ctx, cloud.Sentinel{Writer: g.opts.InstanceID, Heartbeat: g.opts.Clock.Now().Unix()}); err != nil {
		return fmt.Errorf("write sentinel: %w", err)
	}
	if err := g.opts.Pause(ctx); err != nil {
		return err
	}
	s, err := g.opts.Store.ReadSentinel(ctx)
	if err != nil {
		return fmt.Errorf("re-read sentinel: %w", err)
	}
	if s.Writer != g.opts.InstanceID {
		return &ForeignWriterError{Writer: s.Writer}
	}
	return nil
}

func (g *Guard) Tick(ctx context.Context) {
	err := g.Check(ctx)
	var foreign *ForeignWriterError
	if errors.As(err, &foreign) {
		g.opts.Logger.Error("alert: single-writer violation, halting all writes", "writer", foreign.Writer)
		g.opts.Halt()
		return
	}
	if err != nil && ctx.Err() == nil {
		g.opts.Logger.Warn("sentinel check failed", "error", err)
	}
}

func (g *Guard) Run(ctx context.Context) {
	ticker := g.opts.Clock.NewTicker(g.opts.Period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			g.Tick(ctx)
		}
	}
}

func jitter(limit time.Duration) time.Duration {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(limit)))
	if err != nil {
		return limit / 2
	}
	return time.Duration(n.Int64())
}
