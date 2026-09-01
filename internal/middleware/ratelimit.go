package middleware

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/sih26/ps188-backend/internal/apperr"
)

// RateLimit is a simple per-client-IP token-bucket limiter. perMinute requests
// are allowed with a small burst; excess requests get 429 via the error envelope.
func RateLimit(perMinute int) gin.HandlerFunc {
	if perMinute <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	limit := rate.Limit(float64(perMinute) / 60.0)
	burst := perMinute / 6
	if burst < 1 {
		burst = 1
	}

	var mu sync.Mutex
	clients := make(map[string]*clientLimiter)

	// Evict idle clients every few minutes so the map cannot grow unbounded.
	go func() {
		for range time.Tick(3 * time.Minute) {
			mu.Lock()
			for ip, cl := range clients {
				if time.Since(cl.seen) > 5*time.Minute {
					delete(clients, ip)
				}
			}
			mu.Unlock()
		}
	}()

	return func(c *gin.Context) {
		ip := c.ClientIP()
		mu.Lock()
		cl, ok := clients[ip]
		if !ok {
			cl = &clientLimiter{lim: rate.NewLimiter(limit, burst)}
			clients[ip] = cl
		}
		cl.seen = time.Now()
		allowed := cl.lim.Allow()
		mu.Unlock()

		if !allowed {
			Fail(c, apperr.ERRORS.TooManyRequests)
			return
		}
		c.Next()
	}
}

type clientLimiter struct {
	lim  *rate.Limiter
	seen time.Time
}
