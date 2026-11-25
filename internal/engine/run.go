package engine

import (
	"context"
	"fmt"
)

func (e *Engine) Bootstrap(ctx context.Context) error {
	if err := e.Resync(ctx); err != nil {
		return fmt.Errorf("initial resync: %w", err)
	}
	e.ready.Store(true)
	return nil
}

func (e *Engine) Start(ctx context.Context) error {
	if err := e.Bootstrap(ctx); err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	e.cancel = cancel
	for i := 0; i < e.opts.Workers; i++ {
		e.wg.Add(1)
		go e.worker(runCtx)
	}
	if e.opts.Resync > 0 {
		e.wg.Add(1)
		go e.resyncLoop(runCtx)
	}
	return nil
}

func (e *Engine) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
	e.queue.ShutDown()
	e.wg.Wait()
}

func (e *Engine) worker(ctx context.Context) {
	defer e.wg.Done()
	for {
		id, shutdown := e.queue.Get()
		if shutdown {
			return
		}
		if err := e.Process(ctx, id); err != nil {
			e.opts.Logger.Warn("reconcile failed", "server_id", id, "error", err)
		} else {
			e.queue.Forget(id)
		}
		e.queue.Done(id)
	}
}

func (e *Engine) resyncLoop(ctx context.Context) {
	defer e.wg.Done()
	ticker := e.opts.Clock.NewTicker(e.opts.Resync)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			if err := e.Resync(ctx); err != nil {
				e.opts.Logger.Warn("resync failed", "error", err)
			}
		}
	}
}
