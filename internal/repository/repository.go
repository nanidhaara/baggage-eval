// Package repository defines storage for the platform. The Postgres
// implementation is used in production; the in-memory one in tests.
package repository

import (
	"context"
	"errors"

	"github.com/nanidhaara/baggage-eval/internal/domain"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrDuplicate = errors.New("duplicate")
)

// EvaluationFilter narrows ListEvaluations.
type EvaluationFilter struct {
	Status domain.Status
	Label  domain.Label
	Limit  int
}

// Repository is everything the services need from storage.
type Repository interface {
	SaveBaggageRecord(ctx context.Context, r domain.BaggageRecord) error
	GetBaggageRecord(ctx context.Context, baggageID string) (domain.BaggageRecord, error)

	SavePrediction(ctx context.Context, p domain.Prediction) error
	GetPrediction(ctx context.Context, predictionID string) (domain.Prediction, error)

	CreateEvaluation(ctx context.Context, e domain.Evaluation) error
	GetEvaluation(ctx context.Context, id string) (domain.Evaluation, error)
	UpdateEvaluation(ctx context.Context, e domain.Evaluation) error
	ListEvaluations(ctx context.Context, f EvaluationFilter) ([]domain.Evaluation, error)

	AddFeedback(ctx context.Context, f domain.ReviewerFeedback) error
	// LatestGroundTruth returns the most recent reviewer-provided category for a bag.
	LatestGroundTruth(ctx context.Context, baggageID string) (domain.Category, error)

	SaveProcessingError(ctx context.Context, pe domain.ProcessingError) error
	ListProcessingErrors(ctx context.Context) ([]domain.ProcessingError, error)
}
