// Package domain holds the core types shared by every service.
package domain

import "time"

// Category is a supported baggage category.
type Category string

const (
	CategorySuitcase        Category = "SUITCASE"
	CategoryBackpack        Category = "BACKPACK"
	CategoryDuffelBag       Category = "DUFFEL_BAG"
	CategoryHardCase        Category = "HARD_CASE"
	CategoryStroller        Category = "STROLLER"
	CategoryGolfBag         Category = "GOLF_BAG"
	CategorySportsEquipment Category = "SPORTS_EQUIPMENT"
	CategoryOther           Category = "OTHER"
)

// SupportedCategories is the closed list the validator accepts.
var SupportedCategories = map[Category]bool{
	CategorySuitcase: true, CategoryBackpack: true, CategoryDuffelBag: true,
	CategoryHardCase: true, CategoryStroller: true, CategoryGolfBag: true,
	CategorySportsEquipment: true, CategoryOther: true,
}

// Label is the quality label assigned to an AI prediction.
type Label string

const (
	LabelCorrect       Label = "CORRECT"
	LabelIncorrect     Label = "INCORRECT"
	LabelIncomplete    Label = "INCOMPLETE"
	LabelReview        Label = "REVIEW"
	LabelLowConfidence Label = "LOW_CONFIDENCE"
)

// ValidLabels is used when reviewers submit their own label.
var ValidLabels = map[Label]bool{
	LabelCorrect: true, LabelIncorrect: true, LabelIncomplete: true,
	LabelReview: true, LabelLowConfidence: true,
}

// Status is the processing status of an evaluation job.
type Status string

const (
	StatusPending    Status = "PENDING"
	StatusProcessing Status = "PROCESSING"
	StatusCompleted  Status = "COMPLETED"
	StatusFailed     Status = "FAILED"
)

// BaggageRecord is one checked bag with its expected (ground-truth) category.
type BaggageRecord struct {
	BaggageID        string    `json:"baggage_id"`
	FlightNumber     string    `json:"flight_number"`
	FlightDate       string    `json:"flight_date"` // YYYY-MM-DD
	Origin           string    `json:"origin"`      // IATA code, e.g. HYD
	Destination      string    `json:"destination"` // IATA code, e.g. DEL
	ImageRef         string    `json:"image_ref"`   // e.g. s3://bucket/images/BAG001.jpg
	ExpectedCategory Category  `json:"expected_category"`
	Timestamp        time.Time `json:"timestamp"`
	Status           string    `json:"status"` // operational status, e.g. CHECKED_IN
}

// Prediction is the AI model's output for one bag.
type Prediction struct {
	PredictionID      string            `json:"prediction_id"`
	BaggageID         string            `json:"baggage_id"`
	PredictedCategory Category          `json:"predicted_category"`
	Confidence        float64           `json:"confidence"`
	Attributes        map[string]string `json:"attributes"` // color, size, material, wheels...
	ModelName         string            `json:"model_name"`
	ModelVersion      string            `json:"model_version"`
	CreatedAt         time.Time         `json:"created_at"`
}

// Evaluation is the result of checking one prediction.
type Evaluation struct {
	ID                string    `json:"id"`
	PredictionID      string    `json:"prediction_id"`
	BaggageID         string    `json:"baggage_id"`
	ExpectedCategory  Category  `json:"expected_category,omitempty"`
	PredictedCategory Category  `json:"predicted_category,omitempty"`
	Confidence        float64   `json:"confidence"`
	Label             Label     `json:"label,omitempty"`
	Reason            string    `json:"reason,omitempty"`
	ErrorCategory     string    `json:"error_category,omitempty"`
	QualityScore      float64   `json:"quality_score"`
	ReviewerLabel     Label     `json:"reviewer_label,omitempty"` // latest reviewer label, if any
	Status            Status    `json:"status"`
	Attempts          int       `json:"attempts"`
	LastError         string    `json:"last_error,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// ReviewerFeedback is a human reviewer's opinion on an evaluation.
type ReviewerFeedback struct {
	ID              string    `json:"id"`
	EvaluationID    string    `json:"evaluation_id"`
	Reviewer        string    `json:"reviewer"`
	Label           Label     `json:"label"`
	CorrectCategory Category  `json:"correct_category,omitempty"` // reviewer-provided ground truth
	Comment         string    `json:"comment"`
	CreatedAt       time.Time `json:"created_at"`
}

// ProcessingError stores rejected input so it can be investigated later.
type ProcessingError struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`     // e.g. baggage_record, prediction, worker
	ErrorType string    `json:"error_type"` // VALIDATION, DUPLICATE, NOT_FOUND, INTERNAL
	Reference string    `json:"reference"`  // id of the item, if known
	Payload   string    `json:"payload"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// EvaluationJob is the message sent through the queue.
type EvaluationJob struct {
	EvaluationID string `json:"evaluation_id"`
	Attempt      int    `json:"attempt"`
}
