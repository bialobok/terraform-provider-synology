package core

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/synology-community/go-synology"
	"github.com/synology-community/go-synology/pkg/api"
	"github.com/synology-community/go-synology/pkg/api/core"
	"github.com/synology-community/go-synology/pkg/api/core/methods"
)

// UserResourceModel is the Terraform model for synology_core_user.
//
// The password is write-only: it is read from the configuration when it is
// needed and never stored in state or plan. Bump password_wo_version to set
// it again.
type UserResourceModel struct {
	ID                   types.String `tfsdk:"id"`
	Name                 types.String `tfsdk:"name"`
	PasswordWO           types.String `tfsdk:"password_wo"`
	PasswordWOVersion    types.Int64  `tfsdk:"password_wo_version"`
	Description          types.String `tfsdk:"description"`
	Email                types.String `tfsdk:"email"`
	Groups               types.Set    `tfsdk:"groups"`
	CannotChangePassword types.Bool   `tfsdk:"cannot_change_password"`
	PasswordNeverExpire  types.Bool   `tfsdk:"password_never_expire"`
}

var (
	_ resource.Resource                   = &UserResource{}
	_ resource.ResourceWithConfigure      = &UserResource{}
	_ resource.ResourceWithImportState    = &UserResource{}
	_ resource.ResourceWithValidateConfig = &UserResource{}
)

func NewUserResource() resource.Resource {
	return &UserResource{}
}

type UserResource struct {
	client core.Api
	// raw is the base API client, for the user calls that need a password
	// confirmation token (see confirmed).
	raw api.Api
}

// DSM 7 refuses SYNO.Core.User create, set and delete with error 105 unless
// the request carries a SynoConfirmPWToken from SYNO.Core.User.PasswordConfirm
// (the "enter your password again" step in the UI). go-synology's request
// types don't have the field, so these wrap them and add it.
type userCreateConfirmed struct {
	core.UserCreateRequest
	SynoConfirmPWToken string `url:"SynoConfirmPWToken"`
}

type userModifyConfirmed struct {
	core.UserModifyRequest
	SynoConfirmPWToken string `url:"SynoConfirmPWToken"`
}

type userDeleteConfirmed struct {
	core.UserDeleteRequest
	SynoConfirmPWToken string `url:"SynoConfirmPWToken"`
}

// confirmToken asks DSM for a fresh password confirmation token for the
// logged-in admin.
func (r *UserResource) confirmToken(ctx context.Context) (string, error) {
	res, err := r.client.PasswordConfirm(ctx, r.raw.Password())
	if err != nil {
		return "", fmt.Errorf("password confirmation: %w", err)
	}
	if res == nil || res.SynoConfirmPWToken == "" {
		return "", fmt.Errorf("password confirmation returned no token")
	}
	return res.SynoConfirmPWToken, nil
}

func (r *UserResource) createUser(ctx context.Context, req core.UserCreateRequest) error {
	token, err := r.confirmToken(ctx)
	if err != nil {
		return err
	}
	_, err = api.Post[core.UserCreateResponse](r.raw, ctx, &userCreateConfirmed{
		UserCreateRequest:  req,
		SynoConfirmPWToken: token,
	}, methods.UserCreate)
	return err
}

func (r *UserResource) modifyUser(ctx context.Context, req core.UserModifyRequest) error {
	token, err := r.confirmToken(ctx)
	if err != nil {
		return err
	}
	_, err = api.Post[core.UserModifyResponse](r.raw, ctx, &userModifyConfirmed{
		UserModifyRequest:  req,
		SynoConfirmPWToken: token,
	}, methods.UserModify)
	return err
}

func (r *UserResource) deleteUser(ctx context.Context, name string) error {
	token, err := r.confirmToken(ctx)
	if err != nil {
		return err
	}
	return api.PostVoid(r.raw, ctx, &userDeleteConfirmed{
		UserDeleteRequest:  core.UserDeleteRequest{Name: name},
		SynoConfirmPWToken: token,
	}, methods.UserDelete)
}

// Metadata implements resource.Resource.
func (r *UserResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = buildName(req.ProviderTypeName, "user")
}

// Schema implements resource.Resource.
func (r *UserResource) Schema(
	_ context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A local DSM user account.\n\n" +
			"The password is a write-only argument (Terraform 1.11 or later): it is sent to DSM " +
			"but never stored in state or plan. To change it, set a new `password_wo` and " +
			"increase `password_wo_version`.\n\n" +
			"DSM's user list does not return group membership, description or email, so " +
			"changes made to those outside Terraform are not detected.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The user name (same as `name`).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The user name. Changing it replaces the user.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 64),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"password_wo": schema.StringAttribute{
				MarkdownDescription: "The user's password. Write-only: never stored in state. " +
					"Required when the user is created.",
				Optional:  true,
				Sensitive: true,
				WriteOnly: true,
			},
			"password_wo_version": schema.Int64Attribute{
				MarkdownDescription: "Increase to send `password_wo` to DSM again " +
					"(rotate the password).",
				Optional: true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "A description of the user.",
				Optional:            true,
			},
			"email": schema.StringAttribute{
				MarkdownDescription: "The user's email address.",
				Optional:            true,
			},
			"groups": schema.SetAttribute{
				MarkdownDescription: "Groups the user belongs to, for example `[\"users\"]`. " +
					"Changing it replaces the user, because DSM's modify call can't remove groups.",
				ElementType: types.StringType,
				Optional:    true,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.RequiresReplace(),
				},
			},
			"cannot_change_password": schema.BoolAttribute{
				MarkdownDescription: "Whether the user is prevented from changing their " +
					"own password. Defaults to `true`, which suits service accounts managed here.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"password_never_expire": schema.BoolAttribute{
				MarkdownDescription: "Whether the password is exempt from DSM's expiry policy. " +
					"Defaults to `true`.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
		},
	}
}

// ValidateConfig requires a password whenever a version is set, so a
// rotation can't silently send an empty password.
func (r *UserResource) ValidateConfig(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var data UserResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !data.PasswordWOVersion.IsNull() && !data.PasswordWOVersion.IsUnknown() &&
		data.PasswordWO.IsNull() {
		resp.Diagnostics.AddAttributeError(
			path.Root("password_wo"),
			"Missing password",
			"password_wo must be set when password_wo_version is set.",
		)
	}
}

// Create implements resource.Resource.
func (r *UserResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var plan UserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Write-only values are only in the config, never in the plan.
	var password types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("password_wo"), &password)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if password.IsNull() || password.IsUnknown() || password.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("password_wo"),
			"Missing password",
			"password_wo is required to create a user.",
		)
		return
	}

	groups := []string{}
	resp.Diagnostics.Append(plan.Groups.ElementsAs(ctx, &groups, true)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.createUser(ctx, core.UserCreateRequest{
		Name:              plan.Name.ValueString(),
		Password:          password.ValueString(),
		Description:       plan.Description.ValueString(),
		Email:             plan.Email.ValueString(),
		CannotChangePass:  plan.CannotChangePassword.ValueBool(),
		PasswdNeverExpire: plan.PasswordNeverExpire.ValueBool(),
		Groups:            groups,
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create user", err.Error())
		return
	}

	plan.ID = plan.Name
	plan.PasswordWO = types.StringNull()
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read implements resource.Resource.
func (r *UserResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var state UserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	if name == "" {
		name = state.ID.ValueString()
	}

	user, err := r.findUser(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list users", err.Error())
		return
	}
	if user == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = types.StringValue(user.Name)
	state.Name = types.StringValue(user.Name)
	// The list call only returns the name and uid; keep the rest as planned.
	if state.CannotChangePassword.IsNull() {
		state.CannotChangePassword = types.BoolValue(true)
	}
	if state.PasswordNeverExpire.IsNull() {
		state.PasswordNeverExpire = types.BoolValue(true)
	}
	state.PasswordWO = types.StringNull()
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update implements resource.Resource.
func (r *UserResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var plan, state UserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	modify := core.UserModifyRequest{
		Name:              state.Name.ValueString(),
		Description:       plan.Description.ValueString(),
		Email:             plan.Email.ValueString(),
		CannotChangePass:  plan.CannotChangePassword.ValueBool(),
		PasswdNeverExpire: plan.PasswordNeverExpire.ValueBool(),
	}

	if !plan.PasswordWOVersion.Equal(state.PasswordWOVersion) {
		var password types.String
		resp.Diagnostics.Append(
			req.Config.GetAttribute(ctx, path.Root("password_wo"), &password)...)
		if resp.Diagnostics.HasError() {
			return
		}
		if password.IsNull() || password.IsUnknown() || password.ValueString() == "" {
			resp.Diagnostics.AddAttributeError(
				path.Root("password_wo"),
				"Missing password",
				"password_wo_version changed, so password_wo must be set.",
			)
			return
		}
		modify.Password = password.ValueString()
	}

	if err := r.modifyUser(ctx, modify); err != nil {
		resp.Diagnostics.AddError("Failed to update user", err.Error())
		return
	}

	plan.ID = state.ID
	plan.PasswordWO = types.StringNull()
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete implements resource.Resource.
func (r *UserResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var state UserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.deleteUser(ctx, state.Name.ValueString())
	if err != nil {
		// Already gone counts as deleted.
		if user, lerr := r.findUser(ctx, state.Name.ValueString()); lerr == nil && user == nil {
			return
		}
		resp.Diagnostics.AddError("Failed to delete user", err.Error())
	}
}

// ImportState imports a user by name, for example
// `terraform import synology_core_user.x backup`.
func (r *UserResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}

// Configure implements resource.ResourceWithConfigure.
func (r *UserResource) Configure(
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
	r.raw = client
}

// findUser returns the user with this name, or nil if there is none.
func (r *UserResource) findUser(ctx context.Context, name string) (*core.User, error) {
	list, err := r.client.UserList(ctx)
	if err != nil {
		return nil, err
	}
	return findUserByName(list.Users, name), nil
}

func findUserByName(users []core.User, name string) *core.User {
	for i := range users {
		if users[i].Name == name {
			return &users[i]
		}
	}
	return nil
}
