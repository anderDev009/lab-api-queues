package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"api-memberships/internal/config"
	"api-memberships/internal/httpapi"
	"api-memberships/internal/rabbitmq"
	"api-memberships/internal/retry"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()

	var mq *rabbitmq.Conn
	err := retry.Do(ctx, "rabbitmq", 10, time.Second, func() (err error) {
		mq, err = rabbitmq.Connect(cfg.RabbitMQURL, cfg.QueueName)
		return err
	})
	if err != nil {
		log.Fatalf("connect rabbitmq: %v", err)
	}
	defer mq.Close()

	router := httpapi.NewAsyncRouter(rabbitmq.NewPublisher(mq))
	if err := httpapi.Serve(ctx, ":"+cfg.HTTPPort, router); err != nil {
		log.Printf("server: %v", err)
	}
	log.Println("api-async stopped")
}
