package engine

import (
	"github.com/theocourtois/ecoplus/internal/api"
	"github.com/theocourtois/ecoplus/internal/cloud"
	"github.com/theocourtois/ecoplus/internal/fleet"
	"github.com/theocourtois/ecoplus/internal/labels"
)

func recordFromServer(s cloud.Server) (fleet.Record, error) {
	rec, err := labels.Decode(s.Labels)
	if err != nil {
		rec = fleet.Record{State: fleet.StateQuarantine}
	}
	rec.ID = s.ID
	rec.Name = s.Name
	rec.Created = s.Created.Unix()
	rec.ServerType = s.ServerType
	rec.Location = s.Location
	rec.IPv4 = s.IPv4
	rec.IPv6 = s.IPv6
	if rec.Image == "" {
		rec.Image = s.Image
	}
	return rec, err
}

func toAPI(r fleet.Record) api.Server {
	return api.Server{
		ID: r.ID, Name: r.Name, ServerType: r.ServerType, Location: r.Location, Image: r.Image,
		Lease: r.Lease, IPv4: r.IPv4, IPv6: r.IPv6, Reused: r.Reuse > 0, Labels: r.Tenant,
	}
}
