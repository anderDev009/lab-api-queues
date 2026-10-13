// Package config loads application settings from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	HTTPPort          string
	DatabaseURL       string
	RabbitMQURL       string
	QueueName         string
	WorkerConcurrency int
	WorkerPrefetch    int
}

func Load() Config {
	return Config{
		HTTPPort:          getEnv("HTTP_PORT", "8080"),
		DatabaseURL:       databaseURL(),
		RabbitMQURL:       getEnv("RABBITMQ_URL", "amqp://guest:guest@rabbitmq:5672/"),
		QueueName:         getEnv("QUEUE_NAME", "reservations"),
		WorkerConcurrency: getInt("WORKER_CONCURRENCY", 10),
		WorkerPrefetch:    getInt("WORKER_PREFETCH", 20),
	}
}

// databaseURL prefers DATABASE_URL and falls back to discrete DB_* variables.
func databaseURL() string {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return dsn
	}
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		getEnv("DB_HOST", "postgres"),
		getEnv("DB_PORT", "5432"),
		getEnv("DB_USER", "postgres"),
		getEnv("DB_PASSWORD", "postgres"),
		getEnv("DB_NAME", "reservations"),
	)
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	n, err := strconv.Atoi(os.Getenv(key))
	if err != nil || n <= 0 {
		return def
	}
	return n
}
