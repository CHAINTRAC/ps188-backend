package http

import (
	nethttp "net/http"

	"github.com/gin-gonic/gin"

	"github.com/sih26/ps188-backend/internal/middleware"
	"github.com/sih26/ps188-backend/internal/response"
	"github.com/sih26/ps188-backend/internal/service"
	"github.com/sih26/ps188-backend/internal/validate"
)

type authHandler struct {
	auth *service.AuthService
}

type loginBody struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type refreshBody struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

func (h *authHandler) login(c *gin.Context) {
	var body loginBody
	if err := validate.BindJSON(c, &body); err != nil {
		middleware.Fail(c, err)
		return
	}
	res, err := h.auth.Login(c.Request.Context(), body.Username, body.Password)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusOK, res, "Login successful")
}

func (h *authHandler) refresh(c *gin.Context) {
	var body refreshBody
	if err := validate.BindJSON(c, &body); err != nil {
		middleware.Fail(c, err)
		return
	}
	token, err := h.auth.Refresh(c.Request.Context(), body.RefreshToken)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusOK, gin.H{"access_token": token}, "Token refreshed")
}
