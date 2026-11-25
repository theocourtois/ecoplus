package fake

import (
	"context"
	"errors"
	"fmt"

	"github.com/theocourtois/ecoplus/internal/cloud"
)

func (c *Cloud) nameTaken(name string, except int64) bool {
	for id, s := range c.servers {
		if s.Name == name && id != except {
			return true
		}
	}
	return false
}

func (c *Cloud) CreateServer(ctx context.Context, spec cloud.CreateSpec) (cloud.Server, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.enter(ctx, "create", spec.Name); err != nil {
		return cloud.Server{}, err
	}
	if c.nameTaken(spec.Name, 0) {
		return cloud.Server{}, cloud.Wrap(cloud.KindConflict, errors.New("name already used"))
	}
	c.nextID++
	s := &cloud.Server{
		ID: c.nextID, Name: spec.Name, Created: c.clk.Now(), ServerType: spec.ServerType, Location: spec.Location,
		Image: spec.Image, IPv4: fmt.Sprintf("192.0.2.%d", c.nextID%250), IPv6: fmt.Sprintf("2001:db8:%x::1", c.nextID),
		Labels: copyServer(cloud.Server{Labels: spec.Labels}).Labels,
	}
	c.servers[s.ID] = s
	c.disks[s.ID] = "pristine"
	return copyServer(*s), nil
}

func (c *Cloud) AwaitCreate(ctx context.Context, id int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.enter(ctx, "awaitcreate", id); err != nil {
		return err
	}
	if _, ok := c.servers[id]; !ok {
		return notFound(id)
	}
	return nil
}

func (c *Cloud) UpdateServer(ctx context.Context, id int64, spec cloud.UpdateSpec) (cloud.Server, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.enter(ctx, "update", id); err != nil {
		return cloud.Server{}, err
	}
	s, ok := c.servers[id]
	if !ok {
		return cloud.Server{}, notFound(id)
	}
	if spec.Name != "" {
		if c.nameTaken(spec.Name, id) {
			return cloud.Server{}, cloud.Wrap(cloud.KindConflict, errors.New("name already used"))
		}
		s.Name = spec.Name
	}
	if spec.Labels != nil {
		s.Labels = copyServer(cloud.Server{Labels: spec.Labels}).Labels
	}
	return copyServer(*s), nil
}

func (c *Cloud) RebuildServer(ctx context.Context, id int64, image string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.enter(ctx, "rebuild", id, image); err != nil {
		return err
	}
	s, ok := c.servers[id]
	if !ok {
		return notFound(id)
	}
	s.Image = image
	c.disks[id] = "pristine"
	return nil
}

func (c *Cloud) DeleteServer(ctx context.Context, id int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.enter(ctx, "delete", id); err != nil {
		return err
	}
	if _, ok := c.servers[id]; !ok {
		return notFound(id)
	}
	delete(c.servers, id)
	delete(c.disks, id)
	return nil
}
