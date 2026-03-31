package main

import (
	"fmt"

	"github.com/joho/godotenv"
	"github.com/kimenyu/chukuagobackend/cmd/api"
	"github.com/kimenyu/chukuagobackend/db"

	"log"
	"os"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, relying on system env variables")
	}

	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = "8080"
	}

	dbConn, err := db.Connect()
	if err != nil {
		log.Fatal("Failed to connect to DB:", err)
	}

	server := api.NewAPIServer(fmt.Sprintf(":%s", addr), dbConn)
	if err := server.Run(); err != nil {
		log.Fatal("Server failed:", err)
	}
}
