package provider

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/theocourtois/ecoplus/internal/api"
)

type serverModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	ServerType types.String `tfsdk:"server_type"`
	Location   types.String `tfsdk:"location"`
	Image      types.String `tfsdk:"image"`
	SSHKeys    types.Set    `tfsdk:"ssh_keys"`
	UserData   types.String `tfsdk:"user_data"`
	Labels     types.Map    `tfsdk:"labels"`
	Lease      types.String `tfsdk:"lease"`
	IPv4       types.String `tfsdk:"ipv4_address"`
	IPv6       types.String `tfsdk:"ipv6_address"`
	Reused     types.Bool   `tfsdk:"reused"`
}

func (m serverModel) request(ctx context.Context) (api.ServerRequest, diag.Diagnostics) {
	req := api.ServerRequest{
		Name: m.Name.ValueString(), ServerType: m.ServerType.ValueString(), Location: m.Location.ValueString(),
		Image: m.Image.ValueString(), UserData: m.UserData.ValueString(),
	}
	var diags diag.Diagnostics
	if !m.SSHKeys.IsNull() && !m.SSHKeys.IsUnknown() {
		diags.Append(m.SSHKeys.ElementsAs(ctx, &req.SSHKeys, false)...)
	}
	if !m.Labels.IsNull() && !m.Labels.IsUnknown() {
		diags.Append(m.Labels.ElementsAs(ctx, &req.Labels, false)...)
	}
	return req, diags
}

func (m *serverModel) apply(srv api.Server) diag.Diagnostics {
	var diags diag.Diagnostics
	m.ID = types.StringValue(strconv.FormatInt(srv.ID, 10))
	m.Name = types.StringValue(srv.Name)
	m.ServerType = types.StringValue(srv.ServerType)
	m.Location = types.StringValue(srv.Location)
	m.Image = types.StringValue(srv.Image)
	m.Lease = types.StringValue(srv.Lease)
	m.IPv4 = types.StringValue(srv.IPv4)
	m.IPv6 = types.StringValue(srv.IPv6)
	m.Reused = types.BoolValue(srv.Reused)
	m.Labels, diags = labelsValue(m.Labels, srv.Labels)
	return diags
}

func labelsValue(current types.Map, labels map[string]string) (types.Map, diag.Diagnostics) {
	if len(labels) == 0 && current.IsNull() {
		return types.MapNull(types.StringType), nil
	}
	elems := make(map[string]attr.Value, len(labels))
	for k, v := range labels {
		elems[k] = types.StringValue(v)
	}
	return types.MapValue(types.StringType, elems)
}

func (m serverModel) patch(ctx context.Context, prior serverModel) (api.ServerPatch, diag.Diagnostics) {
	var p api.ServerPatch
	var diags diag.Diagnostics
	if !m.Name.Equal(prior.Name) {
		name := m.Name.ValueString()
		p.Name = &name
	}
	if !m.Image.Equal(prior.Image) {
		image := m.Image.ValueString()
		p.Image = &image
	}
	if !m.Labels.Equal(prior.Labels) {
		labels := map[string]string{}
		if !m.Labels.IsNull() {
			diags.Append(m.Labels.ElementsAs(ctx, &labels, false)...)
		}
		p.Labels = &labels
	}
	return p, diags
}
