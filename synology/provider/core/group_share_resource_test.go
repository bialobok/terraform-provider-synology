package core

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/synology-community/go-synology/pkg/api/core"
)

func resourceSchema(t *testing.T, r fwresource.Resource) schema.Schema {
	t.Helper()
	resp := &fwresource.SchemaResponse{}
	r.Schema(context.Background(), fwresource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema returned diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

// Renaming a group is an in-place update, not a replace.
func TestGroupSchema_NameUpdatesInPlace(t *testing.T) {
	t.Parallel()
	attr, ok := resourceSchema(t, NewGroupResource()).Attributes["name"].(schema.StringAttribute)
	if !ok || !attr.Required {
		t.Fatalf(`"name" should be a required string`)
	}
	if len(attr.PlanModifiers) != 0 {
		t.Errorf(`"name" has plan modifiers; a rename should not replace the group`)
	}
}

// Moving or renaming a share replaces it, so both must say so.
func TestShareSchema_ReplaceOnNameAndVolume(t *testing.T) {
	t.Parallel()
	s := resourceSchema(t, NewShareResource())
	for _, tt := range []string{"name", "vol_path"} {
		t.Run(tt, func(t *testing.T) {
			t.Parallel()
			attr, ok := s.Attributes[tt].(schema.StringAttribute)
			if !ok || len(attr.PlanModifiers) == 0 {
				t.Errorf("%q should be a string with a replace plan modifier", tt)
			}
		})
	}
}

func TestFindGroupAndShareByName(t *testing.T) {
	t.Parallel()
	groups := []core.Group{{ID: 100, Name: "users"}, {ID: 65536, Name: "backup-writers"}}
	shares := []core.Share{{Name: "photos", UUID: "a"}, {Name: "mininuke-backups", UUID: "b"}}

	tests := []struct {
		name      string
		wantGroup int
		wantShare string
	}{
		{"backup-writers", 65536, ""},
		{"mininuke-backups", 0, "b"},
		{"users", 100, ""},
		{"missing", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := findGroupByName(groups, tt.name)
			if (tt.wantGroup == 0) != (g == nil) || (g != nil && g.ID != tt.wantGroup) {
				t.Errorf("findGroupByName(%q) = %v, want gid %d", tt.name, g, tt.wantGroup)
			}
			s := findShareByName(shares, tt.name)
			if (tt.wantShare == "") != (s == nil) || (s != nil && s.UUID != tt.wantShare) {
				t.Errorf("findShareByName(%q) = %v, want uuid %q", tt.name, s, tt.wantShare)
			}
		})
	}
}
