// Package worker runs evaluation jobs taken from the queue.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nanidhaara/baggage-eval/internal/domain"
	"github.com/nanidhaara/baggage-eval/internal/evaluation"
	"github.com/nanidhaara/baggage-eval/internal/queue"
	"github.com/nanidhaara/baggage-eval/internal/repository"
)

// ErrPermanent marks failures that retrying cannot fix (e.g. missing data).
var ErrPermanent = errors.New("permanent failure")

// Processor evaluates one job and handles retries.
type Processor struct {
	Repo        repository.Repository
	Publisher   queue.Publisher
	Thresholds  evaluation.Thresholds
	MaxAttempts int
	Backoff     time.Duration
	Logger      *slog.Logger
	Now         func() time.Time
	Sleep       func(time.Duration) // replaced in tests
}

// Handle is the queue.Handler. It never returns an error for a job that
// was retried or dead-lettered, because that job is "handled".
func (p *Processor) Handle(ctx context.Context, job domain.EvaluationJob) error {
	log := p.Logger.With("evaluation_id", job.EvaluationID, "attempt", job.Attempt)

	err := p.process(ctx, job)
	if err == nil {
		log.Info("evaluation completed")
		return nil
	}

	// Duplicate message for an already-completed evaluation: nothing to do.
	if errors.Is(err, errAlreadyDone) {
		log.Info("skipping duplicate message, evaluation already completed")
		return nil
	}

	nextAttempt := job.Attempt + 1
	if errors.Is(err, ErrPermanent) || nextAttempt >= p.MaxAttempts {
		log.Error("evaluation failed, moving to dead-letter queue", "error", err)
		p.markFailed(ctx, job.EvaluationID, err)
		_ = p.Repo.SaveProcessingError(ctx, domain.ProcessingError{
			ID: domain.NewID(), Source: "worker", ErrorType: errorType(err),
			Reference: job.EvaluationID, Message: err.Error(), CreatedAt: p.Now(),
		})
		return p.Publisher.PublishDeadLetter(ctx, job, err.Error())
	}

	// Temporary failure: back off, then put the job back with Attempt+1.
	backoff := p.Backoff * time.Duration(1<<job.Attempt) // 1x, 2x, 4x ...
	log.Warn("temporary failure, retrying", "error", err, "backoff", backoff)
	p.markRetrying(ctx, job.EvaluationID, err)
	p.Sleep(backoff)
	return p.Publisher.Publish(ctx, domain.EvaluationJob{EvaluationID: job.EvaluationID, Attempt: nextAttempt})
}

var errAlreadyDone = errors.New("already completed")

func (p *Processor) process(ctx context.Context, job domain.EvaluationJob) error {
	ev, err := p.Repo.GetEvaluation(ctx, job.EvaluationID)
	if errors.Is(err, repository.ErrNotFound) {
		return fmt.Errorf("evaluation %s: %w", job.EvaluationID, ErrPermanent)
	}
	if err != nil {
		return fmt.Errorf("load evaluation: %w", err)
	}
	if ev.Status == domain.StatusCompleted {
		return errAlreadyDone
	}

	ev.Status = domain.StatusProcessing
	ev.Attempts++
	ev.UpdatedAt = p.Now()
	if err := p.Repo.UpdateEvaluation(ctx, ev); err != nil {
		return fmt.Errorf("mark processing: %w", err)
	}

	pred, err := p.Repo.GetPrediction(ctx, ev.PredictionID)
	if errors.Is(err, repository.ErrNotFound) {
		return fmt.Errorf("prediction %s not found: %w", ev.PredictionID, ErrPermanent)
	}
	if err != nil {
		return fmt.Errorf("load prediction: %w", err)
	}

	rec, err := p.Repo.GetBaggageRecord(ctx, ev.BaggageID)
	if errors.Is(err, repository.ErrNotFound) {
		return fmt.Errorf("baggage record %s not found: %w", ev.BaggageID, ErrPermanent)
	}
	if err != nil {
		return fmt.Errorf("load baggage record: %w", err)
	}

	// Reviewer ground truth wins over the expected category on the record.
	truth := rec.ExpectedCategory
	reviewerTruth, err := p.Repo.LatestGroundTruth(ctx, ev.BaggageID)
	if err != nil {
		return fmt.Errorf("load ground truth: %w", err)
	}
	if reviewerTruth != "" {
		truth = reviewerTruth
	}

	res := evaluation.Evaluate(evaluation.Input{Prediction: pred, GroundTruth: truth}, p.Thresholds)

	ev.ExpectedCategory = truth
	ev.Label = res.Label
	ev.Reason = res.Reason
	ev.ErrorCategory = res.ErrorCategory
	ev.QualityScore = res.QualityScore
	ev.Status = domain.StatusCompleted
	ev.LastError = ""
	ev.UpdatedAt = p.Now()
	if err := p.Repo.UpdateEvaluation(ctx, ev); err != nil {
		return fmt.Errorf("save result: %w", err)
	}
	return nil
}

func (p *Processor) markRetrying(ctx context.Context, id string, cause error) {
	if ev, err := p.Repo.GetEvaluation(ctx, id); err == nil {
		ev.Status = domain.StatusPending
		ev.LastError = cause.Error()
		ev.UpdatedAt = p.Now()
		_ = p.Repo.UpdateEvaluation(ctx, ev)
	}
}

func (p *Processor) markFailed(ctx context.Context, id string, cause error) {
	if ev, err := p.Repo.GetEvaluation(ctx, id); err == nil {
		ev.Status = domain.StatusFailed
		ev.LastError = cause.Error()
		ev.UpdatedAt = p.Now()
		_ = p.Repo.UpdateEvaluation(ctx, ev)
	}
}

func errorType(err error) string {
	if errors.Is(err, ErrPermanent) {
		return "NOT_FOUND"
	}
	return "INTERNAL"
}
