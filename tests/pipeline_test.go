// Functional tests: the full pipeline (API -> queue -> worker -> report)
// running in-process with the in-memory repository and queue.
// No Docker needed: run with `make test-functional`.
package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nanidhaara/baggage-eval/internal/api"
	"github.com/nanidhaara/baggage-eval/internal/domain"
	"github.com/nanidhaara/baggage-eval/internal/evaluation"
	"github.com/nanidhaara/baggage-eval/internal/queue"
	"github.com/nanidhaara/baggage-eval/internal/report"
	"github.com/nanidhaara/baggage-eval/internal/repository"
	"github.com/nanidhaara/baggage-eval/internal/worker"
)

var _ = Describe("Evaluation pipeline", func() {
	var (
		ctx    context.Context
		repo   *repository.Memory
		q      *queue.Memory
		srv    *httptest.Server
		proc   *worker.Processor
		logger *slog.Logger
	)

	post := func(path string, body []byte) api.IngestResponse {
		resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(body))
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var out api.IngestResponse
		Expect(json.NewDecoder(resp.Body).Decode(&out)).To(Succeed())
		return out
	}

	getJSON := func(path string, into any) int {
		resp, err := http.Get(srv.URL + path)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		if into != nil {
			Expect(json.NewDecoder(resp.Body).Decode(into)).To(Succeed())
		}
		return resp.StatusCode
	}

	loadFile := func(name string) []byte {
		b, err := os.ReadFile("../testdata/" + name)
		Expect(err).NotTo(HaveOccurred())
		return b
	}

	BeforeEach(func() {
		ctx = context.Background()
		repo = repository.NewMemory()
		q = queue.NewMemory()
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
		s := &api.Server{Repo: repo, Publisher: q, Logger: logger, Now: time.Now}
		srv = httptest.NewServer(s.Routes())
		proc = &worker.Processor{
			Repo: repo, Publisher: q, Thresholds: evaluation.DefaultThresholds,
			MaxAttempts: 3, Backoff: time.Millisecond, Logger: logger,
			Now: time.Now, Sleep: func(time.Duration) {},
		}
	})

	AfterEach(func() { srv.Close() })

	Context("with the sample dataset in testdata/", func() {
		var predResp api.IngestResponse

		BeforeEach(func() {
			recResp := post("/api/v1/baggage-records", loadFile("baggage_records.json"))
			Expect(recResp.Accepted).To(HaveLen(8))
			Expect(recResp.Rejected).To(HaveLen(2)) // one duplicate, one invalid

			predResp = post("/api/v1/predictions", loadFile("predictions.json"))
			Expect(predResp.Accepted).To(HaveLen(8))
			Expect(q.Jobs).To(HaveLen(8))

			Expect(q.Consume(ctx, proc.Handle)).To(Succeed())
		})

		It("completes every evaluation", func() {
			evals, _ := repo.ListEvaluations(ctx, repository.EvaluationFilter{})
			for _, e := range evals {
				Expect(e.Status).To(Equal(domain.StatusCompleted), "evaluation %s", e.ID)
				Expect(e.Reason).NotTo(BeEmpty())
			}
		})

		It("assigns each expected label", func() {
			labels := map[string]domain.Label{}
			evals, _ := repo.ListEvaluations(ctx, repository.EvaluationFilter{})
			for _, e := range evals {
				labels[e.PredictionID] = e.Label
			}
			Expect(labels).To(Equal(map[string]domain.Label{
				"PRED001": domain.LabelCorrect,
				"PRED002": domain.LabelCorrect,
				"PRED003": domain.LabelIncorrect,
				"PRED004": domain.LabelIncomplete,
				"PRED005": domain.LabelLowConfidence,
				"PRED006": domain.LabelReview,
				"PRED007": domain.LabelIncorrect,
				"PRED008": domain.LabelCorrect,
			}))
		})

		It("produces a quality report", func() {
			var rep report.QualityReport
			Expect(getJSON("/api/v1/reports/quality", &rep)).To(Equal(http.StatusOK))
			Expect(rep.Completed).To(Equal(8))
			Expect(rep.LabelCounts[domain.LabelCorrect]).To(Equal(3))
			Expect(rep.LabelCounts[domain.LabelIncorrect]).To(Equal(2))
			Expect(rep.Accuracy).NotTo(BeNil())
			Expect(*rep.Accuracy).To(BeNumerically("~", 0.6, 0.0001))
			for _, c := range rep.ByCategory {
				if c.Category == domain.CategoryGolfBag {
					Expect(c.Accuracy).To(BeNil(), "GOLF_BAG was only REVIEW, so accuracy is not measurable")
				}
			}
			Expect(rep.InputErrors).To(HaveKeyWithValue("DUPLICATE", 1))
			Expect(rep.InputErrors).To(HaveKeyWithValue("VALIDATION", 1))
		})

		It("re-evaluates when a reviewer supplies the correct category", func() {
			evals, _ := repo.ListEvaluations(ctx, repository.EvaluationFilter{Label: domain.LabelIncorrect})
			target := evals[0]

			body, _ := json.Marshal(api.FeedbackRequest{
				Reviewer: "sthuthi", Label: domain.LabelCorrect,
				CorrectCategory: target.PredictedCategory, Comment: "image shows a hard case; record was wrong",
			})
			resp, err := http.Post(srv.URL+"/api/v1/evaluations/"+target.ID+"/feedback", "application/json", bytes.NewReader(body))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
			resp.Body.Close()

			Expect(q.Consume(ctx, proc.Handle)).To(Succeed())
			updated, _ := repo.GetEvaluation(ctx, target.ID)
			Expect(updated.Label).To(Equal(domain.LabelCorrect))
			Expect(updated.ReviewerLabel).To(Equal(domain.LabelCorrect))
		})
	})

	Context("when the database fails temporarily", func() {
		It("retries and then completes", func() {
			post("/api/v1/baggage-records", loadFile("baggage_records.json"))
			post("/api/v1/predictions", loadFile("predictions.json"))
			repo.FailNext = errors.New("connection refused")

			Expect(q.Consume(ctx, proc.Handle)).To(Succeed())
			Expect(q.DeadLetters).To(BeEmpty())
			evals, _ := repo.ListEvaluations(ctx, repository.EvaluationFilter{Status: domain.StatusCompleted})
			Expect(evals).To(HaveLen(8))
		})
	})

	Context("when the same prediction batch is sent twice", func() {
		It("rejects the duplicates and does not queue new jobs", func() {
			post("/api/v1/baggage-records", loadFile("baggage_records.json"))
			post("/api/v1/predictions", loadFile("predictions.json"))
			second := post("/api/v1/predictions", loadFile("predictions.json"))
			Expect(second.Accepted).To(BeEmpty())
			Expect(second.Rejected).To(HaveLen(8))
			Expect(second.Rejected[0].Reason).To(Equal("DUPLICATE"))
			Expect(q.Jobs).To(HaveLen(8))
		})
	})
})
