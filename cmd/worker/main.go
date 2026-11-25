// Command worker consumes evaluation jobs from RabbitMQ and applies the rules.
package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"

	"github.com/nanidhaara/baggage-eval/internal/config"
	"github.com/nanidhaara/baggage-eval/internal/evaluation"
	"github.com/nanidhaara/baggage-eval/internal/queue/rabbitmq"
	"github.com/nanidhaara/baggage-eval/internal/repository"
	"github.com/nanidhaara/baggage-eval/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "worker")
	cfg := config.Load()

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	var mq *rabbitmq.Client
	for i := 0; i < 30; i++ {
		if err = db.Ping(); err == nil {
			if mq, err = rabbitmq.Dial(cfg.RabbitMQURL, cfg.QueueName, cfg.DeadLetterQueue); err == nil {
				break
			}
		}
		logger.Warn("waiting for postgres/rabbitmq", "error", err)
		time.Sleep(time.Second)
	}
	if mq == nil {
		logger.Error("dependencies not available", "error", err)
		os.Exit(1)
	}
	defer mq.Close()

	proc := &worker.Processor{
		Repo:      repository.NewPostgres(db),
		Publisher: mq,
		Thresholds: evaluation.Thresholds{
			LowConfidence: cfg.LowConfidence,
			Review:        cfg.ReviewThreshold,
			High:          cfg.HighConfidence,
		},
		MaxAttempts: cfg.MaxAttempts,
		Backoff:     cfg.RetryBackoff,
		Logger:      logger,
		Now:         func() time.Time { return time.Now().UTC() },
		Sleep:       time.Sleep,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Info("worker started", "queue", cfg.QueueName)
	if err := mq.Consume(ctx, proc.Handle); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("consume", "error", err)
		os.Exit(1)
	}
	logger.Info("worker stopped")
}
