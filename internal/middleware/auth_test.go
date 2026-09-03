package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/platform/jwt"
)

func ctxWithPrincipal(td jwt.TokenData) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(principalKey, td)
	return c
}

func TestScopeToActor(t *testing.T) {
	t.Run("verifier is scoped to own officer_id", func(t *testing.T) {
		c := ctxWithPrincipal(jwt.TokenData{UserID: "u1", Role: string(model.RoleVerifier), Region: "north"})
		f := model.ScreeningFilter{Region: "south", OfficerID: "someone-else"}
		ScopeToActor(c, &f)
		if f.OfficerID != "u1" || f.Region != "" {
			t.Fatalf("verifier scope = %+v", f)
		}
	})

	t.Run("admin is scoped to own region", func(t *testing.T) {
		c := ctxWithPrincipal(jwt.TokenData{UserID: "a1", Role: string(model.RoleAdmin), Region: "north"})
		f := model.ScreeningFilter{OfficerID: "spoof", Region: "south"}
		ScopeToActor(c, &f)
		if f.Region != "north" || f.OfficerID != "" {
			t.Fatalf("admin scope = %+v", f)
		}
	})

	t.Run("super admin is unscoped", func(t *testing.T) {
		c := ctxWithPrincipal(jwt.TokenData{UserID: "s1", Role: string(model.RoleSuperAdmin)})
		f := model.ScreeningFilter{Verdict: model.VerdictFake}
		ScopeToActor(c, &f)
		if f.Region != "" || f.OfficerID != "" || f.Verdict != model.VerdictFake {
			t.Fatalf("superadmin scope = %+v", f)
		}
	})
}

func TestRegionScope(t *testing.T) {
	if got := RegionScope(jwt.TokenData{Role: string(model.RoleSuperAdmin), Region: "north"}); got != "" {
		t.Fatalf("superadmin RegionScope = %q, want empty", got)
	}
	if got := RegionScope(jwt.TokenData{Role: string(model.RoleAdmin), Region: "north"}); got != "north" {
		t.Fatalf("admin RegionScope = %q", got)
	}
}
