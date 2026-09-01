package middleware

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/response"
)

// Fail records err on the context and stops the handler. The ErrorHandler
// middleware turns it into the JSON error envelope. Handlers use this instead of
// writing error responses themselves.
func Fail(c *gin.Context, err error) {
	_ = c.Error(err)
	c.Abort()
}

// ErrorHandler is the terminal middleware: after the handler runs, it converts
// the first recorded error into an *apperr.AppError envelope. 5xx causes are logged.
func ErrorHandler(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if len(c.Errors) == 0 {
			return
		}
		ae := apperr.From(c.Errors.Last().Err)
		if ae.HTTPStatus >= 500 {
			log.ErrorContext(c.Request.Context(), "request failed",
				slog.String("path", c.FullPath()),
				slog.String("method", c.Request.Method),
				slog.Int("code", ae.Code),
				slog.String("error", ae.Error()),
			)
		}
		if c.Writer.Written() {
			return
		}
		response.Error(c, ae)
	}
}

// Recovery converts a panic into a 500 error envelope instead of a dropped connection.
func Recovery(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				log.ErrorContext(c.Request.Context(), "panic recovered",
					slog.Any("panic", r), slog.String("path", c.Request.URL.Path))
				if !c.Writer.Written() {
					response.Error(c, apperr.ERRORS.UnhandledError)
				}
				c.Abort()
			}
		}()
		c.Next()
	}
}

// NotFound is the fallback for unmatched routes.
func NotFound() gin.HandlerFunc {
	return func(c *gin.Context) {
		response.Error(c, apperr.ERRORS.RouteNotFound)
	}
}
