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
	"api-memberships/internal/rabbitmq"
	"api-memberships/internal/reservation"
	"api-memberships/internal/retry"

	"gorm.io/gorm"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()

	// Reads are served synchronously from Postgres; writes go through the queue.
	var db *gorm.DB
	err := retry.Do(ctx, "postgres", 10, time.Second, func() (err error) {
		db, err = postgres.Connect(cfg.DatabaseURL)
		return err
	})
	if err != nil {
		log.Fatalf("connect postgres: %v", err)
	}
	defer postgres.Close(db)

	var mq *rabbitmq.Conn
	err = retry.Do(ctx, "rabbitmq", 10, time.Second, func() (err error) {
		mq, err = rabbitmq.Connect(cfg.RabbitMQURL, cfg.QueueName)
		return err
	})
	if err != nil {
		log.Fatalf("connect rabbitmq: %v", err)
	}
	defer mq.Close()

	reader := reservation.NewService(postgres.NewRepository(db))
	router := httpapi.NewAsyncRouter(reader, rabbitmq.NewPublisher(mq))
	if err := httpapi.Serve(ctx, ":"+cfg.HTTPPort, router); err != nil {
		log.Printf("server: %v", err)
	}
	log.Println("api-async stopped")
}
