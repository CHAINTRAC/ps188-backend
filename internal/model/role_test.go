package model_test

import (
	"testing"

	"github.com/sih26/ps188-backend/internal/model"
)

func TestRole_Valid(t *testing.T) {
	valid := []model.Role{model.RoleVerifier, model.RoleAdmin, model.RoleSuperAdmin}
	for _, r := range valid {
		if !r.Valid() {
			t.Errorf("%q should be valid", r)
		}
	}
	for _, r := range []model.Role{"", "officer", "supervisor", "root"} {
		if r.Valid() {
			t.Errorf("%q should be invalid", r)
		}
	}
}

func TestRole_AtLeastAdmin(t *testing.T) {
	if model.RoleVerifier.AtLeastAdmin() {
		t.Error("verifier is not admin-level")
	}
	if !model.RoleAdmin.AtLeastAdmin() || !model.RoleSuperAdmin.AtLeastAdmin() {
		t.Error("admin and superadmin are admin-level")
	}
}

func TestCheckpointStatus_Valid(t *testing.T) {
	if !model.CheckpointActive.Valid() || !model.CheckpointAttention.Valid() {
		t.Error("active/attention should be valid")
	}
	if model.CheckpointStatus("closed").Valid() {
		t.Error("closed should be invalid")
	}
}

func TestNormalizeCheckpointCode(t *testing.T) {
	if got := model.NormalizeCheckpointCode("  cp-04 "); got != "CP-04" {
		t.Errorf("got %q", got)
	}
}
