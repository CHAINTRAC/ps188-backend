package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/platform/jwt"
)

const principalKey = "principal"

// Authenticate requires a valid "Authorization: Bearer <token>" header and
// attaches the decoded principal to the context.
func Authenticate(jwtMgr *jwt.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			Fail(c, apperr.ERRORS.NoTokenProvided)
			return
		}
		td, aerr := jwtMgr.DecodeAccessToken(strings.TrimPrefix(h, "Bearer "))
		if aerr != nil {
			Fail(c, aerr)
			return
		}
		c.Set(principalKey, *td)
		c.Next()
	}
}

// RequireRole allows the request only if the principal's role is in roles.
// Must be chained after Authenticate.
func RequireRole(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}
	return func(c *gin.Context) {
		p, ok := Principal(c)
		if !ok {
			Fail(c, apperr.ERRORS.Unauthorized)
			return
		}
		if _, ok := allowed[p.Role]; !ok {
			Fail(c, apperr.ERRORS.Forbidden)
			return
		}
		c.Next()
	}
}

// Principal returns the authenticated principal set by Authenticate.
func Principal(c *gin.Context) (jwt.TokenData, bool) {
	v, ok := c.Get(principalKey)
	if !ok {
		return jwt.TokenData{}, false
	}
	td, ok := v.(jwt.TokenData)
	return td, ok
}
