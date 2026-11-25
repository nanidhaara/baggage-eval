//go:build integration

// Integration tests for the Postgres repository. They run against the real
// database started by docker compose and are skipped by a normal `go test`.
//
//	docker compose up -d postgres
//	go test -tags integration -v ./internal/repository/
package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/nanidhaara/baggage-eval/internal/domain"
)

func openTestDB(t *testing.T) *Postgres {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://baggage:baggage@localhost:5432/baggage_eval?sslmode=disable"
	}
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("postgres not reachable (%v); start it with: docker compose up -d postgres", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewPostgres(db)
}

// uniq gives every run its own ids so tests never clash with seed data
// or with an earlier run.
func uniq(prefix string) string { return prefix + "-" + domain.NewID()[:12] }

// cleanup deletes the rows a test created, children first.
func cleanup(t *testing.T, p *Postgres, baggageID string) {
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = p.db.ExecContext(ctx, `DELETE FROM reviewer_feedback WHERE evaluation_id IN
			(SELECT id FROM evaluation_results WHERE baggage_id = $1)`, baggageID)
		_, _ = p.db.ExecContext(ctx, `DELETE FROM evaluation_results WHERE baggage_id = $1`, baggageID)
		_, _ = p.db.ExecContext(ctx, `DELETE FROM model_predictions WHERE baggage_id = $1`, baggageID)
		_, _ = p.db.ExecContext(ctx, `DELETE FROM baggage_records WHERE baggage_id = $1`, baggageID)
		_, _ = p.db.ExecContext(ctx, `DELETE FROM processing_errors WHERE reference = $1`, baggageID)
	})
}

func sampleRecord(id string) domain.BaggageRecord {
	return domain.BaggageRecord{
		BaggageID: id, FlightNumber: "6E2134", FlightDate: "2026-11-20",
		Origin: "HYD", Destination: "DEL", ImageRef: "s3://bucket/" + id + ".jpg",
		ExpectedCategory: domain.CategorySuitcase,
		Timestamp:        time.Date(2026, 11, 20, 5, 10, 0, 0, time.UTC), Status: "CHECKED_IN",
	}
}

func TestPostgresBaggageRecords(t *testing.T) {
	p, ctx := openTestDB(t), context.Background()
	rec := sampleRecord(uniq("ITBAG"))
	cleanup(t, p, rec.BaggageID)

	if err := p.SaveBaggageRecord(ctx, rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := p.SaveBaggageRecord(ctx, rec); !errors.Is(err, ErrDuplicate) {
		t.Errorf("second save: got %v, want ErrDuplicate", err)
	}

	got, err := p.GetBaggageRecord(ctx, rec.BaggageID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.FlightDate != rec.FlightDate || got.ExpectedCategory != rec.ExpectedCategory ||
		got.Origin != rec.Origin || !got.Timestamp.Equal(rec.Timestamp) {
		t.Errorf("round trip mismatch:\n got  %+v\n want %+v", got, rec)
	}

	if _, err := p.GetBaggageRecord(ctx, "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing record: got %v, want ErrNotFound", err)
	}
}

func TestPostgresPredictions(t *testing.T) {
	p, ctx := openTestDB(t), context.Background()
	rec := sampleRecord(uniq("ITBAG"))
	cleanup(t, p, rec.BaggageID)
	if err := p.SaveBaggageRecord(ctx, rec); err != nil {
		t.Fatal(err)
	}

	pred := domain.Prediction{
		PredictionID: uniq("ITPRED"), BaggageID: rec.BaggageID,
		PredictedCategory: domain.CategoryHardCase, Confidence: 0.9312,
		Attributes: map[string]string{"color": "silver", "size": "medium"},
		ModelName:  "bagnet", ModelVersion: "1.2.0",
		CreatedAt: time.Date(2026, 11, 20, 6, 0, 0, 0, time.UTC),
	}
	if err := p.SavePrediction(ctx, pred); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := p.SavePrediction(ctx, pred); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate: got %v, want ErrDuplicate", err)
	}

	orphan := pred
	orphan.PredictionID = uniq("ITPRED")
	orphan.BaggageID = "no-such-bag"
	if err := p.SavePrediction(ctx, orphan); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown bag: got %v, want ErrNotFound (foreign key)", err)
	}

	got, err := p.GetPrediction(ctx, pred.PredictionID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Confidence != pred.Confidence || got.Attributes["color"] != "silver" || got.Attributes["size"] != "medium" {
		t.Errorf("round trip mismatch: %+v", got)
	}
}

func TestPostgresEvaluationsAndFeedback(t *testing.T) {
	p, ctx := openTestDB(t), context.Background()
	rec := sampleRecord(uniq("ITBAG"))
	cleanup(t, p, rec.BaggageID)
	if err := p.SaveBaggageRecord(ctx, rec); err != nil {
		t.Fatal(err)
	}
	pred := domain.Prediction{PredictionID: uniq("ITPRED"), BaggageID: rec.BaggageID,
		PredictedCategory: domain.CategorySuitcase, Confidence: 0.8,
		Attributes: map[string]string{"color": "red", "size": "large"}, ModelName: "bagnet", ModelVersion: "1"}
	if err := p.SavePrediction(ctx, pred); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	ev := domain.Evaluation{ID: uniq("ITEVAL"), PredictionID: pred.PredictionID, BaggageID: rec.BaggageID,
		PredictedCategory: pred.PredictedCategory, Confidence: pred.Confidence,
		Status: domain.StatusPending, CreatedAt: now, UpdatedAt: now}
	if err := p.CreateEvaluation(ctx, ev); err != nil {
		t.Fatalf("create: %v", err)
	}

	ev.Status = domain.StatusCompleted
	ev.Label = domain.LabelCorrect
	ev.Reason = "matches"
	ev.QualityScore = 1
	ev.Attempts = 1
	ev.ExpectedCategory = domain.CategorySuitcase
	if err := p.UpdateEvaluation(ctx, ev); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := p.GetEvaluation(ctx, ev.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != domain.StatusCompleted || got.Label != domain.LabelCorrect || got.Attempts != 1 ||
		got.ExpectedCategory != domain.CategorySuitcase || got.ErrorCategory != "" {
		t.Errorf("unexpected evaluation: %+v", got)
	}

	list, err := p.ListEvaluations(ctx, EvaluationFilter{Status: domain.StatusCompleted, Label: domain.LabelCorrect})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, e := range list {
		found = found || e.ID == ev.ID
	}
	if !found {
		t.Error("filtered list does not contain the evaluation")
	}

	fb := domain.ReviewerFeedback{ID: uniq("ITFB"), EvaluationID: ev.ID, Reviewer: "sthuthi",
		Label: domain.LabelIncorrect, CorrectCategory: domain.CategoryHardCase, Comment: "shell case", CreatedAt: now}
	if err := p.AddFeedback(ctx, fb); err != nil {
		t.Fatalf("feedback: %v", err)
	}
	got, _ = p.GetEvaluation(ctx, ev.ID)
	if got.ReviewerLabel != domain.LabelIncorrect {
		t.Errorf("reviewer_label = %q, want INCORRECT", got.ReviewerLabel)
	}
	truth, err := p.LatestGroundTruth(ctx, rec.BaggageID)
	if err != nil || truth != domain.CategoryHardCase {
		t.Errorf("ground truth = %q, %v; want HARD_CASE", truth, err)
	}

	fb.ID = uniq("ITFB")
	fb.EvaluationID = "no-such-evaluation"
	if err := p.AddFeedback(ctx, fb); !errors.Is(err, ErrNotFound) {
		t.Errorf("feedback on unknown evaluation: got %v, want ErrNotFound", err)
	}
	if err := p.UpdateEvaluation(ctx, domain.Evaluation{ID: "no-such-evaluation", Status: domain.StatusFailed}); !errors.Is(err, ErrNotFound) {
		t.Errorf("update unknown evaluation: got %v, want ErrNotFound", err)
	}
}

func TestPostgresProcessingErrors(t *testing.T) {
	p, ctx := openTestDB(t), context.Background()
	ref := uniq("ITREF")
	cleanup(t, p, ref)
	pe := domain.ProcessingError{ID: uniq("ITERR"), Source: "baggage_record", ErrorType: "VALIDATION",
		Reference: ref, Payload: `{"baggage_id":""}`, Message: "baggage_id: is required", CreatedAt: time.Now().UTC()}
	if err := p.SaveProcessingError(ctx, pe); err != nil {
		t.Fatalf("save: %v", err)
	}
	all, err := p.ListProcessingErrors(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, e := range all {
		if e.ID == pe.ID {
			if e.ErrorType != "VALIDATION" || e.Payload != pe.Payload {
				t.Errorf("round trip mismatch: %+v", e)
			}
			return
		}
	}
	t.Error("saved processing error not found in list")
}
