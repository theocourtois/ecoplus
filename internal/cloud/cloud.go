package cloud

import (
	"context"
	"time"
)

type Server struct {
	ID         int64
	Name       string
	Created    time.Time
	ServerType string
	Location   string
	Image      string
	IPv4       string
	IPv6       string
	Labels     map[string]string
}

type SSHKey struct {
	ID   int64
	Name string
}

type CreateSpec struct {
	Name       string
	ServerType string
	Location   string
	Image      string
	SSHKeyIDs  []int64
	UserData   string
	Labels     map[string]string
}

type UpdateSpec struct {
	Name   string
	Labels map[string]string
}

type Cloud interface {
	ListServers(ctx context.Context, selector string) ([]Server, error)
	GetServer(ctx context.Context, id int64) (Server, error)
	GetServerByName(ctx context.Context, name string) (Server, error)
	CreateServer(ctx context.Context, spec CreateSpec) (Server, error)
	AwaitCreate(ctx context.Context, id int64) error
	UpdateServer(ctx context.Context, id int64, spec UpdateSpec) (Server, error)
	RebuildServer(ctx context.Context, id int64, image string) error
	DeleteServer(ctx context.Context, id int64) error
	ListSSHKeys(ctx context.Context) ([]SSHKey, error)
}

const SentinelName = "ecoplus-sentinel"

type Sentinel struct {
	Writer    string
	Heartbeat int64
}

type SentinelStore interface {
	ReadSentinel(ctx context.Context) (Sentinel, error)
	WriteSentinel(ctx context.Context, s Sentinel) error
}
