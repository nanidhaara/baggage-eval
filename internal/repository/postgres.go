package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nanidhaara/baggage-eval/internal/domain"
)

// Postgres implements Repository using database/sql.
// The driver (github.com/lib/pq) is registered in cmd/*/main.go.
type Postgres struct {
	db *sql.DB
}

func NewPostgres(db *sql.DB) *Postgres { return &Postgres{db: db} }

// isUniqueViolation detects Postgres error code 23505 without importing the driver.
func isUniqueViolation(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "duplicate key"))
}

// isForeignKeyViolation detects Postgres error code 23503.
func isForeignKeyViolation(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "23503") || strings.Contains(err.Error(), "foreign key"))
}

func (p *Postgres) SaveBaggageRecord(ctx context.Context, r domain.BaggageRecord) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO baggage_records
			(baggage_id, flight_number, flight_date, origin, destination, image_ref, expected_category, recorded_at, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		r.BaggageID, r.FlightNumber, r.FlightDate, r.Origin, r.Destination, r.ImageRef,
		r.ExpectedCategory, r.Timestamp, r.Status)
	if isUniqueViolation(err) {
		return ErrDuplicate
	}
	return err
}

func (p *Postgres) GetBaggageRecord(ctx context.Context, id string) (domain.BaggageRecord, error) {
	var r domain.BaggageRecord
	err := p.db.QueryRowContext(ctx, `
		SELECT baggage_id, flight_number, to_char(flight_date,'YYYY-MM-DD'), origin, destination,
		       image_ref, expected_category, recorded_at, status
		FROM baggage_records WHERE baggage_id = $1`, id).
		Scan(&r.BaggageID, &r.FlightNumber, &r.FlightDate, &r.Origin, &r.Destination,
			&r.ImageRef, &r.ExpectedCategory, &r.Timestamp, &r.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func (p *Postgres) SavePrediction(ctx context.Context, pr domain.Prediction) error {
	attrs, err := json.Marshal(pr.Attributes)
	if err != nil {
		return err
	}
	_, err = p.db.ExecContext(ctx, `
		INSERT INTO model_predictions
			(prediction_id, baggage_id, predicted_category, confidence, attributes, model_name, model_version, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		pr.PredictionID, pr.BaggageID, pr.PredictedCategory, pr.Confidence, attrs,
		pr.ModelName, pr.ModelVersion, pr.CreatedAt)
	switch {
	case isUniqueViolation(err):
		return ErrDuplicate
	case isForeignKeyViolation(err):
		return ErrNotFound
	}
	return err
}

func (p *Postgres) GetPrediction(ctx context.Context, id string) (domain.Prediction, error) {
	var pr domain.Prediction
	var attrs []byte
	err := p.db.QueryRowContext(ctx, `
		SELECT prediction_id, baggage_id, predicted_category, confidence, attributes, model_name, model_version, created_at
		FROM model_predictions WHERE prediction_id = $1`, id).
		Scan(&pr.PredictionID, &pr.BaggageID, &pr.PredictedCategory, &pr.Confidence, &attrs,
			&pr.ModelName, &pr.ModelVersion, &pr.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return pr, ErrNotFound
	}
	if err != nil {
		return pr, err
	}
	if len(attrs) > 0 {
		if err := json.Unmarshal(attrs, &pr.Attributes); err != nil {
			return pr, fmt.Errorf("decode attributes: %w", err)
		}
	}
	return pr, nil
}

const evalColumns = `id, prediction_id, baggage_id, COALESCE(expected_category,''), COALESCE(predicted_category,''),
	confidence, COALESCE(label,''), COALESCE(reason,''), COALESCE(error_category,''), quality_score,
	COALESCE(reviewer_label,''), status, attempts, COALESCE(last_error,''), created_at, updated_at`

func scanEvaluation(s interface{ Scan(...any) error }) (domain.Evaluation, error) {
	var e domain.Evaluation
	err := s.Scan(&e.ID, &e.PredictionID, &e.BaggageID, &e.ExpectedCategory, &e.PredictedCategory,
		&e.Confidence, &e.Label, &e.Reason, &e.ErrorCategory, &e.QualityScore,
		&e.ReviewerLabel, &e.Status, &e.Attempts, &e.LastError, &e.CreatedAt, &e.UpdatedAt)
	return e, err
}

func (p *Postgres) CreateEvaluation(ctx context.Context, e domain.Evaluation) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO evaluation_results
			(id, prediction_id, baggage_id, expected_category, predicted_category, confidence, status, attempts, created_at, updated_at)
		VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,$7,$8,$9,$10)`,
		e.ID, e.PredictionID, e.BaggageID, e.ExpectedCategory, e.PredictedCategory, e.Confidence,
		e.Status, e.Attempts, e.CreatedAt, e.UpdatedAt)
	if isUniqueViolation(err) {
		return ErrDuplicate
	}
	return err
}

func (p *Postgres) GetEvaluation(ctx context.Context, id string) (domain.Evaluation, error) {
	e, err := scanEvaluation(p.db.QueryRowContext(ctx,
		`SELECT `+evalColumns+` FROM evaluation_results WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

func (p *Postgres) UpdateEvaluation(ctx context.Context, e domain.Evaluation) error {
	res, err := p.db.ExecContext(ctx, `
		UPDATE evaluation_results SET
			label = NULLIF($2,''), reason = NULLIF($3,''), error_category = NULLIF($4,''),
			quality_score = $5, status = $6, attempts = $7, last_error = NULLIF($8,''),
			expected_category = NULLIF($9,''), updated_at = $10
		WHERE id = $1`,
		e.ID, e.Label, e.Reason, e.ErrorCategory, e.QualityScore, e.Status, e.Attempts, e.LastError,
		e.ExpectedCategory, e.UpdatedAt)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) ListEvaluations(ctx context.Context, f EvaluationFilter) ([]domain.Evaluation, error) {
	q := `SELECT ` + evalColumns + ` FROM evaluation_results WHERE 1=1`
	var args []any
	if f.Status != "" {
		args = append(args, f.Status)
		q += fmt.Sprintf(" AND status = $%d", len(args))
	}
	if f.Label != "" {
		args = append(args, f.Label)
		q += fmt.Sprintf(" AND label = $%d", len(args))
	}
	q += " ORDER BY created_at"
	if f.Limit > 0 {
		args = append(args, f.Limit)
		q += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	rows, err := p.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Evaluation
	for rows.Next() {
		e, err := scanEvaluation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AddFeedback stores feedback and the reviewer's label in one transaction.
func (p *Postgres) AddFeedback(ctx context.Context, f domain.ReviewerFeedback) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	res, err := tx.ExecContext(ctx,
		`UPDATE evaluation_results SET reviewer_label = $2, updated_at = now() WHERE id = $1`,
		f.EvaluationID, f.Label)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO reviewer_feedback (id, evaluation_id, reviewer, label, correct_category, comment, created_at)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,$7)`,
		f.ID, f.EvaluationID, f.Reviewer, f.Label, f.CorrectCategory, f.Comment, f.CreatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *Postgres) LatestGroundTruth(ctx context.Context, baggageID string) (domain.Category, error) {
	var c domain.Category
	err := p.db.QueryRowContext(ctx, `
		SELECT f.correct_category
		FROM reviewer_feedback f
		JOIN evaluation_results e ON e.id = f.evaluation_id
		WHERE e.baggage_id = $1 AND f.correct_category IS NOT NULL
		ORDER BY f.created_at DESC LIMIT 1`, baggageID).Scan(&c)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return c, err
}

func (p *Postgres) SaveProcessingError(ctx context.Context, pe domain.ProcessingError) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO processing_errors (id, source, error_type, reference, payload, message, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		pe.ID, pe.Source, pe.ErrorType, pe.Reference, pe.Payload, pe.Message, pe.CreatedAt)
	return err
}

func (p *Postgres) ListProcessingErrors(ctx context.Context) ([]domain.ProcessingError, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT id, source, error_type, reference, payload, message, created_at
		FROM processing_errors ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ProcessingError
	for rows.Next() {
		var pe domain.ProcessingError
		if err := rows.Scan(&pe.ID, &pe.Source, &pe.ErrorType, &pe.Reference, &pe.Payload, &pe.Message, &pe.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, pe)
	}
	return out, rows.Err()
}
