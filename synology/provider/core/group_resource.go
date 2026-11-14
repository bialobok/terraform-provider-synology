package core

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/synology-community/go-synology"
	"github.com/synology-community/go-synology/pkg/api/core"
)

// GroupResourceModel is the Terraform model for synology_core_group.
type GroupResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	GID         types.Int64  `tfsdk:"gid"`
}

var (
	_ resource.Resource                = &GroupResource{}
	_ resource.ResourceWithConfigure   = &GroupResource{}
	_ resource.ResourceWithImportState = &GroupResource{}
)

func NewGroupResource() resource.Resource {
	return &GroupResource{}
}

type GroupResource struct {
	client core.Api
}

// Metadata implements resource.Resource.
func (r *GroupResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = buildName(req.ProviderTypeName, "group")
}

// Schema implements resource.Resource.
func (r *GroupResource) Schema(
	_ context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A local DSM group. Add users to it with `synology_core_user`'s " +
			"`groups`.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The group name (same as `name`).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The group name. Changing it renames the group in place.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 64),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "A description of the group.",
				Optional:            true,
			},
			"gid": schema.Int64Attribute{
				MarkdownDescription: "DSM's numeric group id.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

// Create implements resource.Resource.
func (r *GroupResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var plan GroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.GroupCreate(ctx, core.GroupCreateRequest{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create group", err.Error())
		return
	}

	group, err := r.findGroup(ctx, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to list groups", err.Error())
		return
	}
	if group == nil {
		resp.Diagnostics.AddError(
			"Group not found after create",
			fmt.Sprintf("DSM accepted group %q but doesn't list it.", plan.Name.ValueString()),
		)
		return
	}

	plan.ID = plan.Name
	plan.GID = types.Int64Value(int64(group.ID))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read implements resource.Resource.
func (r *GroupResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var state GroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	if name == "" {
		name = state.ID.ValueString()
	}
	group, err := r.findGroup(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list groups", err.Error())
		return
	}
	if group == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = types.StringValue(group.Name)
	state.Name = types.StringValue(group.Name)
	state.GID = types.Int64Value(int64(group.ID))
	if group.Description != "" || !state.Description.IsNull() {
		state.Description = types.StringValue(group.Description)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update implements resource.Resource.
func (r *GroupResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var plan, state GroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	modify := core.GroupModifyRequest{
		Name:        state.Name.ValueString(),
		Description: plan.Description.ValueString(),
	}
	if plan.Name.ValueString() != state.Name.ValueString() {
		modify.NewName = plan.Name.ValueString()
	}
	if _, err := r.client.GroupModify(ctx, modify); err != nil {
		resp.Diagnostics.AddError("Failed to update group", err.Error())
		return
	}

	plan.ID = plan.Name
	plan.GID = state.GID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete implements resource.Resource.
func (r *GroupResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var state GroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.GroupDelete(ctx, core.GroupDeleteRequest{Name: state.Name.ValueString()})
	if err != nil {
		if group, lerr := r.findGroup(ctx, state.Name.ValueString()); lerr == nil && group == nil {
			return
		}
		resp.Diagnostics.AddError("Failed to delete group", err.Error())
	}
}

// ImportState imports a group by name, for example
// `terraform import synology_core_group.x backup-writers`.
func (r *GroupResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}

// Configure implements resource.ResourceWithConfigure.
func (r *GroupResource) Configure(
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

func (r *GroupResource) findGroup(ctx context.Context, name string) (*core.Group, error) {
	list, err := r.client.GroupList(ctx)
	if err != nil {
		return nil, err
	}
	return findGroupByName(list.Groups, name), nil
}

func findGroupByName(groups []core.Group, name string) *core.Group {
	for i := range groups {
		if groups[i].Name == name {
			return &groups[i]
		}
	}
	return nil
}
