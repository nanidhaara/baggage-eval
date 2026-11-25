// Command api runs the ingestion and reporting REST service.
package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq" // Postgres driver

	"github.com/nanidhaara/baggage-eval/internal/api"
	"github.com/nanidhaara/baggage-eval/internal/config"
	"github.com/nanidhaara/baggage-eval/internal/queue/rabbitmq"
	"github.com/nanidhaara/baggage-eval/internal/repository"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := waitFor(func() error { return db.Ping() }, 30, logger, "postgres"); err != nil {
		os.Exit(1)
	}

	var mq *rabbitmq.Client
	if err := waitFor(func() (e error) {
		mq, e = rabbitmq.Dial(cfg.RabbitMQURL, cfg.QueueName, cfg.DeadLetterQueue)
		return e
	}, 30, logger, "rabbitmq"); err != nil {
		os.Exit(1)
	}
	defer mq.Close()

	srv := &api.Server{
		Repo:      repository.NewPostgres(db),
		Publisher: mq,
		Logger:    logger,
		Now:       func() time.Time { return time.Now().UTC() },
	}
	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("api listening", "addr", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown on Ctrl+C or docker stop.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
	logger.Info("api stopped")
}

// waitFor retries fn once a second, because Postgres and RabbitMQ
// start slower than our service inside docker compose.
func waitFor(fn func() error, tries int, logger *slog.Logger, name string) error {
	var err error
	for i := 0; i < tries; i++ {
		if err = fn(); err == nil {
			return nil
		}
		logger.Warn("waiting for dependency", "name", name, "error", err)
		time.Sleep(time.Second)
	}
	logger.Error("dependency not available", "name", name, "error", err)
	return err
}
