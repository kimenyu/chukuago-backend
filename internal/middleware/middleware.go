package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/chukuago/api/internal/auth"
	"github.com/chukuago/api/pkg/response"
	pkgtypes "github.com/chukuago/api/pkg/types"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Authenticate validates the Bearer JWT and injects the user ID into the context.
func Authenticate(tokenSvc *auth.TokenService, log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				response.Unauthorized(w, "missing authorization header")
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				response.Unauthorized(w, "authorization header must be: Bearer <token>")
				return
			}

			claims, err := tokenSvc.Validate(parts[1])
			if err != nil {
				response.Unauthorized(w, "token is invalid or expired")
				return
			}

			ctx := context.WithValue(r.Context(), pkgtypes.UserKey, claims.UserID)
			ctx = context.WithValue(ctx, roleKey, claims.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type contextRoleKey string

const roleKey contextRoleKey = "role"

// RequireRole returns a middleware that enforces one of the allowed roles.
// Usage: r.Use(middleware.RequireRole("admin")) or RequireRole("client", "admin")
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, _ := r.Context().Value(roleKey).(string)
			if _, ok := allowed[role]; !ok {
				response.Forbidden(w, "you do not have permission to access this resource")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimiter provides per-IP and per-user rate limiting backed by Redis.
type RateLimiter struct {
	redis *redis.Client
}

func NewRateLimiter(redis *redis.Client) *RateLimiter {
	return &RateLimiter{redis: redis}
}

// ByIP limits requests by the client's IP address.
func (rl *RateLimiter) ByIP(maxReqs int, window time.Duration) func(http.Handler) http.Handler {
	return rl.limit("ip", maxReqs, window, func(r *http.Request) string {
		return r.RemoteAddr
	})
}

// ByUser limits requests by the authenticated user ID (falls back to IP).
func (rl *RateLimiter) ByUser(maxReqs int, window time.Duration) func(http.Handler) http.Handler {
	return rl.limit("user", maxReqs, window, func(r *http.Request) string {
		userID, err := pkgtypes.UserIDFromContext(r.Context())
		if err != nil {
			return r.RemoteAddr
		}
		return userID.String()
	})
}

func (rl *RateLimiter) limit(prefix string, maxReqs int, window time.Duration, keyFn func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := "ratelimit:" + prefix + ":" + keyFn(r)
			ctx := r.Context()

			count, err := rl.redis.Incr(ctx, key).Result()
			if err != nil {
				// Fail open — don't block traffic on Redis hiccup.
				next.ServeHTTP(w, r)
				return
			}

			if count == 1 {
				rl.redis.Expire(ctx, key, window) //nolint:errcheck
			}

			if int(count) > maxReqs {
				response.Error(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests, please slow down")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ZapLogger is a chi-compatible structured request logger using zap.
func ZapLogger(log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()

			defer func() {
				log.Info("request",
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
					zap.Int("status", ww.Status()),
					zap.Duration("latency", time.Since(start)),
					zap.String("request_id", middleware.GetReqID(r.Context())),
					zap.String("ip", r.RemoteAddr),
				)
			}()

			next.ServeHTTP(ww, r)
		})
	}
}
