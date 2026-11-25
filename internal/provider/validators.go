package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/theocourtois/ecoplus/internal/api"
)

type noReservedLabelPrefix struct{}

func (noReservedLabelPrefix) Description(context.Context) string {
	return fmt.Sprintf("label keys must not start with %q", api.TenantLabelPrefix)
}

func (v noReservedLabelPrefix) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v noReservedLabelPrefix) ValidateMap(ctx context.Context, req validator.MapRequest, resp *validator.MapResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	for key := range req.ConfigValue.Elements() {
		if strings.HasPrefix(key, api.TenantLabelPrefix) {
			resp.Diagnostics.AddAttributeError(req.Path, "Reserved label key", v.Description(ctx)+": "+key)
		}
	}
}
