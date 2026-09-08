package http

import (
	nethttp "net/http"

	"github.com/gin-gonic/gin"

	"github.com/sih26/ps188-backend/internal/middleware"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/response"
	"github.com/sih26/ps188-backend/internal/service"
)

type analyticsHandler struct {
	analytics *service.AnalyticsService
}

// dashboardSummary is role-aware — the service reads the principal's role and
// region and scopes every rollup. No RequireRole gate: all three roles get a
// (differently shaped) summary.
func (h *analyticsHandler) dashboardSummary(c *gin.Context) {
	actor, _ := middleware.Principal(c)
	out, err := h.analytics.DashboardSummary(c.Request.Context(), actor)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusOK, out, "Dashboard summary")
}

// reports backs the admin Reports page. An admin sees their own region; a super
// admin sees the org, or one region via ?region=.
func (h *analyticsHandler) reports(c *gin.Context) {
	actor, _ := middleware.Principal(c)

	region := middleware.RegionScope(actor)
	if actor.Role == string(model.RoleAdmin) && region == "" {
		// misconfigured admin — fail closed rather than leak the whole org
		response.Success(c, nethttp.StatusOK, model.ReportsSummary{
			WeeklyVolume:        []model.DayVolume{},
			DocTypeBreakdown:    []model.DocTypeCount{},
			CheckpointBreakdown: []model.ActorActivity{},
		}, "Reports")
		return
	}
	if actor.Role == string(model.RoleSuperAdmin) {
		region = c.Query("region")
	}

	out, err := h.analytics.Reports(c.Request.Context(), region)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusOK, out, "Reports")
}
