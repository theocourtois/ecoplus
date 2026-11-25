package engine

import (
	"context"
	"errors"

	"github.com/theocourtois/ecoplus/internal/api"
	"github.com/theocourtois/ecoplus/internal/cloud"
)

func upstream(err error) *api.Error {
	switch {
	case cloud.KindOf(err) == cloud.KindRateLimited:
		return api.NewError(api.CodeRateLimited, "hetzner api budget exhausted: %v", err)
	case cloud.IsConflict(err):
		return api.NewError(api.CodeConflict, "%v", err)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return api.NewError(api.CodeUpstream, "request ended before the server was ready: %v", err)
	}
	return api.NewError(api.CodeUpstream, "hetzner api failure: %v", err)
}

func notReady() *api.Error {
	return api.NewError(api.CodeNotReady, "controller index is not loaded")
}

func notFound() *api.Error {
	return api.NewError(api.CodeNotFound, "server not found")
}
