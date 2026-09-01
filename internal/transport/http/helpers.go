package http

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

const (
	defaultPageLimit = 20
	maxPageLimit     = 100
)

// pageParams reads ?cursor= and ?limit= with sane bounds.
func pageParams(c *gin.Context) (cursor string, limit int64) {
	cursor = c.Query("cursor")
	limit = defaultPageLimit
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	return cursor, limit
}
