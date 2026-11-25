// Package report turns evaluation results into quality metrics.
package report

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/nanidhaara/baggage-eval/internal/domain"
)

// CategoryStats is per expected-category performance.
type CategoryStats struct {
	Category  domain.Category `json:"category"`
	Total     int             `json:"total"`
	Correct   int             `json:"correct"`
	Incorrect int             `json:"incorrect"`
	Accuracy  *float64        `json:"accuracy"` // null when no CORRECT/INCORRECT labels (not measurable)
}

// Confusion counts how often one category was predicted as another.
type Confusion struct {
	Expected  domain.Category `json:"expected"`
	Predicted domain.Category `json:"predicted"`
	Count     int             `json:"count"`
}

// QualityReport is the full report returned by the API.
type QualityReport struct {
	GeneratedAt       time.Time            `json:"generated_at"`
	TotalEvaluations  int                  `json:"total_evaluations"`
	Completed         int                  `json:"completed"`
	Pending           int                  `json:"pending"`
	Failed            int                  `json:"failed"`
	LabelCounts       map[domain.Label]int `json:"label_counts"`
	Accuracy          *float64             `json:"accuracy"`            // correct / (correct + incorrect); null if none
	ErrorRate         float64              `json:"error_rate"`          // incorrect / completed
	LowConfidenceRate float64              `json:"low_confidence_rate"` // low_confidence / completed
	IncompleteRate    float64              `json:"incomplete_rate"`
	ReviewRate        float64              `json:"review_rate"`
	AverageQuality    float64              `json:"average_quality_score"`
	ErrorCategories   map[string]int       `json:"error_categories"`
	ByCategory        []CategoryStats      `json:"by_category"`
	TopConfusions     []Confusion          `json:"top_confusions"`
	ReviewerDisagree  int                  `json:"reviewer_disagreements"` // reviewer label != automatic label
	InputErrors       map[string]int       `json:"input_errors"`           // processing errors by type (VALIDATION, DUPLICATE...)
}

// Build computes the report. Only COMPLETED evaluations count toward rates.
func Build(evals []domain.Evaluation, procErrors []domain.ProcessingError, now time.Time) QualityReport {
	r := QualityReport{
		GeneratedAt:     now,
		LabelCounts:     map[domain.Label]int{},
		ErrorCategories: map[string]int{},
		InputErrors:     map[string]int{},
	}
	cats := map[domain.Category]*CategoryStats{}
	conf := map[[2]domain.Category]int{}
	var qualitySum float64

	for _, e := range evals {
		r.TotalEvaluations++
		switch e.Status {
		case domain.StatusPending, domain.StatusProcessing:
			r.Pending++
			continue
		case domain.StatusFailed:
			r.Failed++
			continue
		}
		r.Completed++
		r.LabelCounts[e.Label]++
		qualitySum += e.QualityScore
		if e.ErrorCategory != "" {
			r.ErrorCategories[e.ErrorCategory]++
		}
		if e.ReviewerLabel != "" && e.ReviewerLabel != e.Label {
			r.ReviewerDisagree++
		}

		if e.ExpectedCategory != "" {
			cs := cats[e.ExpectedCategory]
			if cs == nil {
				cs = &CategoryStats{Category: e.ExpectedCategory}
				cats[e.ExpectedCategory] = cs
			}
			cs.Total++
			switch e.Label {
			case domain.LabelCorrect:
				cs.Correct++
			case domain.LabelIncorrect:
				cs.Incorrect++
				conf[[2]domain.Category{e.ExpectedCategory, e.PredictedCategory}]++
			}
		}
	}

	for _, pe := range procErrors {
		r.InputErrors[pe.ErrorType]++
	}

	correct, incorrect := r.LabelCounts[domain.LabelCorrect], r.LabelCounts[domain.LabelIncorrect]
	r.Accuracy = ratioPtr(correct, correct+incorrect)
	r.ErrorRate = ratio(incorrect, r.Completed)
	r.LowConfidenceRate = ratio(r.LabelCounts[domain.LabelLowConfidence], r.Completed)
	r.IncompleteRate = ratio(r.LabelCounts[domain.LabelIncomplete], r.Completed)
	r.ReviewRate = ratio(r.LabelCounts[domain.LabelReview], r.Completed)
	if r.Completed > 0 {
		r.AverageQuality = round(qualitySum / float64(r.Completed))
	}

	for _, cs := range cats {
		cs.Accuracy = ratioPtr(cs.Correct, cs.Correct+cs.Incorrect)
		r.ByCategory = append(r.ByCategory, *cs)
	}
	sort.Slice(r.ByCategory, func(i, j int) bool { return r.ByCategory[i].Category < r.ByCategory[j].Category })

	for k, n := range conf {
		r.TopConfusions = append(r.TopConfusions, Confusion{Expected: k[0], Predicted: k[1], Count: n})
	}
	sort.Slice(r.TopConfusions, func(i, j int) bool {
		if r.TopConfusions[i].Count != r.TopConfusions[j].Count {
			return r.TopConfusions[i].Count > r.TopConfusions[j].Count
		}
		return r.TopConfusions[i].Expected < r.TopConfusions[j].Expected
	})
	if len(r.TopConfusions) > 5 {
		r.TopConfusions = r.TopConfusions[:5]
	}
	return r
}

// WriteCSV writes the per-category table plus a summary row.
func WriteCSV(w io.Writer, r QualityReport) error {
	cw := csv.NewWriter(w)
	rows := [][]string{{"category", "total", "correct", "incorrect", "accuracy"}}
	for _, c := range r.ByCategory {
		rows = append(rows, []string{string(c.Category), itoa(c.Total), itoa(c.Correct), itoa(c.Incorrect), ptoa(c.Accuracy)})
	}
	rows = append(rows,
		[]string{},
		[]string{"metric", "value"},
		[]string{"total_evaluations", itoa(r.TotalEvaluations)},
		[]string{"completed", itoa(r.Completed)},
		[]string{"accuracy", ptoa(r.Accuracy)},
		[]string{"error_rate", ftoa(r.ErrorRate)},
		[]string{"low_confidence_rate", ftoa(r.LowConfidenceRate)},
		[]string{"incomplete_rate", ftoa(r.IncompleteRate)},
		[]string{"review_rate", ftoa(r.ReviewRate)},
		[]string{"reviewer_disagreements", itoa(r.ReviewerDisagree)},
	)
	if err := cw.WriteAll(rows); err != nil {
		return err
	}
	return cw.Error()
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return round(float64(a) / float64(b))
}

// ratioPtr returns nil when b is 0, so "not measured" is not shown as 0%.
func ratioPtr(a, b int) *float64 {
	if b == 0 {
		return nil
	}
	v := ratio(a, b)
	return &v
}

func ptoa(f *float64) string {
	if f == nil {
		return "n/a"
	}
	return ftoa(*f)
}

func round(f float64) float64 { return float64(int(f*10000+0.5)) / 10000 }
func itoa(i int) string       { return fmt.Sprintf("%d", i) }
func ftoa(f float64) string   { return fmt.Sprintf("%.4f", f) }
