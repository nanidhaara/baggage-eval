package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/nanidhaara/baggage-eval/internal/domain"
)

func ev(label domain.Label, exp, got domain.Category) domain.Evaluation {
	return domain.Evaluation{Status: domain.StatusCompleted, Label: label, ExpectedCategory: exp, PredictedCategory: got}
}

func TestBuild(t *testing.T) {
	evals := []domain.Evaluation{
		ev(domain.LabelCorrect, domain.CategorySuitcase, domain.CategorySuitcase),
		ev(domain.LabelCorrect, domain.CategorySuitcase, domain.CategorySuitcase),
		ev(domain.LabelIncorrect, domain.CategorySuitcase, domain.CategoryHardCase),
		ev(domain.LabelLowConfidence, domain.CategoryBackpack, domain.CategoryBackpack),
		{Status: domain.StatusPending},
		{Status: domain.StatusFailed},
	}
	evals[0].ReviewerLabel = domain.LabelIncorrect // reviewer disagrees

	r := Build(evals, []domain.ProcessingError{{ErrorType: "DUPLICATE"}, {ErrorType: "VALIDATION"}}, time.Now())

	check := func(name string, got, want any) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	check("total", r.TotalEvaluations, 6)
	check("completed", r.Completed, 4)
	check("pending", r.Pending, 1)
	check("failed", r.Failed, 1)
	check("accuracy", *r.Accuracy, 0.6667) // 2 correct / 3 decided
	check("error rate", r.ErrorRate, 0.25)
	check("low confidence rate", r.LowConfidenceRate, 0.25)
	check("disagreements", r.ReviewerDisagree, 1)
	check("duplicates", r.InputErrors["DUPLICATE"], 1)
	check("top confusion", r.TopConfusions[0].Predicted, domain.CategoryHardCase)

	var buf bytes.Buffer
	if err := WriteCSV(&buf, r); err != nil {
		t.Fatal(err)
	}
	for _, c := range r.ByCategory {
		if c.Category == domain.CategoryBackpack && c.Accuracy != nil {
			t.Errorf("BACKPACK has no correct/incorrect labels, accuracy should be nil, got %v", *c.Accuracy)
		}
	}
	if !strings.Contains(buf.String(), "BACKPACK,1,0,0,n/a") {
		t.Errorf("csv should show n/a for unmeasured category:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "SUITCASE,3,2,1,0.6667") {
		t.Errorf("csv missing suitcase row:\n%s", buf.String())
	}
}

func TestBuildEmpty(t *testing.T) {
	r := Build(nil, nil, time.Now())
	if r.Accuracy != nil || r.Completed != 0 {
		t.Errorf("empty report should be zero, got %+v", r)
	}
}
