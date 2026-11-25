package api

const (
	HeaderLease         = "Ecoplus-Lease"
	HeaderRetryAfter    = "Retry-After"
	HeaderAuthorization = "Authorization"
	HeaderUserAgent     = "User-Agent"
	PathServers         = "/v1/servers"
	PathSelfActivity    = "/v1/self/activity"
	UserAgentPrefix     = "terraform-provider-hetzner-ecoplus/"
	MaxUserDataBytes    = 32 * 1024
	TenantLabelPrefix   = "ecoplus/"
)

type ServerRequest struct {
	Name       string            `json:"name"`
	ServerType string            `json:"server_type"`
	Location   string            `json:"location"`
	Image      string            `json:"image"`
	SSHKeys    []string          `json:"ssh_keys,omitempty"`
	UserData   string            `json:"user_data,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}

type ServerPatch struct {
	Name   *string            `json:"name,omitempty"`
	Image  *string            `json:"image,omitempty"`
	Labels *map[string]string `json:"labels,omitempty"`
}

type Server struct {
	ID         int64             `json:"id"`
	Name       string            `json:"name"`
	ServerType string            `json:"server_type"`
	Location   string            `json:"location"`
	Image      string            `json:"image"`
	Lease      string            `json:"lease"`
	IPv4       string            `json:"ipv4_address"`
	IPv6       string            `json:"ipv6_address"`
	Reused     bool              `json:"reused"`
	Labels     map[string]string `json:"labels,omitempty"`
}

type Activity struct {
	Activity string `json:"activity"`
}
