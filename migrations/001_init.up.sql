-- Baggage evaluation platform schema.
-- Runs automatically on first start of the postgres container (docker-entrypoint-initdb.d).

CREATE TABLE IF NOT EXISTS annotation_labels (
    label       TEXT PRIMARY KEY,
    description TEXT NOT NULL
);

INSERT INTO annotation_labels (label, description) VALUES
    ('CORRECT',        'Prediction matches ground truth with enough confidence'),
    ('INCORRECT',      'Predicted category differs from ground truth'),
    ('INCOMPLETE',     'Required attributes (color, size) are missing'),
    ('REVIEW',         'Needs a human: no ground truth, or correct but borderline confidence'),
    ('LOW_CONFIDENCE', 'Model confidence below the minimum threshold')
ON CONFLICT (label) DO NOTHING;

CREATE TABLE IF NOT EXISTS baggage_records (
    baggage_id        TEXT PRIMARY KEY,
    flight_number     TEXT        NOT NULL,
    flight_date       DATE        NOT NULL,
    origin            CHAR(3)     NOT NULL,
    destination       CHAR(3)     NOT NULL,
    image_ref         TEXT        NOT NULL,
    expected_category TEXT        NOT NULL,
    recorded_at       TIMESTAMPTZ NOT NULL,
    status            TEXT        NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_route CHECK (origin <> destination)
);

CREATE TABLE IF NOT EXISTS model_predictions (
    prediction_id      TEXT PRIMARY KEY,
    baggage_id         TEXT        NOT NULL REFERENCES baggage_records(baggage_id),
    predicted_category TEXT        NOT NULL,
    confidence         NUMERIC(5,4) NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    attributes         JSONB       NOT NULL DEFAULT '{}',
    model_name         TEXT        NOT NULL,
    model_version      TEXT        NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS evaluation_results (
    id                 TEXT PRIMARY KEY,
    prediction_id      TEXT        NOT NULL UNIQUE REFERENCES model_predictions(prediction_id),
    baggage_id         TEXT        NOT NULL REFERENCES baggage_records(baggage_id),
    expected_category  TEXT,
    predicted_category TEXT,
    confidence         NUMERIC(5,4) NOT NULL DEFAULT 0,
    label              TEXT REFERENCES annotation_labels(label),
    reason             TEXT,
    error_category     TEXT,
    quality_score      NUMERIC(4,3) NOT NULL DEFAULT 0,
    reviewer_label     TEXT REFERENCES annotation_labels(label),
    status             TEXT        NOT NULL CHECK (status IN ('PENDING','PROCESSING','COMPLETED','FAILED')),
    attempts           INT         NOT NULL DEFAULT 0,
    last_error         TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS reviewer_feedback (
    id               TEXT PRIMARY KEY,
    evaluation_id    TEXT        NOT NULL REFERENCES evaluation_results(id) ON DELETE CASCADE,
    reviewer         TEXT        NOT NULL,
    label            TEXT        NOT NULL REFERENCES annotation_labels(label),
    correct_category TEXT,
    comment          TEXT        NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS processing_errors (
    id         TEXT PRIMARY KEY,
    source     TEXT        NOT NULL,
    error_type TEXT        NOT NULL,
    reference  TEXT        NOT NULL DEFAULT '',
    payload    TEXT        NOT NULL DEFAULT '',
    message    TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Indexes for the most common lookups and report queries.
CREATE INDEX IF NOT EXISTS idx_predictions_baggage_id   ON model_predictions (baggage_id);
CREATE INDEX IF NOT EXISTS idx_predictions_category     ON model_predictions (predicted_category);
CREATE INDEX IF NOT EXISTS idx_predictions_created_at   ON model_predictions (created_at);
CREATE INDEX IF NOT EXISTS idx_evaluations_baggage_id   ON evaluation_results (baggage_id);
CREATE INDEX IF NOT EXISTS idx_evaluations_status       ON evaluation_results (status);
CREATE INDEX IF NOT EXISTS idx_evaluations_label        ON evaluation_results (label);
CREATE INDEX IF NOT EXISTS idx_evaluations_created_at   ON evaluation_results (created_at);
CREATE INDEX IF NOT EXISTS idx_feedback_evaluation_id   ON reviewer_feedback (evaluation_id);
CREATE INDEX IF NOT EXISTS idx_processing_errors_type   ON processing_errors (error_type);
