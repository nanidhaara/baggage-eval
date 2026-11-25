// Package api is the REST layer: ingestion, evaluation lookup, reviewer
// feedback and quality reports.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/nanidhaara/baggage-eval/internal/queue"
	"github.com/nanidhaara/baggage-eval/internal/repository"
)

const maxBodyBytes = 5 << 20 // 5 MB per request

// Server holds the dependencies for every handler.
type Server struct {
	Repo      repository.Repository
	Publisher queue.Publisher
	Logger    *slog.Logger
	Now       func() time.Time
}

// Routes uses Go 1.22 method+path patterns, so no router library is needed.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /api/v1/baggage-records", s.handleIngestRecords)
	mux.HandleFunc("POST /api/v1/predictions", s.handleIngestPredictions)
	mux.HandleFunc("GET /api/v1/evaluations", s.handleListEvaluations)
	mux.HandleFunc("GET /api/v1/evaluations/{id}", s.handleGetEvaluation)
	mux.HandleFunc("POST /api/v1/evaluations/{id}/feedback", s.handleFeedback)
	mux.HandleFunc("POST /api/v1/evaluations/{id}/requeue", s.handleRequeue)
	mux.HandleFunc("GET /api/v1/reports/quality", s.handleQualityReport)
	return s.withMiddleware(mux)
}

// withMiddleware adds structured request logging and panic recovery.
func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if rec := recover(); rec != nil {
				s.Logger.Error("panic in handler", "panic", rec, "path", r.URL.Path)
				writeError(rw, http.StatusInternalServerError, "internal error")
			}
			s.Logger.Info("request",
				"method", r.Method, "path", r.URL.Path, "status", rw.status,
				"duration_ms", time.Since(start).Milliseconds())
		}()
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(rw, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
