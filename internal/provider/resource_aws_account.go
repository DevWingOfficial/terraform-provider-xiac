package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure the resource satisfies the framework interfaces.
var (
	_ resource.Resource              = (*awsAccountResource)(nil)
	_ resource.ResourceWithConfigure = (*awsAccountResource)(nil)
)

// awsAccountResource declares a connection between the tenant and one AWS
// account (a cross-account read-only role) as Terraform state.
type awsAccountResource struct {
	client *Client
}

// NewAWSAccountResource is the resource factory registered on the provider.
func NewAWSAccountResource() resource.Resource {
	return &awsAccountResource{}
}

// awsAccountModel maps the xiac_aws_account resource schema to Go.
type awsAccountModel struct {
	ID         types.String `tfsdk:"id"`
	ProviderID types.String `tfsdk:"provider_id"`
	IAMRole    types.String `tfsdk:"iam_role"`
	Regions    types.List   `tfsdk:"regions"`
	STSRegion  types.String `tfsdk:"sts_region"`
	ReadOnly   types.Bool   `tfsdk:"readonly"`
	ExternalID types.String `tfsdk:"external_id"`
	AccountID  types.String `tfsdk:"account_id"`
	Status     types.String `tfsdk:"status"`
}

func (r *awsAccountResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aws_account"
}

func (r *awsAccountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Connect an AWS account to XIaC via a cross-account read-only role.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Platform-assigned provider id.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"provider_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Existing tenant-scoped XIaC provider id to adopt. Omit for standalone creation.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"iam_role": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The cross-account IAM role ARN XIaC assumes to scan the account.",
			},
			"regions": schema.ListAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "AWS regions to scan. Empty means the platform's default region set.",
			},
			"sts_region": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "AWS region XIaC uses for the STS AssumeRole bootstrap call.",
			},
			"readonly": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				MarkdownDescription: "Whether the connection is read-only. Only `true` is supported today; `false` returns an error.",
			},
			"external_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Client-generated UUID required by the role trust policy. Generate it with `random_uuid` and pass the same value to the IAM role and XIaC.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"account_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The AWS account id discovered when the role is verified.",
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Connection status (e.g. `connected`, `error`).",
			},
		},
	}
}

// ensureProvider creates a provider for standalone Terraform usage or adopts
// the exact tenant-scoped provider created by web onboarding. Adoption is
// fail-closed: any lookup/error/mismatch is returned and never falls back to a
// POST that could create a duplicate record.
func (r *awsAccountResource) ensureProvider(ctx context.Context, providerID string, regions []string, stsRegion, clientExternalID string) (id, externalID string, err error) {
	if providerID == "" {
		return r.client.CreateProvider(ctx, regions, stsRegion, clientExternalID)
	}

	_, _, storedExternalID, found, err := r.client.GetProvider(ctx, providerID)
	if err != nil {
		return "", "", fmt.Errorf("read provider %q for adoption: %w", providerID, err)
	}
	if !found {
		return "", "", fmt.Errorf("provider %q was not found for this tenant", providerID)
	}
	if storedExternalID != clientExternalID {
		return "", "", fmt.Errorf("provider %q external_id does not match the client trust anchor", providerID)
	}
	if err := r.client.UpdateProvider(ctx, providerID, regions, stsRegion); err != nil {
		return "", "", fmt.Errorf("update adopted provider %q: %w", providerID, err)
	}
	return providerID, storedExternalID, nil
}

func (r *awsAccountResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		// Provider not yet configured (e.g. during ValidateConfig); nothing to do.
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("Expected *Client, got %T. This is a provider bug.", req.ProviderData),
		)
		return
	}
	r.client = client
}

// regionsFromModel extracts the []string regions from the model, tolerating a
// null/unknown list.
func regionsFromModel(ctx context.Context, m awsAccountModel) ([]string, diag.Diagnostics) {
	if m.Regions.IsNull() || m.Regions.IsUnknown() {
		return nil, nil
	}
	var regions []string
	d := m.Regions.ElementsAs(ctx, &regions, false)
	return regions, d
}

func (r *awsAccountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan awsAccountModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !plan.ReadOnly.ValueBool() {
		resp.Diagnostics.AddError(
			"Write mode not supported",
			"write mode not supported yet (readonly must be true)",
		)
		return
	}

	regions, rd := regionsFromModel(ctx, plan)
	resp.Diagnostics.Append(rd...)
	if resp.Diagnostics.HasError() {
		return
	}

	if plan.ExternalID.IsNull() || plan.ExternalID.IsUnknown() || plan.ExternalID.ValueString() == "" {
		resp.Diagnostics.AddError(
			"Missing client-generated external ID",
			"external_id is required; generate a UUID client-side and use the same value in the AWS role trust policy",
		)
		return
	}
	clientExternalID := plan.ExternalID.ValueString()
	stsRegion := plan.STSRegion.ValueString()
	if stsRegion == "" {
		resp.Diagnostics.AddError("Missing STS region", "sts_region must resolve to a non-empty AWS region")
		return
	}
	providerID := ""
	if !plan.ProviderID.IsNull() && !plan.ProviderID.IsUnknown() {
		providerID = plan.ProviderID.ValueString()
	}

	id, externalID, err := r.ensureProvider(ctx, providerID, regions, stsRegion, clientExternalID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create or adopt XIaC provider", err.Error())
		return
	}

	connected, accountID, detail, err := r.client.Connect(ctx, id, plan.IAMRole.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to connect AWS account", err.Error())
		return
	}
	if !connected {
		resp.Diagnostics.AddError(
			"AWS account connection rejected",
			fmt.Sprintf("the platform did not accept the role: %s", detail),
		)
		return
	}

	status, refAccountID, refExternalID, found, err := r.client.GetProvider(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read XIaC provider after connect", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Provider missing after create", "the provider vanished immediately after being created")
		return
	}
	if refExternalID != "" {
		externalID = refExternalID
	}
	if refAccountID != "" {
		accountID = refAccountID
	}

	plan.ID = types.StringValue(id)
	plan.ProviderID = types.StringValue(id)
	plan.ExternalID = types.StringValue(externalID)
	plan.AccountID = types.StringValue(accountID)
	plan.Status = types.StringValue(status)
	plan.ReadOnly = types.BoolValue(true)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *awsAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state awsAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	status, accountID, externalID, found, err := r.client.GetProvider(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read XIaC provider", err.Error())
		return
	}
	if !found {
		// The provider no longer exists on the platform: drop it from state so
		// Terraform plans a re-create.
		resp.State.RemoveResource(ctx)
		return
	}

	state.Status = types.StringValue(status)
	if accountID != "" {
		state.AccountID = types.StringValue(accountID)
	}
	if externalID != "" {
		state.ExternalID = types.StringValue(externalID)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *awsAccountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state awsAccountModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !plan.ReadOnly.ValueBool() {
		resp.Diagnostics.AddError(
			"Write mode not supported",
			"write mode not supported yet (readonly must be true)",
		)
		return
	}

	// id + external_id are stable across an in-place update.
	plan.ID = state.ID
	plan.ProviderID = state.ProviderID
	plan.ExternalID = state.ExternalID

	id := state.ID.ValueString()
	regions, rd := regionsFromModel(ctx, plan)
	resp.Diagnostics.Append(rd...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.UpdateProvider(ctx, id, regions, plan.STSRegion.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to update XIaC provider settings", err.Error())
		return
	}

	// A change to iam_role or regions requires re-connecting so the platform
	// re-verifies the role (regions changes are honored on the next scan; for
	// MVP we simply re-connect and refresh).
	connected, accountID, detail, err := r.client.Connect(ctx, id, plan.IAMRole.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to reconnect AWS account", err.Error())
		return
	}
	if !connected {
		resp.Diagnostics.AddError(
			"AWS account connection rejected",
			fmt.Sprintf("the platform did not accept the role: %s", detail),
		)
		return
	}

	status, refAccountID, refExternalID, found, err := r.client.GetProvider(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read XIaC provider after update", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	if refExternalID != "" {
		plan.ExternalID = types.StringValue(refExternalID)
	}
	if refAccountID != "" {
		accountID = refAccountID
	}
	plan.AccountID = types.StringValue(accountID)
	plan.Status = types.StringValue(status)
	plan.ReadOnly = types.BoolValue(true)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *awsAccountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state awsAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteProvider(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete XIaC provider", err.Error())
	}
}
