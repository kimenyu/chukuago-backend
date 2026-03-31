package logging

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/httplog/v3"
	"github.com/go-redis/redis_rate/v10"
)

// RequestLogger returns a chi middleware that logs requests using the global logger.
func RequestLogger(opts *httplog.Options) func(http.Handler) http.Handler {
	l := Logger()
	if opts == nil {
		opts = &httplog.Options{
			Level:           slog.LevelInfo,
			Schema:          httplog.SchemaECS,
			RecoverPanics:   true,
			Skip:            func(_ *http.Request, status int) bool { return status == 404 || status == 405 },
			LogRequestBody:  isDebugHeaderSet,
			LogResponseBody: isDebugHeaderSet,
		}
	}
	return httplog.RequestLogger(l, opts)
}

func isDebugHeaderSet(r *http.Request) bool {
	return r.Header.Get("Debug") == "reveal-body-logs"
}

// RateLimitMiddleware logs throttles (429) and wraps a redis-rate limiter.
// keyFn generates a stable key (ip:..., uid:...), limit defines the allowance.
func RateLimitMiddleware(
	lim *redis_rate.Limiter,
	limit redis_rate.Limit,
	keyFn func(*http.Request) string,
) func(http.Handler) http.Handler {
	logger := Logger()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFn(r)
			res, err := lim.Allow(r.Context(), key, limit)
			if err != nil {
				logger.Error("rate_limit_error",
					slog.String("key", key),
					slog.String("err", err.Error()),
				)
				http.Error(w, "rate limiter error", http.StatusInternalServerError)
				return
			}
			if res.Allowed == 0 {
				logger.Warn("rate_limited",
					slog.String("key", key),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Duration("retry_after", res.RetryAfter),
				)
				w.Header().Set("Retry-After", strconv.Itoa(int(res.RetryAfter/time.Second)))
				http.Error(w, "too many requests", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func AddAttrs(next http.Handler, attrs ...slog.Attr) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httplog.SetAttrs(r.Context(), attrs...)
		next.ServeHTTP(w, r)
	})
}
