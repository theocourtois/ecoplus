package hetzner

import (
	"net"
	"strconv"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/theocourtois/hetzner-ecoplus/internal/cloud"
)

func toServer(s *hcloud.Server) cloud.Server {
	out := cloud.Server{ID: s.ID, Name: s.Name, Created: s.Created, Labels: s.Labels}
	if s.ServerType != nil {
		out.ServerType = s.ServerType.Name
	}
	if s.Location != nil {
		out.Location = s.Location.Name
	}
	if s.Image != nil {
		out.Image = s.Image.Name
		if out.Image == "" {
			out.Image = strconv.FormatInt(s.Image.ID, 10)
		}
	}
	if !s.PublicNet.IPv4.IsUnspecified() && s.PublicNet.IPv4.IP != nil {
		out.IPv4 = s.PublicNet.IPv4.IP.String()
	}
	if s.PublicNet.IPv6.Network != nil {
		out.IPv6 = hostAddress(s.PublicNet.IPv6.IP)
	}
	return out
}

func imageRef(image string) *hcloud.Image {
	if id, err := strconv.ParseInt(image, 10, 64); err == nil {
		return &hcloud.Image{ID: id}
	}
	return &hcloud.Image{Name: image}
}

func hostAddress(prefix net.IP) string {
	addr := append(net.IP(nil), prefix.To16()...)
	if len(addr) == net.IPv6len {
		addr[net.IPv6len-1] = 1
	}
	return addr.String()
}
