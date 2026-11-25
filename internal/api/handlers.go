package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/nanidhaara/baggage-eval/internal/domain"
	"github.com/nanidhaara/baggage-eval/internal/report"
	"github.com/nanidhaara/baggage-eval/internal/repository"
	"github.com/nanidhaara/baggage-eval/internal/validation"
)

// Rejected describes one item in a batch that was not stored.
type Rejected struct {
	Index  int                     `json:"index"`
	ID     string                  `json:"id,omitempty"`
	Reason string                  `json:"reason"` // VALIDATION, DUPLICATE, NOT_FOUND, INTERNAL
	Errors []validation.FieldError `json:"errors,omitempty"`
}

// IngestResponse is returned by both ingestion endpoints.
type IngestResponse struct {
	Accepted      []string   `json:"accepted"`
	Rejected      []Rejected `json:"rejected"`
	EvaluationIDs []string   `json:"evaluation_ids,omitempty"`
}

// POST /api/v1/baggage-records  body: [BaggageRecord, ...]
func (s *Server) handleIngestRecords(w http.ResponseWriter, r *http.Request) {
	var records []domain.BaggageRecord
	if err := json.NewDecoder(r.Body).Decode(&records); err != nil {
		writeError(w, http.StatusBadRequest, "body must be a JSON array of baggage records: "+err.Error())
		return
	}
	ctx := r.Context()
	resp := IngestResponse{Accepted: []string{}, Rejected: []Rejected{}}
	seen := map[string]bool{}

	for i, rec := range records {
		if errs := validation.ValidateBaggageRecord(rec); len(errs) > 0 {
			s.reject(ctx, &resp, i, rec.BaggageID, "baggage_record", "VALIDATION", errs.Error(), errs, rec)
			continue
		}
		if seen[rec.BaggageID] {
			s.reject(ctx, &resp, i, rec.BaggageID, "baggage_record", "DUPLICATE", "duplicate baggage_id in batch", nil, rec)
			continue
		}
		seen[rec.BaggageID] = true

		switch err := s.Repo.SaveBaggageRecord(ctx, rec); {
		case errors.Is(err, repository.ErrDuplicate):
			s.reject(ctx, &resp, i, rec.BaggageID, "baggage_record", "DUPLICATE", "baggage_id already exists", nil, rec)
		case err != nil:
			s.Logger.Error("save baggage record", "error", err, "baggage_id", rec.BaggageID)
			s.reject(ctx, &resp, i, rec.BaggageID, "baggage_record", "INTERNAL", "could not store record", nil, rec)
		default:
			resp.Accepted = append(resp.Accepted, rec.BaggageID)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// POST /api/v1/predictions  body: [Prediction, ...]
// Each stored prediction gets a PENDING evaluation and a queued job.
func (s *Server) handleIngestPredictions(w http.ResponseWriter, r *http.Request) {
	var preds []domain.Prediction
	if err := json.NewDecoder(r.Body).Decode(&preds); err != nil {
		writeError(w, http.StatusBadRequest, "body must be a JSON array of predictions: "+err.Error())
		return
	}
	ctx := r.Context()
	resp := IngestResponse{Accepted: []string{}, Rejected: []Rejected{}, EvaluationIDs: []string{}}
	seen := map[string]bool{}

	for i, p := range preds {
		if errs := validation.ValidatePrediction(p); len(errs) > 0 {
			s.reject(ctx, &resp, i, p.PredictionID, "prediction", "VALIDATION", errs.Error(), errs, p)
			continue
		}
		if seen[p.PredictionID] {
			s.reject(ctx, &resp, i, p.PredictionID, "prediction", "DUPLICATE", "duplicate prediction_id in batch", nil, p)
			continue
		}
		seen[p.PredictionID] = true
		if p.CreatedAt.IsZero() {
			p.CreatedAt = s.Now()
		}

		switch err := s.Repo.SavePrediction(ctx, p); {
		case errors.Is(err, repository.ErrDuplicate):
			s.reject(ctx, &resp, i, p.PredictionID, "prediction", "DUPLICATE", "prediction_id already exists", nil, p)
			continue
		case errors.Is(err, repository.ErrNotFound):
			s.reject(ctx, &resp, i, p.PredictionID, "prediction", "NOT_FOUND", "baggage_id "+p.BaggageID+" does not exist", nil, p)
			continue
		case err != nil:
			s.Logger.Error("save prediction", "error", err, "prediction_id", p.PredictionID)
			s.reject(ctx, &resp, i, p.PredictionID, "prediction", "INTERNAL", "could not store prediction", nil, p)
			continue
		}

		now := s.Now()
		ev := domain.Evaluation{
			ID: domain.NewID(), PredictionID: p.PredictionID, BaggageID: p.BaggageID,
			PredictedCategory: p.PredictedCategory, Confidence: p.Confidence,
			Status: domain.StatusPending, CreatedAt: now, UpdatedAt: now,
		}
		if err := s.Repo.CreateEvaluation(ctx, ev); err != nil {
			s.Logger.Error("create evaluation", "error", err, "prediction_id", p.PredictionID)
			s.reject(ctx, &resp, i, p.PredictionID, "prediction", "INTERNAL", "could not create evaluation", nil, p)
			continue
		}
		resp.Accepted = append(resp.Accepted, p.PredictionID)
		resp.EvaluationIDs = append(resp.EvaluationIDs, ev.ID)
		s.enqueue(ctx, ev)
	}
	writeJSON(w, http.StatusOK, resp)
}

// enqueue publishes a job. If the queue is down the evaluation stays
// PENDING with last_error set, and can be retried via /requeue.
func (s *Server) enqueue(ctx context.Context, ev domain.Evaluation) {
	if err := s.Publisher.Publish(ctx, domain.EvaluationJob{EvaluationID: ev.ID}); err != nil {
		s.Logger.Error("publish evaluation job", "error", err, "evaluation_id", ev.ID)
		ev.LastError = "queue publish failed: " + err.Error()
		ev.UpdatedAt = s.Now()
		_ = s.Repo.UpdateEvaluation(ctx, ev)
	}
}

// GET /api/v1/evaluations?status=COMPLETED&label=INCORRECT&limit=50
func (s *Server) handleListEvaluations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := repository.EvaluationFilter{
		Status: domain.Status(q.Get("status")),
		Label:  domain.Label(q.Get("label")),
		Limit:  100,
	}
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 && l <= 1000 {
		f.Limit = l
	}
	evals, err := s.Repo.ListEvaluations(r.Context(), f)
	if err != nil {
		s.Logger.Error("list evaluations", "error", err)
		writeError(w, http.StatusInternalServerError, "could not list evaluations")
		return
	}
	if evals == nil {
		evals = []domain.Evaluation{}
	}
	writeJSON(w, http.StatusOK, evals)
}

// GET /api/v1/evaluations/{id}
func (s *Server) handleGetEvaluation(w http.ResponseWriter, r *http.Request) {
	ev, err := s.Repo.GetEvaluation(r.Context(), r.PathValue("id"))
	if errors.Is(err, repository.ErrNotFound) {
		writeError(w, http.StatusNotFound, "evaluation not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load evaluation")
		return
	}
	writeJSON(w, http.StatusOK, ev)
}

// FeedbackRequest is the body of the feedback endpoint.
type FeedbackRequest struct {
	Reviewer        string          `json:"reviewer"`
	Label           domain.Label    `json:"label"`
	CorrectCategory domain.Category `json:"correct_category,omitempty"`
	Comment         string          `json:"comment"`
}

// POST /api/v1/evaluations/{id}/feedback
// If the reviewer gives correct_category, the evaluation is re-run with
// that as ground truth.
func (s *Server) handleFeedback(w http.ResponseWriter, r *http.Request) {
	var req FeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	var errs validation.Errors
	if req.Reviewer == "" {
		errs = append(errs, validation.FieldError{Field: "reviewer", Message: "is required"})
	}
	if !domain.ValidLabels[req.Label] {
		errs = append(errs, validation.FieldError{Field: "label", Message: "must be one of CORRECT, INCORRECT, INCOMPLETE, REVIEW, LOW_CONFIDENCE"})
	}
	if req.CorrectCategory != "" && !domain.SupportedCategories[req.CorrectCategory] {
		errs = append(errs, validation.FieldError{Field: "correct_category", Message: "unsupported category"})
	}
	if len(errs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"errors": errs})
		return
	}

	ctx := r.Context()
	id := r.PathValue("id")
	fb := domain.ReviewerFeedback{
		ID: domain.NewID(), EvaluationID: id, Reviewer: req.Reviewer, Label: req.Label,
		CorrectCategory: req.CorrectCategory, Comment: req.Comment, CreatedAt: s.Now(),
	}
	if err := s.Repo.AddFeedback(ctx, fb); errors.Is(err, repository.ErrNotFound) {
		writeError(w, http.StatusNotFound, "evaluation not found")
		return
	} else if err != nil {
		s.Logger.Error("add feedback", "error", err, "evaluation_id", id)
		writeError(w, http.StatusInternalServerError, "could not save feedback")
		return
	}

	reevaluate := false
	if req.CorrectCategory != "" {
		if ev, err := s.Repo.GetEvaluation(ctx, id); err == nil {
			ev.Status = domain.StatusPending
			ev.UpdatedAt = s.Now()
			if err := s.Repo.UpdateEvaluation(ctx, ev); err == nil {
				s.enqueue(ctx, ev)
				reevaluate = true
			}
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"feedback": fb, "reevaluation_queued": reevaluate})
}

// POST /api/v1/evaluations/{id}/requeue  re-publishes a PENDING or FAILED job.
func (s *Server) handleRequeue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ev, err := s.Repo.GetEvaluation(ctx, r.PathValue("id"))
	if errors.Is(err, repository.ErrNotFound) {
		writeError(w, http.StatusNotFound, "evaluation not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load evaluation")
		return
	}
	if ev.Status == domain.StatusCompleted || ev.Status == domain.StatusProcessing {
		writeError(w, http.StatusConflict, "only PENDING or FAILED evaluations can be requeued")
		return
	}
	ev.Status = domain.StatusPending
	ev.Attempts = 0
	ev.UpdatedAt = s.Now()
	if err := s.Repo.UpdateEvaluation(ctx, ev); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update evaluation")
		return
	}
	if err := s.Publisher.Publish(ctx, domain.EvaluationJob{EvaluationID: ev.ID}); err != nil {
		writeError(w, http.StatusServiceUnavailable, "queue unavailable")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "requeued", "evaluation_id": ev.ID})
}

// GET /api/v1/reports/quality?format=json|csv
func (s *Server) handleQualityReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	evals, err := s.Repo.ListEvaluations(ctx, repository.EvaluationFilter{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load evaluations")
		return
	}
	procErrs, err := s.Repo.ListProcessingErrors(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load processing errors")
		return
	}
	rep := report.Build(evals, procErrs, s.Now())

	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", `attachment; filename="quality_report.csv"`)
		if err := report.WriteCSV(w, rep); err != nil {
			s.Logger.Error("write csv", "error", err)
		}
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// reject records a rejected item in the response and in processing_errors.
func (s *Server) reject(ctx context.Context, resp *IngestResponse, idx int, id, source, reason, msg string,
	errs validation.Errors, payload any) {
	resp.Rejected = append(resp.Rejected, Rejected{Index: idx, ID: id, Reason: reason, Errors: errs})
	body, _ := json.Marshal(payload)
	if err := s.Repo.SaveProcessingError(ctx, domain.ProcessingError{
		ID: domain.NewID(), Source: source, ErrorType: reason, Reference: id,
		Payload: string(body), Message: msg, CreatedAt: s.Now(),
	}); err != nil {
		s.Logger.Error("save processing error", "error", err)
	}
}
