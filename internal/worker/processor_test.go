package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/nanidhaara/baggage-eval/internal/domain"
	"github.com/nanidhaara/baggage-eval/internal/evaluation"
	"github.com/nanidhaara/baggage-eval/internal/queue"
	"github.com/nanidhaara/baggage-eval/internal/repository"
)

func setup(t *testing.T) (*Processor, *repository.Memory, *queue.Memory) {
	t.Helper()
	ctx := context.Background()
	repo := repository.NewMemory()
	q := queue.NewMemory()
	_ = repo.SaveBaggageRecord(ctx, domain.BaggageRecord{BaggageID: "B1", ExpectedCategory: domain.CategorySuitcase})
	_ = repo.SavePrediction(ctx, domain.Prediction{PredictionID: "P1", BaggageID: "B1",
		PredictedCategory: domain.CategorySuitcase, Confidence: 0.93,
		Attributes: map[string]string{"color": "blue", "size": "medium"}})
	_ = repo.CreateEvaluation(ctx, domain.Evaluation{ID: "E1", PredictionID: "P1", BaggageID: "B1", Status: domain.StatusPending})

	p := &Processor{
		Repo: repo, Publisher: q, Thresholds: evaluation.DefaultThresholds,
		MaxAttempts: 3, Backoff: time.Millisecond,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:    time.Now, Sleep: func(time.Duration) {},
	}
	return p, repo, q
}

func TestHandleCompletes(t *testing.T) {
	p, repo, _ := setup(t)
	if err := p.Handle(context.Background(), domain.EvaluationJob{EvaluationID: "E1"}); err != nil {
		t.Fatal(err)
	}
	ev, _ := repo.GetEvaluation(context.Background(), "E1")
	if ev.Status != domain.StatusCompleted || ev.Label != domain.LabelCorrect {
		t.Errorf("got status=%s label=%s", ev.Status, ev.Label)
	}
}

func TestHandleRetriesTemporaryFailure(t *testing.T) {
	p, repo, q := setup(t)
	repo.FailNext = errors.New("connection reset")

	_ = p.Handle(context.Background(), domain.EvaluationJob{EvaluationID: "E1"})
	if len(q.Jobs) != 1 || q.Jobs[0].Attempt != 1 {
		t.Fatalf("expected one retry job with attempt 1, got %+v", q.Jobs)
	}
	_ = q.Consume(context.Background(), p.Handle) // retry succeeds
	ev, _ := repo.GetEvaluation(context.Background(), "E1")
	if ev.Status != domain.StatusCompleted || ev.Attempts != 2 {
		t.Errorf("got status=%s attempts=%d", ev.Status, ev.Attempts)
	}
}

func TestHandleDeadLettersAfterMaxAttempts(t *testing.T) {
	p, repo, q := setup(t)
	_ = p.Handle(context.Background(), domain.EvaluationJob{EvaluationID: "E1", Attempt: 2})
	// Attempt 2 succeeds normally; now force a failing final attempt.
	_ = repo.UpdateEvaluation(context.Background(), domain.Evaluation{ID: "E1", PredictionID: "P1", BaggageID: "B1", Status: domain.StatusPending})
	repo.FailNext = errors.New("db down")
	_ = p.Handle(context.Background(), domain.EvaluationJob{EvaluationID: "E1", Attempt: 2})

	ev, _ := repo.GetEvaluation(context.Background(), "E1")
	if ev.Status != domain.StatusFailed {
		t.Errorf("status = %s, want FAILED", ev.Status)
	}
	if len(q.DeadLetters) != 1 {
		t.Errorf("expected 1 dead letter, got %d", len(q.DeadLetters))
	}
}

func TestHandlePermanentFailureSkipsRetry(t *testing.T) {
	p, _, q := setup(t)
	_ = p.Handle(context.Background(), domain.EvaluationJob{EvaluationID: "does-not-exist"})
	if len(q.Jobs) != 0 || len(q.DeadLetters) != 1 {
		t.Errorf("jobs=%d deadletters=%d, want 0 and 1", len(q.Jobs), len(q.DeadLetters))
	}
}

func TestHandleDuplicateMessageIsIdempotent(t *testing.T) {
	p, repo, _ := setup(t)
	ctx := context.Background()
	_ = p.Handle(ctx, domain.EvaluationJob{EvaluationID: "E1"})
	_ = p.Handle(ctx, domain.EvaluationJob{EvaluationID: "E1"}) // same message again
	ev, _ := repo.GetEvaluation(ctx, "E1")
	if ev.Attempts != 1 {
		t.Errorf("attempts = %d, want 1 (duplicate must be skipped)", ev.Attempts)
	}
}

func TestReviewerGroundTruthOverridesExpected(t *testing.T) {
	p, repo, _ := setup(t)
	ctx := context.Background()
	_ = p.Handle(ctx, domain.EvaluationJob{EvaluationID: "E1"})
	_ = repo.AddFeedback(ctx, domain.ReviewerFeedback{ID: "F1", EvaluationID: "E1", Reviewer: "sthuthi",
		Label: domain.LabelIncorrect, CorrectCategory: domain.CategoryHardCase})

	ev, _ := repo.GetEvaluation(ctx, "E1")
	ev.Status = domain.StatusPending
	_ = repo.UpdateEvaluation(ctx, ev)
	_ = p.Handle(ctx, domain.EvaluationJob{EvaluationID: "E1"})

	ev, _ = repo.GetEvaluation(ctx, "E1")
	if ev.Label != domain.LabelIncorrect || ev.ExpectedCategory != domain.CategoryHardCase {
		t.Errorf("got label=%s expected=%s", ev.Label, ev.ExpectedCategory)
	}
}
