package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// defaultEndpoint is the hosted XIaC platform. Users on the published provider
// hit production by default; set `endpoint` (or XIAC_ENDPOINT) to point at a
// local/dev platform, e.g. http://127.0.0.1:8099.
const defaultEndpoint = "https://api.xiac.co"

// Ensure xiacProvider satisfies the provider.Provider interface.
var _ provider.Provider = (*xiacProvider)(nil)

// xiacProvider is the terraform-provider-xiac root provider. It configures an
// API client from the tenant api_key + platform endpoint and exposes the
// xiac_aws_account resource.
type xiacProvider struct{}

// New is the provider factory passed to providerserver.Serve.
func New() provider.Provider {
	return &xiacProvider{}
}

func (p *xiacProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "xiac"
}

func (p *xiacProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Declare XIaC cloud account connections as Terraform resources.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Tenant API key sent as the `X-Api-Key` header. Falls back to the `XIAC_API_KEY` environment variable when unset.",
			},
			"endpoint": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Platform base URL. Falls back to the `XIAC_ENDPOINT` environment variable, then defaults to the hosted platform `https://api.xiac.co`. Set it to a local platform (e.g. `http://127.0.0.1:8099`) for dev.",
			},
		},
	}
}

// providerModel maps the provider configuration block.
type providerModel struct {
	APIKey   types.String `tfsdk:"api_key"`
	Endpoint types.String `tfsdk:"endpoint"`
}

func (p *xiacProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// api_key: config value, else XIAC_API_KEY env, else error.
	apiKey := cfg.APIKey.ValueString()
	if cfg.APIKey.IsNull() || apiKey == "" {
		apiKey = os.Getenv("XIAC_API_KEY")
	}
	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Missing XIaC API key",
			"api_key must be set in the provider block or via the XIAC_API_KEY environment variable.",
		)
		return
	}

	// endpoint: config value, else XIAC_ENDPOINT env, else default.
	endpoint := cfg.Endpoint.ValueString()
	if cfg.Endpoint.IsNull() || endpoint == "" {
		endpoint = os.Getenv("XIAC_ENDPOINT")
	}
	if endpoint == "" {
		endpoint = defaultEndpoint
	}

	client := NewClient(endpoint, apiKey)
	resp.ResourceData = client
}

func (p *xiacProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewAWSAccountResource,
	}
}

func (p *xiacProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
