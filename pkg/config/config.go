package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	_ "strings"
	"time"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	Env  string
	Port int

	DatabaseURL string
	RedisURL    string

	JWTSecret     string
	JWTAccessTTL  time.Duration
	JWTRefreshTTL time.Duration

	OTPExpiry time.Duration

	// Africa's Talking (OTP / SMS)
	ATAPIKey   string
	ATUsername string
	ATSenderID string
	ATSandbox  bool

	// Firebase Cloud Messaging (push notifications)
	FirebaseCredJSON string

	// Cloudinary (file uploads — KYC docs, chat images)
	CloudinaryCloudName string
	CloudinaryAPIKey    string
	CloudinaryAPISecret string

	CORSOrigins []string
}

// MustLoad loads config from the environment and panics on any missing required value.
func MustLoad() *Config {
	return &Config{
		Env:  getEnv("APP_ENV", "development"),
		Port: getEnvInt("PORT", 8080),

		DatabaseURL: mustGetEnv("DATABASE_URL"),
		RedisURL:    mustGetEnv("REDIS_URL"),

		JWTSecret:     mustGetEnv("JWT_SECRET"),
		JWTAccessTTL:  getEnvDuration("JWT_ACCESS_TTL", 15*time.Minute),
		JWTRefreshTTL: getEnvDuration("JWT_REFRESH_TTL", 7*24*time.Hour),

		OTPExpiry: getEnvDuration("OTP_EXPIRY", 10*time.Minute),

		ATAPIKey:   mustGetEnv("AT_API_KEY"),
		ATUsername: mustGetEnv("AT_USERNAME"),
		ATSenderID: getEnv("AT_SENDER_ID", "sandbox"),
		ATSandbox:  getEnvBool("AT_SANDBOX", false),

		FirebaseCredJSON: mustGetEnv("FIREBASE_CRED_JSON"),

		CloudinaryCloudName: mustGetEnv("CLOUDINARY_CLOUD_NAME"),
		CloudinaryAPIKey:    mustGetEnv("CLOUDINARY_API_KEY"),
		CloudinaryAPISecret: mustGetEnv("CLOUDINARY_API_SECRET"),

		CORSOrigins: strings.Split(getEnv("CORS_ORIGINS", "http://localhost:3000"), ","),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustGetEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("required environment variable %q is not set", key))
	}
	return v
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
