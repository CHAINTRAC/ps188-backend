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

type userHandler struct {
	users *service.UserService
}

func (h *userHandler) create(c *gin.Context) {
	var in model.CreateUserInput
	if err := validate.BindJSON(c, &in); err != nil {
		middleware.Fail(c, err)
		return
	}
	actor, _ := middleware.Principal(c)
	view, err := h.users.Create(c.Request.Context(), actor.UserID, c.ClientIP(), in)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusCreated, view, "User created")
}

func (h *userHandler) list(c *gin.Context) {
	cursor, limit := pageParams(c)
	actor, _ := middleware.Principal(c)
	filter := model.UserFilter{
		Region: middleware.RegionScope(actor),
		Role:   model.Role(c.Query("role")),
		Status: model.UserStatus(c.Query("status")),
	}
	if filter.Region == "" {
		if actor.Role == string(model.RoleAdmin) {
			filter.Deny = true // misconfigured account — fail closed, not unscoped
		} else {
			filter.Region = c.Query("region")
		}
	}
	if filter.Deny {
		response.Paginated(c, response.Page[model.UserView]{Data: []model.UserView{}}, "Users")
		return
	}

	page, err := h.users.List(c.Request.Context(), filter, cursor, limit)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Paginated(c, page, "Users")
}

func (h *userHandler) update(c *gin.Context) {
	var in model.UpdateUserInput
	if err := validate.BindJSON(c, &in); err != nil {
		middleware.Fail(c, err)
		return
	}
	actor, _ := middleware.Principal(c)
	view, err := h.users.Update(c.Request.Context(), actor, c.ClientIP(), c.Param("id"), in)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusOK, view, "User updated")
}

func (h *userHandler) profile(c *gin.Context) {
	p, _ := middleware.Principal(c)
	view, err := h.users.Profile(c.Request.Context(), p.UserID)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusOK, view, "Profile")
}

type changePasswordBody struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required,min=8,max=128"`
}

func (h *userHandler) changePassword(c *gin.Context) {
	var body changePasswordBody
	if err := validate.BindJSON(c, &body); err != nil {
		middleware.Fail(c, err)
		return
	}
	p, _ := middleware.Principal(c)
	if err := h.users.ChangePassword(c.Request.Context(), p.UserID, c.ClientIP(), body.CurrentPassword, body.NewPassword); err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusOK, gin.H{"changed": true}, "Password changed")
}

func (h *userHandler) resetPassword(c *gin.Context) {
	actor, _ := middleware.Principal(c)
	temp, err := h.users.ResetPassword(c.Request.Context(), actor, c.ClientIP(), c.Param("id"))
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusOK, gin.H{"temp_password": temp}, "Password reset")
}
