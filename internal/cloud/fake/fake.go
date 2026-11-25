package fake

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/theocourtois/ecoplus/internal/cloud"
	"k8s.io/utils/clock"
)

type Cloud struct {
	mu         sync.Mutex
	clk        clock.Clock
	nextID     int64
	servers    map[int64]*cloud.Server
	sshKeys    []cloud.SSHKey
	failures   map[string][]failure
	calls      map[string]int
	disks      map[int64]string
	log        []string
	priorities map[string][]cloud.Priority
	sentinel   cloud.Sentinel
}

type failure struct {
	call int
	err  error
}

func New(clk clock.Clock) *Cloud {
	return &Cloud{
		clk: clk, nextID: 1000, servers: map[int64]*cloud.Server{}, failures: map[string][]failure{},
		calls: map[string]int{}, disks: map[int64]string{}, priorities: map[string][]cloud.Priority{},
	}
}

func (c *Cloud) AddSSHKey(name string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	c.sshKeys = append(c.sshKeys, cloud.SSHKey{ID: c.nextID, Name: name})
	return c.nextID
}

func (c *Cloud) FailOn(op string, call int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures[op] = append(c.failures[op], failure{call: call, err: err})
}

func (c *Cloud) Calls(op string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[op]
}

func (c *Cloud) Log() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.log...)
}

func (c *Cloud) enter(ctx context.Context, op string, detail ...any) error {
	c.calls[op]++
	c.priorities[op] = append(c.priorities[op], cloud.PriorityOf(ctx))
	c.log = append(c.log, op+" "+fmt.Sprint(detail...))
	for _, f := range c.failures[op] {
		if f.call == c.calls[op] {
			return f.err
		}
	}
	return nil
}

func (c *Cloud) Disk(id int64) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.disks[id]
}

func (c *Cloud) WriteDisk(id int64, data string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.disks[id] = data
}

func (c *Cloud) All() []cloud.Server {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshot(func(cloud.Server) bool { return true })
}

func (c *Cloud) snapshot(keep func(cloud.Server) bool) []cloud.Server {
	out := make([]cloud.Server, 0, len(c.servers))
	for _, s := range c.servers {
		if keep(*s) {
			out = append(out, copyServer(*s))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func copyServer(s cloud.Server) cloud.Server {
	labels := make(map[string]string, len(s.Labels))
	for k, v := range s.Labels {
		labels[k] = v
	}
	s.Labels = labels
	return s
}

func matches(selector string, labels map[string]string) bool {
	if selector == "" {
		return true
	}
	key, value, _ := strings.Cut(selector, "=")
	return labels[key] == value
}

func notFound(id int64) error {
	return cloud.Wrap(cloud.KindNotFound, fmt.Errorf("server %d not found", id))
}

func (c *Cloud) ListServers(ctx context.Context, selector string) ([]cloud.Server, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.enter(ctx, "list", selector); err != nil {
		return nil, err
	}
	return c.snapshot(func(s cloud.Server) bool { return matches(selector, s.Labels) }), nil
}

func (c *Cloud) GetServer(ctx context.Context, id int64) (cloud.Server, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.enter(ctx, "get", id); err != nil {
		return cloud.Server{}, err
	}
	s, ok := c.servers[id]
	if !ok {
		return cloud.Server{}, notFound(id)
	}
	return copyServer(*s), nil
}

func (c *Cloud) GetServerByName(ctx context.Context, name string) (cloud.Server, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.enter(ctx, "getbyname", name); err != nil {
		return cloud.Server{}, err
	}
	for _, s := range c.servers {
		if s.Name == name {
			return copyServer(*s), nil
		}
	}
	return cloud.Server{}, cloud.Wrap(cloud.KindNotFound, errors.New("no such name"))
}

func (c *Cloud) ListSSHKeys(ctx context.Context) ([]cloud.SSHKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.enter(ctx, "listsshkeys"); err != nil {
		return nil, err
	}
	return append([]cloud.SSHKey(nil), c.sshKeys...), nil
}

func (c *Cloud) ReadSentinel(ctx context.Context) (cloud.Sentinel, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.enter(ctx, "readsentinel"); err != nil {
		return cloud.Sentinel{}, err
	}
	return c.sentinel, nil
}

func (c *Cloud) WriteSentinel(ctx context.Context, s cloud.Sentinel) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.enter(ctx, "writesentinel"); err != nil {
		return err
	}
	c.sentinel = s
	return nil
}

func (c *Cloud) Priorities(op string) []cloud.Priority {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]cloud.Priority(nil), c.priorities[op]...)
}
