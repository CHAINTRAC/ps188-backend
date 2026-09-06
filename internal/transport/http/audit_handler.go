package http

import (
	"github.com/gin-gonic/gin"

	"github.com/sih26/ps188-backend/internal/middleware"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/response"
	"github.com/sih26/ps188-backend/internal/service"
)

type auditHandler struct {
	audit *service.AuditService
}

// list has no RequireRole gate — ScopeAuditToActor scopes per-role instead.
func (h *auditHandler) list(c *gin.Context) {
	cursor, limit := pageParams(c)
	filter := model.AuditFilter{
		Action: c.Query("action"),
		Region: c.Query("region"),
	}
	middleware.ScopeAuditToActor(c, &filter)
	if filter.Deny {
		response.Paginated(c, response.Page[model.AuditLogView]{Data: []model.AuditLogView{}}, "Audit logs")
		return
	}

	page, err := h.audit.List(c.Request.Context(), filter, cursor, limit)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Paginated(c, page, "Audit logs")
}
