// cmd/api/api.go
// @title Executive API
// @version 1.0
// @description This is a REST API for the Executive eCommerce platform.
// @host localhost:8080
// @BasePath /api/v1
// @schemes http
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
package api

import (
	"context"
	"crypto/tls"
	"database/sql"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httplog/v3"
	"github.com/kimenyu/chukuagobackend/internal/logging"
	user "github.com/kimenyu/chukuagobackend/services/users"
	"github.com/kimenyu/chukuagobackend/types"

	httpSwagger "github.com/swaggo/http-swagger"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"

	"github.com/google/uuid"
)

type APIServer struct {
	addr string
	db   *sql.DB
}

func NewAPIServer(addr string, db *sql.DB) *APIServer {
	return &APIServer{
		addr: addr,
		db:   db,
	}
}

func ClientKey(r *http.Request) string {
	if uid := types.UserIDFromContext(r.Context()); uid != uuid.Nil {
		return "uid:" + uid.String()
	}
	return "ip:" + RealIP(r)
}

func RealIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xr := r.Header.Get("X-Real-IP"); xr != "" {
		return xr
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func (s *APIServer) Run() error {
	logging.Init(logging.Config{
		AppName: "executive-api",
		Version: os.Getenv("APP_VERSION"),
		Env:     os.Getenv("APP_ENV"),
		Level:   os.Getenv("LOG_LEVEL"),
		Compact: true,
	})

	redisAddr := os.Getenv("REDIS_ADDR")
	redisPassword := os.Getenv("REDIS_PASSWORD")

	// Upstash requires TLS — single clean declaration
	rdb := redis.NewClient(&redis.Options{
		Addr:         redisAddr,
		Password:     redisPassword,
		DB:           0,
		DialTimeout:  10 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	})

	// Test Redis connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Printf("WARNING: Redis connection failed: %v", err)
		log.Printf("Rate limiting will not work properly")
	} else {
		log.Printf("Redis connected successfully at %s", redisAddr)
	}

	limiter := redis_rate.NewLimiter(rdb)

	router := chi.NewRouter()

	// Core middlewares
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)

	// Structured request logger
	router.Use(logging.RequestLogger(&httplog.Options{
		Level:           slog.LevelInfo,
		Schema:          httplog.SchemaECS,
		RecoverPanics:   true,
		Skip:            func(_ *http.Request, status int) bool { return status == 404 || status == 405 },
		LogRequestBody:  func(r *http.Request) bool { return r.Header.Get("Debug") == "reveal-body-logs" },
		LogResponseBody: func(r *http.Request) bool { return r.Header.Get("Debug") == "reveal-body-logs" },
	}))

	// Attach per-request log attrs
	router.Use(func(next http.Handler) http.Handler {
		return logging.AddAttrs(next)
	})

	// Global distributed rate limit (per client)
	router.Use(logging.RateLimitMiddleware(limiter, redis_rate.PerMinute(300), ClientKey))

	// Health check
	router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Test route
	router.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message":"api is working well"}`))
	})

	// Swagger
	router.Get("/swagger/*", httpSwagger.WrapHandler)

	router.Route("/api/v1", func(r chi.Router) {
		userStore := user.NewStore(s.db)
		userHandler := user.NewHandler(userStore)

		// Add user_id attr to logs when authenticated
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, rr *http.Request) {
				if uid := types.UserIDFromContext(rr.Context()); uid != uuid.Nil {
					next = logging.AddAttrs(next, slog.String("user_id", uid.String()))
				}
				next.ServeHTTP(w, rr)
			})
		})

		userHandler.RegisterRoutes(r)
	})

	log.Printf("Server listening on %s", s.addr)
	return http.ListenAndServe(s.addr, router)
}
