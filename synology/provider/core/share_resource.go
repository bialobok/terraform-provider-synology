package core

import (
	"context"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/synology-community/go-synology"
	"github.com/synology-community/go-synology/pkg/api/core"
)

// ShareResourceModel is the Terraform model for synology_core_share.
type ShareResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	VolPath     types.String `tfsdk:"vol_path"`
	Description types.String `tfsdk:"description"`
	UUID        types.String `tfsdk:"uuid"`
}

var (
	_ resource.Resource                = &ShareResource{}
	_ resource.ResourceWithConfigure   = &ShareResource{}
	_ resource.ResourceWithImportState = &ShareResource{}
)

func NewShareResource() resource.Resource {
	return &ShareResource{}
}

type ShareResource struct {
	client core.Api
}

// Metadata implements resource.Resource.
func (r *ShareResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = buildName(req.ProviderTypeName, "share")
}

// Schema implements resource.Resource.
func (r *ShareResource) Schema(
	_ context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A DSM shared folder.\n\n" +
			"~> **Destroying this resource deletes the shared folder and every file in it.** " +
			"Changing `name` or `vol_path` replaces it, which also deletes its files. " +
			"Consider `lifecycle { prevent_destroy = true }`.\n\n" +
			"Permissions are not managed here. `description` can only be set when the share " +
			"is created: DSM's share API in go-synology has no update call.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The share name (same as `name`).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The shared folder's name. Changing it replaces the share " +
					"(deleting its files).",
				Required: true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 32),
					stringvalidator.RegexMatches(
						regexp.MustCompile(`^[^/\\:*?"<>|=+;,\[\]]+$`),
						"Share names can't contain / \\ : * ? \" < > | = + ; , [ ]",
					),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"vol_path": schema.StringAttribute{
				MarkdownDescription: "The volume to create it on. Defaults to `/volume1`. " +
					"Changing it replaces the share (deleting its files).",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("/volume1"),
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						regexp.MustCompile(`^/volume[0-9]+$`),
						"must look like /volume1",
					),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "A description of the share. Set at create time only.",
				Optional:            true,
			},
			"uuid": schema.StringAttribute{
				MarkdownDescription: "DSM's id for the share.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

// Create implements resource.Resource.
func (r *ShareResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var plan ShareResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.ShareCreate(ctx, core.ShareInfo{
		Name:    plan.Name.ValueString(),
		VolPath: plan.VolPath.ValueString(),
		Desc:    plan.Description.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create shared folder", err.Error())
		return
	}

	share, err := r.findShare(ctx, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to list shared folders", err.Error())
		return
	}
	if share == nil {
		resp.Diagnostics.AddError(
			"Shared folder not found after create",
			fmt.Sprintf("DSM accepted share %q but doesn't list it.", plan.Name.ValueString()),
		)
		return
	}

	plan.ID = plan.Name
	plan.UUID = types.StringValue(share.UUID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read implements resource.Resource.
func (r *ShareResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var state ShareResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	if name == "" {
		name = state.ID.ValueString()
	}
	share, err := r.findShare(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list shared folders", err.Error())
		return
	}
	if share == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = types.StringValue(share.Name)
	state.Name = types.StringValue(share.Name)
	state.UUID = types.StringValue(share.UUID)
	if share.VolPath != "" {
		state.VolPath = types.StringValue(share.VolPath)
	}
	if share.Desc != "" || !state.Description.IsNull() {
		state.Description = types.StringValue(share.Desc)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update implements resource.Resource. Only description can reach here
// (name and vol_path replace), and there's no API call to change it.
func (r *ShareResource) Update(
	_ context.Context,
	_ resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	resp.Diagnostics.AddAttributeError(
		path.Root("description"),
		"Can't change a shared folder's description",
		"go-synology has no share update call yet. Change the description in DSM "+
			"(Control Panel > Shared Folder > Edit), then match it here.",
	)
}

// Delete implements resource.Resource. It deletes the share and its files.
func (r *ShareResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var state ShareResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.ShareDelete(ctx, state.Name.ValueString()); err != nil {
		if share, lerr := r.findShare(ctx, state.Name.ValueString()); lerr == nil && share == nil {
			return
		}
		resp.Diagnostics.AddError("Failed to delete shared folder", err.Error())
	}
}

// ImportState imports a share by name, for example
// `terraform import synology_core_share.x mininuke-backups`.
func (r *ShareResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}

// Configure implements resource.ResourceWithConfigure.
func (r *ShareResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(synology.Api)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf(
				"Expected synology.Api, got: %T. "+
					"Please report this issue to the provider developers.",
				req.ProviderData,
			),
		)
		return
	}
	r.client = client.CoreAPI()
}

func (r *ShareResource) findShare(ctx context.Context, name string) (*core.Share, error) {
	list, err := r.client.ShareList(ctx)
	if err != nil {
		return nil, err
	}
	return findShareByName(list.Shares, name), nil
}

func findShareByName(shares []core.Share, name string) *core.Share {
	for i := range shares {
		if shares[i].Name == name {
			return &shares[i]
		}
	}
	return nil
}
