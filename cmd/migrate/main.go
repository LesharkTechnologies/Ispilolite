package main

import (
	"context"
	"log"
	"os"
	"time"

	"ispilolite/pkg/config"
	"ispilolite/pkg/database"
)

func main() {
	if _, err := config.Load(); err != nil {
		log.Fatal(err)
	}
	path := os.Getenv("CONFIG_PATH")
	if path == "" {
		path = "config/config.yaml"
	}
	_ = os.Setenv("SKIP_MIGRATIONS", "true")
	if err := database.InitDB(path); err != nil {
		log.Fatal(err)
	}
	defer database.CloseDB()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := database.RunMigrations(ctx, os.Getenv("MIGRATIONS_PATH")); err != nil {
		log.Fatal(err)
	}
	log.Println("database migrations and schema verification completed")
}
