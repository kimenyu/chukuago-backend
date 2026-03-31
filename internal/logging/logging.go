package logging

import (
	"log/slog"
	"os"
	"strings"

	"github.com/go-chi/httplog/v3"
)

type Config struct {
	AppName string
	Version string
	Env     string
	Level   string
	Compact bool
}

var base *slog.Logger

func Init(cfg Config) *slog.Logger {
	level := parseLevel(cfg.Level)

	compact := cfg.Compact ||
		strings.EqualFold(cfg.Env, "development") ||
		strings.EqualFold(cfg.Env, "dev") ||
		strings.EqualFold(cfg.Env, "local")

	replace := httplog.SchemaECS.Concise(compact).ReplaceAttr

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: replace,
	})

	base = slog.New(handler).With(
		slog.String("app", cfg.AppName),
		slog.String("version", cfg.Version),
		slog.String("env", cfg.Env),
	)

	return base
}

func Logger() *slog.Logger {
	if base == nil {
		// Sane fallback if Init wasn't called
		base = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return base
}

func parseLevel(lvl string) slog.Leveler {
	switch strings.ToLower(lvl) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
