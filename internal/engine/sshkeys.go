package engine

import (
	"context"
	"sync"
	"time"

	"github.com/theocourtois/ecoplus/internal/api"
	"github.com/theocourtois/ecoplus/internal/cloud"
)

type sshKeyCache struct {
	mu      sync.Mutex
	byName  map[string]int64
	fetched time.Time
}

func (e *Engine) refreshKeys(ctx context.Context) error {
	keys, err := e.opts.Cloud.ListSSHKeys(cloud.WithPriority(ctx, cloud.Low))
	if err != nil {
		return upstream(err)
	}
	e.keys.byName = make(map[string]int64, len(keys))
	for _, k := range keys {
		e.keys.byName[k.Name] = k.ID
	}
	e.keys.fetched = e.opts.Clock.Now()
	return nil
}

func (e *Engine) resolveKeys(ctx context.Context, names []string) ([]int64, error) {
	e.keys.mu.Lock()
	defer e.keys.mu.Unlock()
	stale := e.keys.byName == nil || e.opts.Clock.Since(e.keys.fetched) > e.opts.SSHKeyTTL
	ids := make([]int64, 0, len(names))
	refreshed := false
	for _, name := range names {
		id, ok := e.keys.byName[name]
		if !ok || (stale && !refreshed) {
			if err := e.refreshKeys(ctx); err != nil {
				return nil, err
			}
			refreshed = true
			id, ok = e.keys.byName[name]
		}
		if !ok {
			return nil, api.NewError(api.CodeInvalidRequest, "unknown ssh key %q", name)
		}
		ids = append(ids, id)
	}
	return ids, nil
}
