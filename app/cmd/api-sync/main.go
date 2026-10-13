package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"api-memberships/internal/config"
	"api-memberships/internal/httpapi"
	"api-memberships/internal/postgres"
	"api-memberships/internal/reservation"
	"api-memberships/internal/retry"

	"gorm.io/gorm"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()

	var db *gorm.DB
	err := retry.Do(ctx, "postgres", 10, time.Second, func() (err error) {
		db, err = postgres.Connect(cfg.DatabaseURL)
		return err
	})
	if err != nil {
		log.Fatalf("connect postgres: %v", err)
	}
	defer postgres.Close(db)

	svc := reservation.NewService(postgres.NewRepository(db))
	if err := httpapi.Serve(ctx, ":"+cfg.HTTPPort, httpapi.NewSyncRouter(svc)); err != nil {
		log.Printf("server: %v", err)
	}
	log.Println("api-sync stopped")
}
