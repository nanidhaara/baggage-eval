// Package queue defines how evaluation jobs move between the API and the worker.
package queue

import (
	"context"
	"sync"

	"github.com/nanidhaara/baggage-eval/internal/domain"
)

// Publisher sends jobs. The API only needs this.
type Publisher interface {
	Publish(ctx context.Context, job domain.EvaluationJob) error
	PublishDeadLetter(ctx context.Context, job domain.EvaluationJob, reason string) error
}

// Handler processes one job. Returning an error means "not done".
type Handler func(ctx context.Context, job domain.EvaluationJob) error

// Consumer receives jobs. The worker needs this.
type Consumer interface {
	Consume(ctx context.Context, h Handler) error
}

// DeadLetter is a job that could not be processed.
type DeadLetter struct {
	Job    domain.EvaluationJob
	Reason string
}

// Memory is an in-process queue used in tests.
type Memory struct {
	mu          sync.Mutex
	Jobs        []domain.EvaluationJob
	DeadLetters []DeadLetter
	// FailPublish, when set, makes every Publish return this error.
	FailPublish error
}

func NewMemory() *Memory { return &Memory{} }

func (m *Memory) Publish(_ context.Context, job domain.EvaluationJob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailPublish != nil {
		return m.FailPublish
	}
	m.Jobs = append(m.Jobs, job)
	return nil
}

func (m *Memory) PublishDeadLetter(_ context.Context, job domain.EvaluationJob, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DeadLetters = append(m.DeadLetters, DeadLetter{Job: job, Reason: reason})
	return nil
}

// Next pops the oldest job. ok is false when the queue is empty.
func (m *Memory) Next() (job domain.EvaluationJob, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Jobs) == 0 {
		return job, false
	}
	job, m.Jobs = m.Jobs[0], m.Jobs[1:]
	return job, true
}

// Consume drains the queue, including jobs re-published by the handler's
// retry logic, then returns.
func (m *Memory) Consume(ctx context.Context, h Handler) error {
	for {
		job, ok := m.Next()
		if !ok {
			return nil
		}
		_ = h(ctx, job) // the worker handles errors itself
	}
}
