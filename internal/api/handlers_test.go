package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nanidhaara/baggage-eval/internal/domain"
	"github.com/nanidhaara/baggage-eval/internal/queue"
	"github.com/nanidhaara/baggage-eval/internal/repository"
)

func newServer() (*Server, *repository.Memory, *queue.Memory) {
	repo := repository.NewMemory()
	q := queue.NewMemory()
	return &Server{Repo: repo, Publisher: q, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: time.Now}, repo, q
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const records = `[
 {"baggage_id":"BAG001","flight_number":"6E2134","flight_date":"2026-11-20","origin":"HYD","destination":"DEL",
  "image_ref":"s3://b/BAG001.jpg","expected_category":"SUITCASE","timestamp":"2026-11-20T06:30:00Z","status":"CHECKED_IN"},
 {"baggage_id":"BAG001","flight_number":"6E2134","flight_date":"2026-11-20","origin":"HYD","destination":"DEL",
  "image_ref":"s3://b/BAG001.jpg","expected_category":"SUITCASE","timestamp":"2026-11-20T06:30:00Z","status":"CHECKED_IN"},
 {"baggage_id":"","flight_number":"bad","origin":"HYD","destination":"HYD"}
]`

func TestIngestRecords(t *testing.T) {
	s, repo, _ := newServer()
	rec := do(t, s.Routes(), "POST", "/api/v1/baggage-records", records)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var resp IngestResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Accepted) != 1 || len(resp.Rejected) != 2 {
		t.Fatalf("accepted=%v rejected=%+v", resp.Accepted, resp.Rejected)
	}
	if resp.Rejected[0].Reason != "DUPLICATE" || resp.Rejected[1].Reason != "VALIDATION" {
		t.Errorf("unexpected reasons: %+v", resp.Rejected)
	}
	errs, _ := repo.ListProcessingErrors(context.Background())
	if len(errs) != 2 {
		t.Errorf("expected 2 processing errors stored, got %d", len(errs))
	}
}

func TestIngestPredictionsQueuesJobs(t *testing.T) {
	s, _, q := newServer()
	h := s.Routes()
	do(t, h, "POST", "/api/v1/baggage-records", records)

	body := `[
	  {"prediction_id":"P1","baggage_id":"BAG001","predicted_category":"SUITCASE","confidence":0.92,
	   "attributes":{"color":"black","size":"large"},"model_name":"bagnet","model_version":"1.2.0"},
	  {"prediction_id":"P2","baggage_id":"UNKNOWN","predicted_category":"SUITCASE","confidence":0.9,
	   "model_name":"bagnet","model_version":"1.2.0"}
	]`
	rec := do(t, h, "POST", "/api/v1/predictions", body)
	var resp IngestResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Accepted) != 1 || len(resp.EvaluationIDs) != 1 {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.Rejected[0].Reason != "NOT_FOUND" {
		t.Errorf("want NOT_FOUND for unknown bag, got %+v", resp.Rejected)
	}
	if len(q.Jobs) != 1 || q.Jobs[0].EvaluationID != resp.EvaluationIDs[0] {
		t.Errorf("queued jobs = %+v", q.Jobs)
	}

	get := do(t, h, "GET", "/api/v1/evaluations/"+resp.EvaluationIDs[0], "")
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"PENDING"`) {
		t.Errorf("get evaluation: %d %s", get.Code, get.Body)
	}
}

func TestQueueDownKeepsEvaluationPending(t *testing.T) {
	s, repo, q := newServer()
	h := s.Routes()
	do(t, h, "POST", "/api/v1/baggage-records", records)
	q.FailPublish = errors.New("rabbitmq unavailable")

	rec := do(t, h, "POST", "/api/v1/predictions", `[{"prediction_id":"P1","baggage_id":"BAG001",
	  "predicted_category":"SUITCASE","confidence":0.92,"attributes":{"color":"black","size":"large"},
	  "model_name":"bagnet","model_version":"1"}]`)
	var resp IngestResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	ev, _ := repo.GetEvaluation(context.Background(), resp.EvaluationIDs[0])
	if ev.Status != domain.StatusPending || ev.LastError == "" {
		t.Errorf("status=%s last_error=%q", ev.Status, ev.LastError)
	}
}

func TestFeedbackValidation(t *testing.T) {
	s, _, _ := newServer()
	rec := do(t, s.Routes(), "POST", "/api/v1/evaluations/X/feedback", `{"reviewer":"","label":"MAYBE"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
	rec = do(t, s.Routes(), "POST", "/api/v1/evaluations/X/feedback", `{"reviewer":"r1","label":"CORRECT"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestBadJSON(t *testing.T) {
	s, _, _ := newServer()
	if rec := do(t, s.Routes(), "POST", "/api/v1/predictions", `{not json`); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestQualityReportCSV(t *testing.T) {
	s, _, _ := newServer()
	rec := do(t, s.Routes(), "GET", "/api/v1/reports/quality?format=csv", "")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "category,total") {
		t.Errorf("csv report: %d %s", rec.Code, rec.Body)
	}
}
