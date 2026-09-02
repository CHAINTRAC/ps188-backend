package http

import (
	nethttp "net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/sih26/ps188-backend/internal/middleware"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/response"
	"github.com/sih26/ps188-backend/internal/service"
	"github.com/sih26/ps188-backend/internal/validate"
)

type blacklistHandler struct {
	blacklist *service.BlacklistService
}

func (h *blacklistHandler) create(c *gin.Context) {
	var in model.CreateBlacklistInput
	if err := validate.BindJSON(c, &in); err != nil {
		middleware.Fail(c, err)
		return
	}
	actor, _ := middleware.Principal(c)
	view, err := h.blacklist.Add(c.Request.Context(), actor.UserID, c.ClientIP(), in)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusCreated, view, "Blacklist entry added")
}

func (h *blacklistHandler) list(c *gin.Context) {
	cursor, limit := pageParams(c)
	filter := model.BlacklistFilter{Kind: model.BlacklistKind(c.Query("kind"))}
	if v := c.Query("active"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			filter.Active = &b
		}
	}
	page, err := h.blacklist.List(c.Request.Context(), filter, cursor, limit)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Paginated(c, page, "Blacklist entries")
}

func (h *blacklistHandler) get(c *gin.Context) {
	view, err := h.blacklist.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusOK, view, "Blacklist entry")
}

func (h *blacklistHandler) deactivate(c *gin.Context) {
	actor, _ := middleware.Principal(c)
	view, err := h.blacklist.Deactivate(c.Request.Context(), actor.UserID, c.ClientIP(), c.Param("id"))
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusOK, view, "Blacklist entry deactivated")
}

func (h *blacklistHandler) check(c *gin.Context) {
	probe := model.BlacklistProbe{
		DocNumber:   c.Query("doc_number"),
		Name:        c.Query("name"),
		DOB:         c.Query("dob"),
		Nationality: c.Query("nationality"),
	}
	res, err := h.blacklist.Check(c.Request.Context(), probe)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusOK, res, "Blacklist check")
}
