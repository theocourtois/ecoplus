package hetzner

import (
	"context"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/theocourtois/hetzner-ecoplus/internal/cloud"
)

const pageSize = 50

func (c *Client) ListServers(ctx context.Context, selector string) ([]cloud.Server, error) {
	servers, err := c.api.Server.AllWithOpts(ctx, hcloud.ServerListOpts{ListOpts: hcloud.ListOpts{LabelSelector: selector, PerPage: pageSize}})
	if err != nil {
		return nil, classify("list servers", err)
	}
	out := make([]cloud.Server, 0, len(servers))
	for _, s := range servers {
		out = append(out, toServer(s))
	}
	return out, nil
}

func (c *Client) GetServer(ctx context.Context, id int64) (cloud.Server, error) {
	s, _, err := c.api.Server.GetByID(ctx, id)
	if err != nil {
		return cloud.Server{}, classify("get server", err)
	}
	if s == nil {
		return cloud.Server{}, missing("get server", "server")
	}
	return toServer(s), nil
}

func (c *Client) GetServerByName(ctx context.Context, name string) (cloud.Server, error) {
	s, _, err := c.api.Server.GetByName(ctx, name)
	if err != nil {
		return cloud.Server{}, classify("get server by name", err)
	}
	if s == nil {
		return cloud.Server{}, missing("get server by name", "server")
	}
	return toServer(s), nil
}

func (c *Client) CreateServer(ctx context.Context, spec cloud.CreateSpec) (cloud.Server, error) {
	opts := hcloud.ServerCreateOpts{
		Name: spec.Name, ServerType: &hcloud.ServerType{Name: spec.ServerType}, Location: &hcloud.Location{Name: spec.Location},
		Image: imageRef(spec.Image), UserData: spec.UserData, Labels: spec.Labels,
	}
	for _, id := range spec.SSHKeyIDs {
		opts.SSHKeys = append(opts.SSHKeys, &hcloud.SSHKey{ID: id})
	}
	res, _, err := c.api.Server.Create(ctx, opts)
	if err != nil {
		return cloud.Server{}, classify("create server", err)
	}
	actions := append([]*hcloud.Action{res.Action}, res.NextActions...)
	c.mu.Lock()
	c.pending[res.Server.ID] = actions
	c.mu.Unlock()
	return toServer(res.Server), nil
}

func (c *Client) AwaitCreate(ctx context.Context, id int64) error {
	c.mu.Lock()
	actions := c.pending[id]
	c.mu.Unlock()
	if len(actions) > 0 {
		if err := c.api.Action.WaitFor(ctx, actions...); err != nil {
			return classify("await create", err)
		}
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil
	}
	return c.pollRunning(ctx, id)
}

func (c *Client) pollRunning(ctx context.Context, id int64) error {
	wait := time.Second
	for {
		s, _, err := c.api.Server.GetByID(ctx, id)
		if err != nil {
			return classify("await create", err)
		}
		if s == nil {
			return missing("await create", "server")
		}
		if s.Status != hcloud.ServerStatusInitializing && s.Status != hcloud.ServerStatusStarting {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.clk.After(wait):
		}
		wait = min(wait*2, 10*time.Second)
	}
}

func (c *Client) UpdateServer(ctx context.Context, id int64, spec cloud.UpdateSpec) (cloud.Server, error) {
	s, _, err := c.api.Server.Update(ctx, &hcloud.Server{ID: id}, hcloud.ServerUpdateOpts{Name: spec.Name, Labels: spec.Labels})
	if err != nil {
		return cloud.Server{}, classify("update server", err)
	}
	return toServer(s), nil
}

func (c *Client) RebuildServer(ctx context.Context, id int64, image string) error {
	res, _, err := c.api.Server.RebuildWithResult(ctx, &hcloud.Server{ID: id}, hcloud.ServerRebuildOpts{Image: imageRef(image)})
	if err != nil {
		return classify("rebuild server", err)
	}
	return classify("rebuild server", c.api.Action.WaitFor(ctx, res.Action))
}

func (c *Client) DeleteServer(ctx context.Context, id int64) error {
	_, _, err := c.api.Server.DeleteWithResult(ctx, &hcloud.Server{ID: id})
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
	return classify("delete server", err)
}

func (c *Client) ListSSHKeys(ctx context.Context) ([]cloud.SSHKey, error) {
	keys, err := c.api.SSHKey.All(ctx)
	if err != nil {
		return nil, classify("list ssh keys", err)
	}
	out := make([]cloud.SSHKey, 0, len(keys))
	for _, k := range keys {
		out = append(out, cloud.SSHKey{ID: k.ID, Name: k.Name})
	}
	return out, nil
}
