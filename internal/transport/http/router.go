// Package http wires the Gin engine: middleware order, route table, handler
// construction. It is the only package that imports gin outside of middleware.
package http

import (
	"context"
	"log/slog"
	nethttp "net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/sih26/ps188-backend/internal/config"
	"github.com/sih26/ps188-backend/internal/middleware"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/platform/jwt"
	"github.com/sih26/ps188-backend/internal/response"
	"github.com/sih26/ps188-backend/internal/service"
)

// Deps is everything the HTTP layer needs, assembled in main.
type Deps struct {
	Cfg        *config.Config
	Log        *slog.Logger
	Mongo      *mongo.Database
	JWT        *jwt.Manager
	Auth       *service.AuthService
	Users      *service.UserService
	Screenings *service.ScreeningService
}

// NewRouter builds the fully-wired gin.Engine.
func NewRouter(d Deps) *gin.Engine {
	if d.Cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(
		middleware.Recovery(d.Log),
		middleware.ErrorHandler(d.Log),
		middleware.RequestLog(d.Log),
		middleware.CORS(d.Cfg.CORSAllowOrigins),
		middleware.RateLimit(d.Cfg.RateLimitPerMin),
	)
	r.NoRoute(middleware.NotFound())

	r.GET("/health", healthHandler(d.Mongo))

	authH := &authHandler{auth: d.Auth}
	userH := &userHandler{users: d.Users}
	scrH := &screeningHandler{screenings: d.Screenings, maxUpload: d.Cfg.MaxUploadBytes}

	authed := middleware.Authenticate(d.JWT)
	admin := middleware.RequireRole(string(model.RoleAdmin))
	supervisor := middleware.RequireRole(string(model.RoleSupervisor))

	api := r.Group("/api")
	{
		auth := api.Group("/auth")
		auth.POST("/login", authH.login)
		auth.POST("/refresh-token", authH.refresh)

		users := api.Group("/users", authed)
		users.GET("/profile", userH.profile)
		users.POST("", admin, userH.create)
		users.GET("", admin, userH.list)

		scr := api.Group("/screenings", authed)
		scr.POST("", supervisor, scrH.submit)
		scr.GET("", scrH.list)
		scr.GET("/:id", scrH.get)
		scr.GET("/:id/image", scrH.image)
		scr.POST("/:id/decision", supervisor, scrH.decide)
	}
	return r
}

func healthHandler(db *mongo.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		mongoStatus := "ok"
		if err := db.Client().Ping(ctx, nil); err != nil {
			mongoStatus = "down"
			response.Success(c, nethttp.StatusServiceUnavailable,
				gin.H{"status": "degraded", "mongo": mongoStatus}, "health")
			return
		}
		response.Success(c, nethttp.StatusOK,
			gin.H{"status": "ok", "mongo": mongoStatus}, "health")
	}
}
