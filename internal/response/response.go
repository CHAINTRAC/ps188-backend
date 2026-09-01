// Package response builds the two JSON envelopes every endpoint returns.
// Handlers never construct maps by hand — they call Success / Paginated, and
// let the error middleware call Error.
package response

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sih26/ps188-backend/internal/apperr"
)

// Envelope shapes:
//
//	success:    {"success":true,"message":"...","data":{...},"timestamp":"..."}
//	paginated:  {"success":true,"message":"...","data":[...],"page":{"has_next":true,"next_cursor":"..."},"timestamp":"..."}
//	error:      {"success":false,"error":{"code":40001,"message":"..."},"timestamp":"..."}

// PageInfo is the cursor-pagination footer. NextCursor is the hex ObjectID the
// client passes back as ?cursor= to fetch the following page.
type PageInfo struct {
	HasNext    bool   `json:"has_next"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// Page wraps a slice plus its PageInfo. Repositories return this; handlers pass
// it straight to Paginated.
type Page[T any] struct {
	Data []T
	Info PageInfo
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// Success writes a 200 (or the given status) success envelope.
func Success(c *gin.Context, status int, data any, message string) {
	if message == "" {
		message = "OK"
	}
	c.JSON(status, gin.H{
		"success":   true,
		"message":   message,
		"data":      data,
		"timestamp": now(),
	})
}

// Paginated writes a success envelope with a page footer.
func Paginated[T any](c *gin.Context, page Page[T], message string) {
	if message == "" {
		message = "OK"
	}
	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"message":   message,
		"data":      page.Data,
		"page":      page.Info,
		"timestamp": now(),
	})
}

// Error writes the error envelope for an *apperr.AppError.
func Error(c *gin.Context, e *apperr.AppError) {
	c.JSON(e.HTTPStatus, gin.H{
		"success": false,
		"error": gin.H{
			"code":    e.Code,
			"message": e.Message,
		},
		"timestamp": now(),
	})
}
