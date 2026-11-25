package provider

import (
	"context"
	"os"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type Option func(*ecoplusProvider)

func WithBackoff(d time.Duration) Option {
	return func(p *ecoplusProvider) { p.backoff = d }
}

type ecoplusProvider struct {
	version string
	backoff time.Duration
}

type providerModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	Token    types.String `tfsdk:"token"`
}

func New(version string, opts ...Option) func() provider.Provider {
	return func() provider.Provider {
		p := &ecoplusProvider{version: version, backoff: time.Second}
		for _, o := range opts {
			o(p)
		}
		return p
	}
}

func (p *ecoplusProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "hetzner-ecoplus"
	resp.Version = p.version
}

func (p *ecoplusProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Terraform provider for the ecoplus controller, which pools Hetzner Cloud servers to avoid paying for unused billed hours.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Base URL of the ecoplus controller. Falls back to the `ECOPLUS_ENDPOINT` environment variable.",
			},
			"token": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Bearer token for the controller. Falls back to the `ECOPLUS_TOKEN` environment variable.",
			},
		},
	}
}

func (p *ecoplusProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	endpoint := valueOrEnv(cfg.Endpoint, "ECOPLUS_ENDPOINT")
	token := valueOrEnv(cfg.Token, "ECOPLUS_TOKEN")
	if endpoint == "" {
		resp.Diagnostics.AddAttributeError(path.Root("endpoint"), "Missing endpoint", "Set `endpoint` or the ECOPLUS_ENDPOINT environment variable.")
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Missing token", "Set `token` or the ECOPLUS_TOKEN environment variable.")
	}
	if resp.Diagnostics.HasError() {
		return
	}
	client := NewClient(endpoint, token, p.version, p.backoff)
	resp.ResourceData = client
	resp.DataSourceData = client
}

func valueOrEnv(v types.String, env string) string {
	if !v.IsNull() && !v.IsUnknown() && v.ValueString() != "" {
		return v.ValueString()
	}
	return os.Getenv(env)
}

func (p *ecoplusProvider) DataSources(context.Context) []func() datasource.DataSource { return nil }

func (p *ecoplusProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{NewServerResource}
}
