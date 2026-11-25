package validation

import (
	"testing"
	"time"

	"github.com/nanidhaara/baggage-eval/internal/domain"
)

func validRecord() domain.BaggageRecord {
	return domain.BaggageRecord{
		BaggageID: "BAG001", FlightNumber: "6E2134", FlightDate: "2026-11-20",
		Origin: "HYD", Destination: "DEL", ImageRef: "s3://bucket/BAG001.jpg",
		ExpectedCategory: domain.CategorySuitcase, Timestamp: time.Now().Add(-time.Hour), Status: "CHECKED_IN",
	}
}

func hasField(errs Errors, field string) bool {
	for _, e := range errs {
		if e.Field == field {
			return true
		}
	}
	return false
}

func TestValidateBaggageRecord(t *testing.T) {
	if errs := ValidateBaggageRecord(validRecord()); len(errs) != 0 {
		t.Fatalf("valid record rejected: %v", errs)
	}

	cases := map[string]func(*domain.BaggageRecord){
		"baggage_id":        func(r *domain.BaggageRecord) { r.BaggageID = "" },
		"flight_number":     func(r *domain.BaggageRecord) { r.FlightNumber = "flight-1" },
		"flight_date":       func(r *domain.BaggageRecord) { r.FlightDate = "20/11/2026" },
		"origin":            func(r *domain.BaggageRecord) { r.Origin = "HY" },
		"destination":       func(r *domain.BaggageRecord) { r.Destination = r.Origin },
		"image_ref":         func(r *domain.BaggageRecord) { r.ImageRef = "" },
		"expected_category": func(r *domain.BaggageRecord) { r.ExpectedCategory = "BRIEFCASE" },
		"timestamp":         func(r *domain.BaggageRecord) { r.Timestamp = time.Now().Add(48 * time.Hour) },
	}
	for field, mutate := range cases {
		t.Run(field, func(t *testing.T) {
			r := validRecord()
			mutate(&r)
			if errs := ValidateBaggageRecord(r); !hasField(errs, field) {
				t.Errorf("expected error on %s, got %v", field, errs)
			}
		})
	}
}

func TestValidatePrediction(t *testing.T) {
	p := domain.Prediction{PredictionID: "P1", BaggageID: "BAG001", PredictedCategory: domain.CategoryBackpack,
		Confidence: 0.9, ModelName: "bagnet", ModelVersion: "1.0"}
	if errs := ValidatePrediction(p); len(errs) != 0 {
		t.Fatalf("valid prediction rejected: %v", errs)
	}
	p.Confidence = 1.2
	if errs := ValidatePrediction(p); !hasField(errs, "confidence") {
		t.Errorf("expected confidence error, got %v", errs)
	}
}

func TestFindDuplicateIDs(t *testing.T) {
	got := FindDuplicateIDs([]string{"A", "B", "A", "C", "A", "B"})
	if len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Errorf("got %v, want [A B]", got)
	}
}
