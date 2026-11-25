package hetzner

import (
	"fmt"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/theocourtois/ecoplus/internal/cloud"
)

func classify(op string, err error) error {
	if err == nil {
		return nil
	}
	kind := cloud.KindUnavailable
	switch {
	case hcloud.IsError(err, hcloud.ErrorCodeNotFound):
		kind = cloud.KindNotFound
	case hcloud.IsError(err, hcloud.ErrorCodeUniquenessError):
		kind = cloud.KindConflict
	case hcloud.IsError(err, hcloud.ErrorCodeRateLimitExceeded):
		kind = cloud.KindRateLimited
	case hcloud.IsError(err, hcloud.ErrorCodeInvalidInput, hcloud.ErrorCodeForbidden, hcloud.ErrorCodeUnauthorized,
		hcloud.ErrorCodeInvalidServerType, hcloud.ErrorCodeResourceLimitExceeded, hcloud.ErrorCodeProtected):
		kind = cloud.KindInvalid
	}
	return cloud.Wrap(kind, fmt.Errorf("%s: %w", op, err))
}

func missing(op string, what string) error {
	return cloud.Wrap(cloud.KindNotFound, fmt.Errorf("%s: %s not found", op, what))
}
