package core

import (
	"context"
	"strings"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/synology-community/go-synology/pkg/api/core"
)

func userSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &fwresource.SchemaResponse{}
	NewUserResource().Schema(context.Background(), fwresource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema returned diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

// The password must never reach state: it has to be write-only and sensitive.
func TestUserSchema_PasswordIsWriteOnly(t *testing.T) {
	t.Parallel()
	attr, ok := userSchema(t).Attributes["password_wo"].(schema.StringAttribute)
	if !ok {
		t.Fatalf(`attribute "password_wo" is not a StringAttribute`)
	}
	if !attr.WriteOnly || !attr.Sensitive {
		t.Errorf(
			"password_wo WriteOnly=%v Sensitive=%v, want both true",
			attr.WriteOnly,
			attr.Sensitive,
		)
	}
	if attr.Computed {
		t.Errorf("password_wo must not be Computed")
	}
}

func TestUserSchema_ReplaceOnNameOnly(t *testing.T) {
	t.Parallel()
	s := userSchema(t)

	name, ok := s.Attributes["name"].(schema.StringAttribute)
	if !ok || !name.Required || len(name.PlanModifiers) == 0 {
		t.Errorf(`"name" should be a required string with a replace plan modifier`)
	}
	// Group changes happen in place; replacing a user would delete it.
	groups, ok := s.Attributes["groups"].(schema.SetAttribute)
	if !ok || len(groups.PlanModifiers) != 0 {
		t.Errorf(`"groups" should be a set without plan modifiers`)
	}
}

func TestGroupDiff(t *testing.T) {
	t.Parallel()
	join, leave := groupDiff(
		[]string{"users", "administrators", "old"},
		[]string{"users", "new", "administrators"},
	)
	if strings.Join(join, ",") != "new" || strings.Join(leave, ",") != "old" {
		t.Errorf("groupDiff: join=%v leave=%v, want [new] [old]", join, leave)
	}
	join, leave = groupDiff(nil, []string{"users"})
	if len(join) != 1 || len(leave) != 0 {
		t.Errorf("groupDiff from nothing: join=%v leave=%v", join, leave)
	}
}

func TestReadString(t *testing.T) {
	t.Parallel()
	if got := readString(types.StringNull(), ""); !got.IsNull() {
		t.Errorf("unset and empty in DSM should stay null, got %v", got)
	}
	if got := readString(types.StringValue("x"), ""); got.ValueString() != "" || got.IsNull() {
		t.Errorf("cleared in DSM should read as \"\", got %v", got)
	}
	if got := readString(types.StringNull(), "set in DSM"); got.ValueString() != "set in DSM" {
		t.Errorf("set in DSM should be read, got %v", got)
	}
}

func TestUserSchema_ServiceAccountDefaults(t *testing.T) {
	t.Parallel()
	s := userSchema(t)
	for _, tt := range []string{"cannot_change_password", "password_never_expire"} {
		t.Run(tt, func(t *testing.T) {
			t.Parallel()
			attr, ok := s.Attributes[tt].(schema.BoolAttribute)
			if !ok {
				t.Fatalf("attribute %q is not a BoolAttribute", tt)
			}
			if attr.Default == nil {
				t.Fatalf("attribute %q has no default", tt)
			}
		})
	}
}

func TestFindUserByName(t *testing.T) {
	t.Parallel()
	users := []core.User{{ID: 1026, Name: "admin2"}, {ID: 1027, Name: "mininuke-backup"}}

	tests := []struct {
		name string
		want int // uid, 0 for not found
	}{
		{"mininuke-backup", 1027},
		{"admin2", 1026},
		{"missing", 0},
		{"", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			have := findUserByName(users, tt.name)
			switch {
			case tt.want == 0 && have != nil:
				t.Errorf("findUserByName(%q) = %v, want nil", tt.name, have)
			case tt.want != 0 && (have == nil || have.ID != tt.want):
				t.Errorf("findUserByName(%q) = %v, want uid %d", tt.name, have, tt.want)
			}
		})
	}
}
