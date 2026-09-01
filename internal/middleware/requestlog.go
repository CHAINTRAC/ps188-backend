package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestLog emits one structured line per request after it completes.
func RequestLog(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		attrs := []any{
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", c.Writer.Status()),
			slog.Duration("took", time.Since(start)),
			slog.String("ip", c.ClientIP()),
		}
		if p, ok := Principal(c); ok {
			attrs = append(attrs, slog.String("user", p.Username))
		}
		log.LogAttrs(c.Request.Context(), slog.LevelInfo, "request", toAttrs(attrs)...)
	}
}

func toAttrs(in []any) []slog.Attr {
	out := make([]slog.Attr, 0, len(in))
	for _, a := range in {
		if at, ok := a.(slog.Attr); ok {
			out = append(out, at)
		}
	}
	return out
}
