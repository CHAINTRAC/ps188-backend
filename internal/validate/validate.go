// Package validate centralises request-body binding so every handler reports a
// malformed or invalid body the same way.
package validate

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"github.com/sih26/ps188-backend/internal/apperr"
)

// BindJSON binds and validates the JSON body into dst (which must carry gin
// `binding:"..."` tags). It returns InvalidRequestBody for malformed JSON and
// ValidationError for tag violations — both as *apperr.AppError.
func BindJSON(c *gin.Context, dst any) error {
	if err := c.ShouldBindJSON(dst); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			return apperr.ERRORS.ValidationError.Wrap(err)
		}
		return apperr.ERRORS.InvalidRequestBody.Wrap(err)
	}
	return nil
}
