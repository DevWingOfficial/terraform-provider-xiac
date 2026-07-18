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

var (
	_ resource.Resource              = (*awsAccountResource)(nil)
	_ resource.ResourceWithConfigure = (*awsAccountResource)(nil)
)

type awsAccountResource struct {
	client *Client
}

func NewAWSAccountResource() resource.Resource {
	return &awsAccountResource{}
}

// awsAccountModel uses AWS-native account_id as its only resource identity.
// XIaC's private database UUID never enters Terraform state.
type awsAccountModel struct {
	AccountID  types.String `tfsdk:"account_id"`
	IAMRole    types.String `tfsdk:"iam_role"`
	Regions    types.List   `tfsdk:"regions"`
	STSRegion  types.String `tfsdk:"sts_region"`
	ReadOnly   types.Bool   `tfsdk:"readonly"`
	ExternalID types.String `tfsdk:"external_id"`
	Status     types.String `tfsdk:"status"`
}

func (r *awsAccountResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aws_account"
}

func (r *awsAccountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Connect an AWS account to XIaC via a cross-account read-only role. The AWS account ID is the public resource identity.",
		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "AWS account ID registered as the tenant-scoped XIaC scope.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"iam_role": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Cross-account IAM role ARN XIaC assumes to discover the account.",
			},
			"regions": schema.ListAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "AWS regions to scan. Empty lets XIaC resolve enabled regions.",
			},
			"sts_region": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "AWS region used for the STS AssumeRole bootstrap call.",
			},
			"readonly": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				MarkdownDescription: "Must be true. XIaC rejects AWS roles proven to allow writes.",
			},
			"external_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Client-generated UUID placed in both the AWS trust policy and XIaC registration.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Current XIaC connection status.",
			},
		},
	}
}

func (r *awsAccountResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
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

func regionsFromModel(ctx context.Context, model awsAccountModel) ([]string, diag.Diagnostics) {
	if model.Regions.IsNull() || model.Regions.IsUnknown() {
		return []string{}, nil
	}
	var regions []string
	diagnostics := model.Regions.ElementsAs(ctx, &regions, false)
	return regions, diagnostics
}

func scopeFromModel(ctx context.Context, model awsAccountModel) (AWSAccountScope, diag.Diagnostics) {
	regions, diagnostics := regionsFromModel(ctx, model)
	return AWSAccountScope{
		AccountID:  model.AccountID.ValueString(),
		IAMRole:    model.IAMRole.ValueString(),
		Regions:    regions,
		STSRegion:  model.STSRegion.ValueString(),
		ReadOnly:   model.ReadOnly.ValueBool(),
		ExternalID: model.ExternalID.ValueString(),
	}, diagnostics
}

func validatePlannedScope(model awsAccountModel, diagnostics *diag.Diagnostics) {
	if !model.ReadOnly.ValueBool() {
		diagnostics.AddError("Write mode not supported", "readonly must be true")
	}
	if model.AccountID.IsNull() || model.AccountID.IsUnknown() || model.AccountID.ValueString() == "" {
		diagnostics.AddError("Missing AWS account ID", "account_id is required")
	}
	if model.ExternalID.IsNull() || model.ExternalID.IsUnknown() || model.ExternalID.ValueString() == "" {
		diagnostics.AddError("Missing client-generated External ID", "external_id is required")
	}
	if model.STSRegion.IsNull() || model.STSRegion.IsUnknown() || model.STSRegion.ValueString() == "" {
		diagnostics.AddError("Missing STS region", "sts_region must resolve to a non-empty AWS region")
	}
}

func applyScopeToModel(model *awsAccountModel, scope AWSAccountScope) {
	if scope.AccountID != "" {
		model.AccountID = types.StringValue(scope.AccountID)
	}
	if scope.ExternalID != "" {
		model.ExternalID = types.StringValue(scope.ExternalID)
	}
	if scope.Status != "" {
		model.Status = types.StringValue(scope.Status)
	}
	model.ReadOnly = types.BoolValue(scope.ReadOnly)
}

func (r *awsAccountResource) upsertAndConnect(ctx context.Context, model *awsAccountModel, diagnostics *diag.Diagnostics) {
	validatePlannedScope(*model, diagnostics)
	if diagnostics.HasError() {
		return
	}
	plannedScope, regionDiagnostics := scopeFromModel(ctx, *model)
	diagnostics.Append(regionDiagnostics...)
	if diagnostics.HasError() {
		return
	}

	if _, err := r.client.UpsertAWSAccount(ctx, plannedScope); err != nil {
		diagnostics.AddError("Failed to register AWS account scope", err.Error())
		return
	}
	connectedScope, err := r.client.ConnectAWSAccount(ctx, plannedScope.AccountID)
	if err != nil {
		diagnostics.AddError("Failed to connect AWS account", err.Error())
		return
	}
	if !connectedScope.Connected {
		diagnostics.AddError(
			"AWS account connection rejected",
			fmt.Sprintf("the platform did not accept the role: %s", connectedScope.StatusDetail),
		)
		return
	}
	applyScopeToModel(model, connectedScope)
}

func (r *awsAccountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan awsAccountModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.upsertAndConnect(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *awsAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state awsAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, found, err := r.client.GetAWSAccount(ctx, state.AccountID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read AWS account scope", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	applyScopeToModel(&state, scope)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *awsAccountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan awsAccountModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.upsertAndConnect(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *awsAccountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state awsAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteAWSAccount(ctx, state.AccountID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete AWS account scope", err.Error())
	}
}
