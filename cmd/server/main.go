package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chukuago/api/internal/admin"
	"github.com/chukuago/api/internal/auth"
	"github.com/chukuago/api/internal/chat"
	"github.com/chukuago/api/internal/delivery"
	"github.com/chukuago/api/internal/disputes"
	"github.com/chukuago/api/internal/errands"
	"github.com/chukuago/api/internal/middleware"
	"github.com/chukuago/api/internal/notifications"
	"github.com/chukuago/api/internal/offers"
	"github.com/chukuago/api/internal/reviews"
	"github.com/chukuago/api/internal/runners"
	"github.com/chukuago/api/internal/users"
	"github.com/chukuago/api/pkg/config"
	"github.com/chukuago/api/pkg/database"
	rdb "github.com/chukuago/api/pkg/redis"
	"github.com/chukuago/api/pkg/storage"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	godotenv.Load()
	cfg := config.MustLoad()

	logger, err := buildLogger(cfg.Env)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync() //nolint:errcheck

	// Neon Postgres — pool is tuned for serverless in pkg/database.
	db, err := database.NewPostgres(cfg.DatabaseURL)
	if err != nil {
		logger.Fatal("failed to connect to postgres (Neon)", zap.Error(err))
	}
	defer db.Close()

	if err := database.RunMigrations(cfg.DatabaseURL, "migrations"); err != nil {
		logger.Fatal("failed to run migrations", zap.Error(err))
	}

	redis, err := rdb.NewClient(cfg.RedisURL)
	if err != nil {
		logger.Fatal("failed to connect to redis", zap.Error(err))
	}
	defer redis.Close()

	// Cloudinary for file uploads (KYC docs, chat images).
	cloudinaryClient := storage.NewCloudinaryClient(
		cfg.CloudinaryCloudName,
		cfg.CloudinaryAPIKey,
		cfg.CloudinaryAPISecret,
	)

	// Stores
	userStore := users.NewStore(db)
	runnerStore := runners.NewStore(db)
	errandStore := errands.NewStore(db)
	offerStore := offers.NewStore(db)
	chatStore := chat.NewStore(db)
	deliveryStore := delivery.NewStore(db)
	reviewStore := reviews.NewStore(db)
	disputeStore := disputes.NewStore(db)
	notifStore := notifications.NewStore(db)
	adminStore := admin.NewStore(db)

	// Third-party services
	smsProvider := auth.NewAfricasTalkingProvider(cfg.ATAPIKey, cfg.ATUsername, cfg.ATSenderID, cfg.ATSandbox)
	notifPusher := notifications.NewFirebasePusher(cfg.FirebaseCredJSON)

	// Core services
	tokenSvc := auth.NewTokenService(cfg.JWTSecret, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
	otpSvc := auth.NewOTPService(redis, smsProvider, cfg.OTPExpiry)
	rateLimiter := middleware.NewRateLimiter(redis)

	authSvc := auth.NewService(auth.NewStore(db), otpSvc, tokenSvc, logger)
	userSvc := users.NewService(userStore, logger)
	runnerSvc := runners.NewService(runnerStore, userStore, logger)
	notifSvc := notifications.NewService(notifStore, notifPusher, logger)
	errandSvc := errands.NewService(errandStore, notifStore, notifPusher, logger)
	offerSvc := offers.NewService(offerStore, errandStore, runnerStore, notifStore, notifPusher, db, logger)
	chatSvc := chat.NewService(chatStore, logger)
	deliverySvc := delivery.NewService(deliveryStore, errandStore, notifStore, notifPusher, logger)
	reviewSvc := reviews.NewService(reviewStore, errandStore, runnerStore, logger)
	disputeSvc := disputes.NewService(disputeStore, errandStore, logger)
	adminSvc := admin.NewService(adminStore, runnerStore, logger)

	// Handlers
	authHandler := auth.NewHandler(authSvc, logger)
	userHandler := users.NewHandler(userSvc, logger)
	runnerHandler := runners.NewHandler(runnerSvc, cloudinaryClient, logger)
	errandHandler := errands.NewHandler(errandSvc, logger)
	offerHandler := offers.NewHandler(offerSvc, logger)
	chatHandler := chat.NewHandler(chatSvc, logger)
	deliveryHandler := delivery.NewHandler(deliverySvc, logger)
	reviewHandler := reviews.NewHandler(reviewSvc, logger)
	disputeHandler := disputes.NewHandler(disputeSvc, logger)
	notifHandler := notifications.NewHandler(notifSvc, logger)
	adminHandler := admin.NewHandler(adminSvc, logger)

	// Router
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(middleware.ZapLogger(logger))
	r.Use(chimiddleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		ExposedHeaders:   []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	authMiddleware := middleware.Authenticate(tokenSvc, logger)
	requireRole := middleware.RequireRole

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	r.Route("/api/v1", func(r chi.Router) {
		// Public — rate limited by IP
		r.Group(func(r chi.Router) {
			r.Use(rateLimiter.ByIP(20, time.Minute))
			r.Post("/auth/send-otp", authHandler.SendOTP)
			r.Post("/auth/verify-otp", authHandler.VerifyOTP)
			r.Post("/auth/refresh", authHandler.Refresh)
		})

		// Authenticated — rate limited by user
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware)
			r.Use(rateLimiter.ByUser(100, time.Minute))

			r.Post("/auth/logout", authHandler.Logout)

			r.Get("/profile", userHandler.GetProfile)
			r.Patch("/profile", userHandler.UpdateProfile)
			r.Patch("/profile/location", userHandler.UpdateLocation)

			r.Get("/notifications", notifHandler.List)
			r.Patch("/notifications/{id}/read", notifHandler.MarkRead)

			// Client errands
			r.Route("/errands", func(r chi.Router) {
				r.Use(requireRole("client", "admin"))
				r.Post("/", errandHandler.Create)
				r.Get("/", errandHandler.List)
				r.Route("/{errandId}", func(r chi.Router) {
					r.Get("/", errandHandler.GetByID)
					r.Patch("/", errandHandler.Update)
					r.Delete("/", errandHandler.Cancel)
					r.Post("/stops", errandHandler.AddStop)
					r.Get("/offers", offerHandler.ListForErrand)
					r.Post("/offers/{offerId}/accept", offerHandler.Accept)
					r.Post("/delivery-otp", deliveryHandler.GenerateOTP)
					r.Post("/disputes", disputeHandler.Open)
				})
			})

			// Runner
			r.Route("/runner", func(r chi.Router) {
				r.Use(requireRole("runner", "admin"))
				r.Post("/kyc", runnerHandler.SubmitKYC)
				r.Post("/kyc/upload", runnerHandler.UploadKYC)
				r.Post("/errands/{errandId}/claim", offerHandler.ClaimFixed)
				r.Get("/kyc/status", runnerHandler.KYCStatus)
				r.Patch("/availability", runnerHandler.SetAvailability)
				r.Post("/service-areas", runnerHandler.AddServiceArea)
				r.Delete("/service-areas/{id}", runnerHandler.RemoveServiceArea)
				r.Post("/errands/feed", errandHandler.Feed)
				r.Post("/errands/{errandId}/offers", offerHandler.PlaceBid)
				r.Patch("/errands/{errandId}/status", errandHandler.UpdateStatus)
				r.Get("/errands", errandHandler.ListForRunner)
				r.Post("/errands/{errandId}/verify-delivery", deliveryHandler.VerifyOTP)
				r.Get("/reviews", reviewHandler.ListForRunner) 


			})

			r.Route("/errands/{errandId}/chat", func(r chi.Router) {
			    r.Use(requireRole("client", "runner", "admin"))
			    r.Get("/", chatHandler.GetConversation)
			    r.Post("/messages", chatHandler.SendMessage)
			})

			// Reviews — both clients AND runners can submit/read
			r.Route("/errands/{errandId}/reviews", func(r chi.Router) {
			    r.Use(requireRole("client", "runner", "admin"))
			    r.Post("/", reviewHandler.Create)
			    r.Get("/", reviewHandler.List)
			})

			// Admin
			r.Route("/admin", func(r chi.Router) {
				r.Use(requireRole("admin"))
				r.Get("/users", adminHandler.ListUsers)
				r.Patch("/users/{id}/status", adminHandler.UpdateUserStatus)
				r.Get("/runner-kyc", adminHandler.ListPendingKYC)
				r.Patch("/runner-kyc/{id}", adminHandler.ReviewKYC)
				r.Get("/errands", adminHandler.ListErrands)
				r.Get("/disputes", adminHandler.ListDisputes)
				r.Patch("/disputes/{id}/resolve", adminHandler.ResolveDispute)
			})
		})
	})

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("server starting", zap.Int("port", cfg.Port), zap.String("env", cfg.Env))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("server forced to shutdown", zap.Error(err))
	}
	logger.Info("server stopped")
}

func buildLogger(env string) (*zap.Logger, error) {
	if env == "production" {
		return zap.NewProduction()
	}
	return zap.NewDevelopment()
}

// authStore wraps the pgxpool for the auth package store constructor.
func authStore(db interface{ Close() }) *auth.Store {
	// Type assertion is safe — db is always *pgxpool.Pool at this call site.
	type pgPool interface {
		Close()
	}
	_ = db.(pgPool)
	return nil // replaced by real wiring once pgxpool import is shared
}
