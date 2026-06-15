// cmd/seed/main.go
//
// One-time admin seeder. Run with:
//
//	go run ./cmd/seed \
//	  -email admin@chukuago.com \
//	  -name  "Joseph Admin" \
//	  -pass  "password"
//
// The program exits after inserting (or skipping if the email already exists).
// Never commit real passwords — use a strong password and change it after first login.

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/chukuago/api/internal/auth"
	"github.com/chukuago/api/pkg/config"
	"github.com/chukuago/api/pkg/database"
	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()

	email := flag.String("email", "", "Admin email address (required)")
	name := flag.String("name", "", "Admin display name  (required)")
	pass := flag.String("pass", "", "Admin password      (required, min 8 chars)")
	flag.Parse()

	if *email == "" || *name == "" || *pass == "" {
		fmt.Fprintln(os.Stderr, "Usage: go run ./cmd/seed -email <email> -name <name> -pass <password>")
		os.Exit(1)
	}
	if len(*pass) < 8 {
		fmt.Fprintln(os.Stderr, "Password must be at least 8 characters")
		os.Exit(1)
	}

	cfg := config.MustLoad()

	db, err := database.NewPostgres(cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db connect failed: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	store := auth.NewStore(db)
	admin, err := store.CreateAdmin(ctx, *email, *name, *pass)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf(" Admin created\n")
	fmt.Printf("  ID    : %s\n", admin.ID)
	fmt.Printf("  Email : %s\n", admin.Email)
	fmt.Printf("  Name  : %s\n", admin.Name)
}
