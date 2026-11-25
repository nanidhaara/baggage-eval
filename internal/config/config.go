// Package config loads settings from environment variables.
package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr        string
	DatabaseURL     string
	RabbitMQURL     string
	QueueName       string
	DeadLetterQueue string
	MaxAttempts     int
	RetryBackoff    time.Duration
	LowConfidence   float64 // below this -> LOW_CONFIDENCE
	ReviewThreshold float64 // correct but below this -> REVIEW
	HighConfidence  float64 // wrong but at or above this -> high-confidence misclassification
}

func Load() Config {
	return Config{
		HTTPAddr:        getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://baggage:baggage@localhost:5432/baggage_eval?sslmode=disable"),
		RabbitMQURL:     getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		QueueName:       getEnv("QUEUE_NAME", "evaluation.jobs"),
		DeadLetterQueue: getEnv("DEAD_LETTER_QUEUE", "evaluation.jobs.dlq"),
		MaxAttempts:     getInt("MAX_ATTEMPTS", 3),
		RetryBackoff:    getDuration("RETRY_BACKOFF", 2*time.Second),
		LowConfidence:   getFloat("LOW_CONFIDENCE_THRESHOLD", 0.50),
		ReviewThreshold: getFloat("REVIEW_THRESHOLD", 0.75),
		HighConfidence:  getFloat("HIGH_CONFIDENCE_THRESHOLD", 0.90),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func getFloat(key string, def float64) float64 {
	if v, err := strconv.ParseFloat(os.Getenv(key), 64); err == nil {
		return v
	}
	return def
}

func getDuration(key string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(key)); err == nil {
		return v
	}
	return def
}
