// Package validation checks incoming baggage records and AI predictions
// before anything is stored.
package validation

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/nanidhaara/baggage-eval/internal/domain"
)

// FieldError describes one problem with one field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e FieldError) Error() string { return e.Field + ": " + e.Message }

// Errors is a list of field errors.
type Errors []FieldError

func (es Errors) Error() string {
	parts := make([]string, len(es))
	for i, e := range es {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "; ")
}

var (
	flightNumberRe = regexp.MustCompile(`^[A-Z0-9]{2}[0-9]{1,4}$`) // e.g. 6E2134, AI101
	iataRe         = regexp.MustCompile(`^[A-Z]{3}$`)
	idRe           = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// Now is replaceable in tests.
var Now = time.Now

// ValidateBaggageRecord returns nil when the record is valid.
func ValidateBaggageRecord(r domain.BaggageRecord) Errors {
	var errs Errors
	add := func(field, msg string) { errs = append(errs, FieldError{field, msg}) }

	if r.BaggageID == "" {
		add("baggage_id", "is required")
	} else if !idRe.MatchString(r.BaggageID) {
		add("baggage_id", "must be 1-64 letters, digits, '-' or '_'")
	}
	if r.FlightNumber == "" {
		add("flight_number", "is required")
	} else if !flightNumberRe.MatchString(r.FlightNumber) {
		add("flight_number", "must look like AI101 or 6E2134")
	}
	if r.FlightDate == "" {
		add("flight_date", "is required")
	} else if _, err := time.Parse("2006-01-02", r.FlightDate); err != nil {
		add("flight_date", "must be in YYYY-MM-DD format")
	}
	if !iataRe.MatchString(r.Origin) {
		add("origin", "must be a 3-letter IATA airport code")
	}
	if !iataRe.MatchString(r.Destination) {
		add("destination", "must be a 3-letter IATA airport code")
	}
	if r.Origin != "" && r.Origin == r.Destination {
		add("destination", "must be different from origin")
	}
	if r.ImageRef == "" {
		add("image_ref", "is required")
	}
	if r.ExpectedCategory == "" {
		add("expected_category", "is required")
	} else if !domain.SupportedCategories[r.ExpectedCategory] {
		add("expected_category", fmt.Sprintf("unsupported category %q", r.ExpectedCategory))
	}
	if r.Timestamp.IsZero() {
		add("timestamp", "is required")
	} else if r.Timestamp.After(Now().Add(5 * time.Minute)) {
		add("timestamp", "cannot be in the future")
	}
	return errs
}

// ValidatePrediction returns nil when the prediction is valid.
func ValidatePrediction(p domain.Prediction) Errors {
	var errs Errors
	add := func(field, msg string) { errs = append(errs, FieldError{field, msg}) }

	if p.PredictionID == "" {
		add("prediction_id", "is required")
	} else if !idRe.MatchString(p.PredictionID) {
		add("prediction_id", "must be 1-64 letters, digits, '-' or '_'")
	}
	if p.BaggageID == "" {
		add("baggage_id", "is required")
	}
	if p.PredictedCategory == "" {
		add("predicted_category", "is required")
	} else if !domain.SupportedCategories[p.PredictedCategory] {
		add("predicted_category", fmt.Sprintf("unsupported category %q", p.PredictedCategory))
	}
	if p.Confidence < 0 || p.Confidence > 1 {
		add("confidence", "must be between 0 and 1")
	}
	if p.ModelName == "" {
		add("model_name", "is required")
	}
	if p.ModelVersion == "" {
		add("model_version", "is required")
	}
	return errs
}

// FindDuplicateIDs returns ids that appear more than once in a batch.
func FindDuplicateIDs(ids []string) []string {
	seen := map[string]int{}
	var dups []string
	for _, id := range ids {
		seen[id]++
		if seen[id] == 2 {
			dups = append(dups, id)
		}
	}
	return dups
}
