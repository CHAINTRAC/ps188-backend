// Package config is the single place the process reads its environment.
// Nothing else in the codebase touches os.Getenv — everything imports Config.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds every runtime setting. Populate it once via Load() in main.
type Config struct {
	Env  string // development | production | test
	Port string

	MongoURI string
	MongoDB  string

	JWTSecret        string
	JWTAccessTTL     time.Duration
	JWTRefreshSecret string
	JWTRefreshTTL    time.Duration
	CORSAllowOrigins []string
	MaxUploadBytes   int64
	RateLimitPerMin  int

	// First-run bootstrap admin (seeded only when the users collection is empty).
	AdminUsername string
	AdminPassword string
	AdminEmail    string

	// External Python/FastAPI screening model.
	ScreeningEngine     string // http | mock
	ScreeningServiceURL string
	ScreeningAPIKey     string
	ScreeningTimeout    time.Duration
}

// Load reads .env.<ENV>.local (if present) then the process environment.
// Missing required values return an error rather than a silent zero value.
func Load() (*Config, error) {
	env := getenv("APP_ENV", "development")

	// Best-effort: a missing env file is fine (Docker injects vars directly).
	_ = godotenv.Load(fmt.Sprintf(".env.%s.local", env))

	c := &Config{
		Env:  env,
		Port: getenv("PORT", "8080"),

		MongoURI: getenv("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:  getenv("MONGO_DB", "ps188"),

		JWTSecret:        os.Getenv("JWT_SECRET"),
		JWTAccessTTL:     getdur("JWT_ACCESS_TTL", time.Hour),
		JWTRefreshSecret: getenv("JWT_REFRESH_SECRET", os.Getenv("JWT_SECRET")),
		JWTRefreshTTL:    getdur("JWT_REFRESH_TTL", 30*24*time.Hour),
		CORSAllowOrigins: splitList(getenv("CORS_ALLOW_ORIGINS", "*")),
		MaxUploadBytes:   getint64("MAX_UPLOAD_BYTES", 10<<20), // 10 MiB
		RateLimitPerMin:  int(getint64("RATE_LIMIT_PER_MIN", 300)),

		AdminUsername: getenv("ADMIN_USERNAME", "admin"),
		AdminPassword: getenv("ADMIN_PASSWORD", "admin12345"),
		AdminEmail:    getenv("ADMIN_EMAIL", "admin@ps188.local"),

		ScreeningEngine:     getenv("SCREENING_ENGINE", "http"),
		ScreeningServiceURL: os.Getenv("SCREENING_SERVICE_URL"),
		ScreeningAPIKey:     os.Getenv("SCREENING_SERVICE_API_KEY"),
		ScreeningTimeout:    getdur("SCREENING_SERVICE_TIMEOUT", 30*time.Second),
	}

	if c.JWTSecret == "" {
		return nil, fmt.Errorf("config: JWT_SECRET is required")
	}
	if c.Env != "test" && c.ScreeningEngine == "http" && c.ScreeningServiceURL == "" {
		return nil, fmt.Errorf("config: SCREENING_SERVICE_URL is required when SCREENING_ENGINE=http")
	}
	return c, nil
}

func (c *Config) IsProduction() bool { return c.Env == "production" }

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getint64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func getdur(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func splitList(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
