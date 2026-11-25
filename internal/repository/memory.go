package repository

import (
	"context"
	"sort"
	"sync"

	"github.com/nanidhaara/baggage-eval/internal/domain"
)

// Memory is a thread-safe in-memory Repository used in tests and demos.
type Memory struct {
	mu          sync.RWMutex
	records     map[string]domain.BaggageRecord
	predictions map[string]domain.Prediction
	evaluations map[string]domain.Evaluation
	feedback    []domain.ReviewerFeedback
	procErrors  []domain.ProcessingError

	// FailNext, when set, makes the next call to GetPrediction return this
	// error once. Used to simulate a temporary database failure.
	FailNext error
}

func NewMemory() *Memory {
	return &Memory{
		records:     map[string]domain.BaggageRecord{},
		predictions: map[string]domain.Prediction{},
		evaluations: map[string]domain.Evaluation{},
	}
}

func (m *Memory) SaveBaggageRecord(_ context.Context, r domain.BaggageRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.records[r.BaggageID]; ok {
		return ErrDuplicate
	}
	m.records[r.BaggageID] = r
	return nil
}

func (m *Memory) GetBaggageRecord(_ context.Context, id string) (domain.BaggageRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.records[id]
	if !ok {
		return r, ErrNotFound
	}
	return r, nil
}

func (m *Memory) SavePrediction(_ context.Context, p domain.Prediction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.predictions[p.PredictionID]; ok {
		return ErrDuplicate
	}
	if _, ok := m.records[p.BaggageID]; !ok {
		return ErrNotFound // mirrors the foreign key in Postgres
	}
	m.predictions[p.PredictionID] = p
	return nil
}

func (m *Memory) GetPrediction(_ context.Context, id string) (domain.Prediction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailNext != nil {
		err := m.FailNext
		m.FailNext = nil
		return domain.Prediction{}, err
	}
	p, ok := m.predictions[id]
	if !ok {
		return p, ErrNotFound
	}
	return p, nil
}

func (m *Memory) CreateEvaluation(_ context.Context, e domain.Evaluation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.evaluations[e.ID]; ok {
		return ErrDuplicate
	}
	m.evaluations[e.ID] = e
	return nil
}

func (m *Memory) GetEvaluation(_ context.Context, id string) (domain.Evaluation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.evaluations[id]
	if !ok {
		return e, ErrNotFound
	}
	return e, nil
}

func (m *Memory) UpdateEvaluation(_ context.Context, e domain.Evaluation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.evaluations[e.ID]; !ok {
		return ErrNotFound
	}
	m.evaluations[e.ID] = e
	return nil
}

func (m *Memory) ListEvaluations(_ context.Context, f EvaluationFilter) ([]domain.Evaluation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []domain.Evaluation
	for _, e := range m.evaluations {
		if f.Status != "" && e.Status != f.Status {
			continue
		}
		if f.Label != "" && e.Label != f.Label {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (m *Memory) AddFeedback(_ context.Context, f domain.ReviewerFeedback) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.evaluations[f.EvaluationID]
	if !ok {
		return ErrNotFound
	}
	m.feedback = append(m.feedback, f)
	e.ReviewerLabel = f.Label
	m.evaluations[e.ID] = e
	return nil
}

func (m *Memory) LatestGroundTruth(_ context.Context, baggageID string) (domain.Category, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := len(m.feedback) - 1; i >= 0; i-- {
		f := m.feedback[i]
		if f.CorrectCategory == "" {
			continue
		}
		if e, ok := m.evaluations[f.EvaluationID]; ok && e.BaggageID == baggageID {
			return f.CorrectCategory, nil
		}
	}
	return "", nil
}

func (m *Memory) SaveProcessingError(_ context.Context, pe domain.ProcessingError) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.procErrors = append(m.procErrors, pe)
	return nil
}

func (m *Memory) ListProcessingErrors(_ context.Context) ([]domain.ProcessingError, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]domain.ProcessingError(nil), m.procErrors...), nil
}
