package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"api-memberships/internal/config"
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

	svc := reservation.NewService(postgres.NewRepository(db))
	handle := func(ctx context.Context, job reservation.Job) error {
		res, err := svc.CreateReservation(ctx, job.Input)
		if err == nil {
			log.Printf("job %s stored as reservation %d", job.RequestID, res.ID)
		}
		return err
	}

	log.Printf("worker consuming %q (concurrency=%d, prefetch=%d)", cfg.QueueName, cfg.WorkerConcurrency, cfg.WorkerPrefetch)
	if err := mq.Consume(ctx, cfg.WorkerConcurrency, cfg.WorkerPrefetch, handle); err != nil {
		log.Printf("consumer: %v", err)
	}
	log.Println("worker stopped")
}
