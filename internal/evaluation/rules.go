// Package evaluation contains the rules that label an AI prediction.
// It has no database or queue code, so it is easy to unit test.
package evaluation

import (
	"fmt"
	"strings"

	"github.com/nanidhaara/baggage-eval/internal/domain"
)

// RequiredAttributes must be present in every prediction.
var RequiredAttributes = []string{"color", "size"}

// Error categories recorded on each evaluation.
const (
	ErrMissingAttributes  = "MISSING_ATTRIBUTES"
	ErrLowConfidence      = "LOW_CONFIDENCE"
	ErrNoGroundTruth      = "NO_GROUND_TRUTH"
	ErrBorderline         = "BORDERLINE_CONFIDENCE"
	ErrWrongCategory      = "WRONG_CATEGORY"
	ErrHighConfidenceMiss = "HIGH_CONFIDENCE_MISCLASSIFICATION"
)

// Thresholds controls how confidence scores are interpreted.
type Thresholds struct {
	LowConfidence float64 // below -> LOW_CONFIDENCE
	Review        float64 // correct but below -> REVIEW
	High          float64 // wrong and at or above -> high-confidence miss
}

// DefaultThresholds are used when nothing is configured.
var DefaultThresholds = Thresholds{LowConfidence: 0.50, Review: 0.75, High: 0.90}

// Input is everything the rules need for one decision.
type Input struct {
	Prediction domain.Prediction
	// GroundTruth is the reviewer-provided category if present,
	// otherwise the expected category from the baggage record.
	GroundTruth domain.Category
}

// Result is the decision for one prediction.
type Result struct {
	Label         domain.Label
	Reason        string
	ErrorCategory string
	QualityScore  float64 // 0.0 - 1.0
}

// Evaluate applies the rules in a fixed order. The first rule that
// matches wins, so the order below is part of the annotation guidelines.
func Evaluate(in Input, t Thresholds) Result {
	p := in.Prediction

	// Rule 1: required attributes must be present.
	if missing := missingAttributes(p.Attributes); len(missing) > 0 {
		return Result{
			Label:         domain.LabelIncomplete,
			Reason:        "missing required attributes: " + strings.Join(missing, ", "),
			ErrorCategory: ErrMissingAttributes,
			QualityScore:  0.2,
		}
	}

	// Rule 2: very low confidence predictions are not trusted.
	if p.Confidence < t.LowConfidence {
		return Result{
			Label:         domain.LabelLowConfidence,
			Reason:        fmt.Sprintf("confidence %.2f is below %.2f", p.Confidence, t.LowConfidence),
			ErrorCategory: ErrLowConfidence,
			QualityScore:  0.3,
		}
	}

	// Rule 3: without ground truth a human must decide.
	if in.GroundTruth == "" {
		return Result{
			Label:         domain.LabelReview,
			Reason:        "no expected label or reviewer ground truth available",
			ErrorCategory: ErrNoGroundTruth,
			QualityScore:  0.5,
		}
	}

	// Rule 4: category matches.
	if p.PredictedCategory == in.GroundTruth {
		if p.Confidence < t.Review {
			return Result{
				Label: domain.LabelReview,
				Reason: fmt.Sprintf("category matches but confidence %.2f is below review threshold %.2f",
					p.Confidence, t.Review),
				ErrorCategory: ErrBorderline,
				QualityScore:  0.6,
			}
		}
		return Result{
			Label:        domain.LabelCorrect,
			Reason:       fmt.Sprintf("predicted %s matches ground truth", p.PredictedCategory),
			QualityScore: 1.0,
		}
	}

	// Rule 5: category does not match.
	errCat := ErrWrongCategory
	if p.Confidence >= t.High {
		errCat = ErrHighConfidenceMiss
	}
	return Result{
		Label: domain.LabelIncorrect,
		Reason: fmt.Sprintf("predicted %s but ground truth is %s (confidence %.2f)",
			p.PredictedCategory, in.GroundTruth, p.Confidence),
		ErrorCategory: errCat,
		QualityScore:  0.0,
	}
}

func missingAttributes(attrs map[string]string) []string {
	var missing []string
	for _, a := range RequiredAttributes {
		if strings.TrimSpace(attrs[a]) == "" {
			missing = append(missing, a)
		}
	}
	return missing
}
