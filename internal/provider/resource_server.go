package provider

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/theocourtois/hetzner-ecoplus/internal/api"
)

const operationTimeout = 20 * time.Minute

type serverResource struct {
	client *Client
}

func NewServerResource() resource.Resource { return &serverResource{} }

func (r *serverResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server"
}

func (r *serverResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	stringKeep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Hetzner Cloud server leased from the ecoplus pool. Destroying it wipes the disk and returns the server to the pool until just before its next billed hour.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: stringKeep, MarkdownDescription: "Hetzner server ID."},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Server name, an RFC 1123 hostname label unique within the Hetzner project. Changing it renames the server in place.",
				Validators:          []validator.String{stringvalidator.RegexMatches(regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`), "must be an RFC 1123 hostname label")},
			},
			"server_type": schema.StringAttribute{
				Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				MarkdownDescription: "Hetzner server type, for example `cx22`. Changing it replaces the server.",
			},
			"location": schema.StringAttribute{
				Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				MarkdownDescription: "Hetzner location, for example `nbg1`. Changing it replaces the server.",
			},
			"image": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Image name or snapshot ID. Changing it rebuilds the server in place.",
			},
			"ssh_keys": schema.SetAttribute{
				Optional: true, ElementType: types.StringType, PlanModifiers: []planmodifier.Set{setplanmodifier.RequiresReplace()},
				MarkdownDescription: "Names of Hetzner SSH keys to install. Changing the set replaces the server.",
			},
			"user_data": schema.StringAttribute{
				Optional: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          []validator.String{stringvalidator.LengthAtMost(api.MaxUserDataBytes)},
				MarkdownDescription: "Cloud-init user data, at most 32 KiB. An empty string is treated as unset. Changing it replaces the server.",
			},
			"labels": schema.MapAttribute{
				Optional: true, ElementType: types.StringType,
				Validators:          []validator.Map{noReservedLabelPrefix{}},
				MarkdownDescription: "Labels applied to the server while you hold it. Keys must not start with `ecoplus/`. Cleared when the server is released.",
			},
			"lease":        schema.StringAttribute{Computed: true, PlanModifiers: stringKeep, MarkdownDescription: "Lease ID fencing this resource to the server."},
			"ipv4_address": schema.StringAttribute{Computed: true, PlanModifiers: stringKeep, MarkdownDescription: "Public IPv4 address."},
			"ipv6_address": schema.StringAttribute{Computed: true, PlanModifiers: stringKeep, MarkdownDescription: "Public IPv6 address."},
			"reused": schema.BoolAttribute{
				Computed: true, PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "Whether the server came from the pool instead of being freshly created.",
			},
		},
	}
}

func (r *serverResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	r.client = client
}

func (r *serverResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan serverModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	body, diags := plan.request(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	srv, err := r.client.Create(ctx, body)
	if err != nil {
		resp.Diagnostics.AddError("Creating server failed", err.Error())
		return
	}
	resp.Diagnostics.Append(plan.apply(srv)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *serverResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state serverModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := strconv.ParseInt(state.ID.ValueString(), 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid server ID in state", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	srv, err := r.client.Get(ctx, id, state.Lease.ValueString())
	if IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading server failed", err.Error())
		return
	}
	resp.Diagnostics.Append(state.apply(srv)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *serverResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state serverModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := strconv.ParseInt(state.ID.ValueString(), 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid server ID in state", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	patch, diags := plan.patch(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	srv, err := r.client.Patch(ctx, id, state.Lease.ValueString(), patch)
	if IsNotFound(err) {
		resp.Diagnostics.AddError("Server disappeared", "The server is gone or its lease was reassigned. Run plan with refresh to recreate it.")
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Updating server failed", err.Error())
		return
	}
	resp.Diagnostics.Append(plan.apply(srv)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *serverResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state serverModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := strconv.ParseInt(state.ID.ValueString(), 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid server ID in state", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if err := r.client.Delete(ctx, id, state.Lease.ValueString()); err != nil {
		resp.Diagnostics.AddError("Deleting server failed", err.Error())
	}
}

func (r *serverResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, lease, ok := strings.Cut(req.ID, "/")
	if _, err := strconv.ParseInt(id, 10, 64); !ok || err != nil || lease == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Use the format <server_id>/<lease>.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("lease"), lease)...)
}
