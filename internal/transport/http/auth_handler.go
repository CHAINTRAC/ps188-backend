package http

import (
	nethttp "net/http"

	"github.com/gin-gonic/gin"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/middleware"
	"github.com/sih26/ps188-backend/internal/response"
	"github.com/sih26/ps188-backend/internal/service"
	"github.com/sih26/ps188-backend/internal/validate"
)

type authHandler struct {
	auth *service.AuthService
}

// loginBody accepts any one of identifier / email / username — the UI sign-in
// form submits an email, older clients submit username.
type loginBody struct {
	Identifier string `json:"identifier" binding:"omitempty"`
	Email      string `json:"email" binding:"omitempty"`
	Username   string `json:"username" binding:"omitempty"`
	Password   string `json:"password" binding:"required"`
}

func (b loginBody) resolveIdentifier() string {
	switch {
	case b.Identifier != "":
		return b.Identifier
	case b.Email != "":
		return b.Email
	default:
		return b.Username
	}
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
	identifier := body.resolveIdentifier()
	if identifier == "" {
		middleware.Fail(c, apperr.ERRORS.InvalidCredentials)
		return
	}
	res, err := h.auth.Login(c.Request.Context(), identifier, body.Password, c.ClientIP())
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
