package evaluation

import (
	"testing"

	"github.com/nanidhaara/baggage-eval/internal/domain"
)

func pred(cat domain.Category, conf float64, attrs map[string]string) domain.Prediction {
	return domain.Prediction{PredictionID: "P1", BaggageID: "B1", PredictedCategory: cat, Confidence: conf, Attributes: attrs}
}

var fullAttrs = map[string]string{"color": "black", "size": "large"}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name      string
		in        Input
		wantLabel domain.Label
		wantErr   string
	}{
		{"correct", Input{pred(domain.CategorySuitcase, 0.95, fullAttrs), domain.CategorySuitcase}, domain.LabelCorrect, ""},
		{"incorrect", Input{pred(domain.CategoryBackpack, 0.80, fullAttrs), domain.CategorySuitcase}, domain.LabelIncorrect, ErrWrongCategory},
		{"high confidence miss", Input{pred(domain.CategoryBackpack, 0.97, fullAttrs), domain.CategorySuitcase}, domain.LabelIncorrect, ErrHighConfidenceMiss},
		{"missing size", Input{pred(domain.CategorySuitcase, 0.95, map[string]string{"color": "red"}), domain.CategorySuitcase}, domain.LabelIncomplete, ErrMissingAttributes},
		{"nil attributes", Input{pred(domain.CategorySuitcase, 0.95, nil), domain.CategorySuitcase}, domain.LabelIncomplete, ErrMissingAttributes},
		{"low confidence", Input{pred(domain.CategorySuitcase, 0.30, fullAttrs), domain.CategorySuitcase}, domain.LabelLowConfidence, ErrLowConfidence},
		{"borderline correct", Input{pred(domain.CategorySuitcase, 0.60, fullAttrs), domain.CategorySuitcase}, domain.LabelReview, ErrBorderline},
		{"no ground truth", Input{pred(domain.CategorySuitcase, 0.95, fullAttrs), ""}, domain.LabelReview, ErrNoGroundTruth},
		{"incomplete wins over low confidence", Input{pred(domain.CategorySuitcase, 0.10, nil), domain.CategorySuitcase}, domain.LabelIncomplete, ErrMissingAttributes},
		{"exactly at review threshold is correct", Input{pred(domain.CategorySuitcase, 0.75, fullAttrs), domain.CategorySuitcase}, domain.LabelCorrect, ""},
		{"exactly at low threshold is not low confidence", Input{pred(domain.CategoryBackpack, 0.50, fullAttrs), domain.CategorySuitcase}, domain.LabelIncorrect, ErrWrongCategory},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(tc.in, DefaultThresholds)
			if got.Label != tc.wantLabel {
				t.Errorf("label = %s, want %s (reason: %s)", got.Label, tc.wantLabel, got.Reason)
			}
			if got.ErrorCategory != tc.wantErr {
				t.Errorf("error category = %q, want %q", got.ErrorCategory, tc.wantErr)
			}
			if got.Reason == "" {
				t.Error("reason must always be set")
			}
		})
	}
}
