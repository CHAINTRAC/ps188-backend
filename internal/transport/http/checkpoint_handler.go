package http

import (
	nethttp "net/http"

	"github.com/gin-gonic/gin"

	"github.com/sih26/ps188-backend/internal/middleware"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/response"
	"github.com/sih26/ps188-backend/internal/service"
	"github.com/sih26/ps188-backend/internal/validate"
)

type checkpointHandler struct {
	checkpoints *service.CheckpointService
}

func (h *checkpointHandler) create(c *gin.Context) {
	var in model.CreateCheckpointInput
	if err := validate.BindJSON(c, &in); err != nil {
		middleware.Fail(c, err)
		return
	}
	actor, _ := middleware.Principal(c)
	view, err := h.checkpoints.Create(c.Request.Context(), actor.UserID, c.ClientIP(), in)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusCreated, view, "Checkpoint created")
}

func (h *checkpointHandler) list(c *gin.Context) {
	cursor, limit := pageParams(c)
	actor, _ := middleware.Principal(c)
	// Admin sees only their own region; super admin sees all (optionally filtered).
	filter := model.CheckpointFilter{Region: middleware.RegionScope(actor)}
	if filter.Region == "" {
		filter.Region = c.Query("region")
	}
	if v := c.Query("admin_id"); v != "" {
		filter.AdminID = v
	}
	page, err := h.checkpoints.List(c.Request.Context(), filter, cursor, limit)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Paginated(c, page, "Checkpoints")
}

func (h *checkpointHandler) get(c *gin.Context) {
	view, err := h.checkpoints.Get(c.Request.Context(), c.Param("code"))
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusOK, view, "Checkpoint")
}

func (h *checkpointHandler) update(c *gin.Context) {
	var in model.UpdateCheckpointInput
	if err := validate.BindJSON(c, &in); err != nil {
		middleware.Fail(c, err)
		return
	}
	actor, _ := middleware.Principal(c)
	view, err := h.checkpoints.Update(c.Request.Context(), actor.UserID, c.ClientIP(), c.Param("code"), in)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusOK, view, "Checkpoint updated")
}
