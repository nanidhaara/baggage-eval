.PHONY: help deps build fmt vet test test-unit test-race test-integration test-functional cover run-api run-worker up-deps up down reset logs seed report clean

help: ## Show available commands
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'

deps: ## Download Go modules (run this first)
	go mod tidy

build: ## Build api and worker binaries into bin/
	go build -o bin/api ./cmd/api
	go build -o bin/worker ./cmd/worker

fmt: ## Format code
	gofmt -w .

vet: ## Static checks
	go vet ./...

test: test-unit test-functional ## Run all tests

test-unit: ## Go unit tests inside internal/
	go test -cover ./internal/...

test-race: ## Unit tests with the race detector (needs gcc/cgo on Windows)
	go test -race ./internal/...

test-functional: ## Ginkgo/Gomega functional tests
	go test -v ./tests/...

test-integration: ## Postgres repository tests (needs: docker compose up -d postgres)
	go test -tags integration -v ./internal/repository/

cover: ## HTML coverage report
	go test -coverprofile=coverage.out ./internal/... && go tool cover -html=coverage.out -o coverage.html

run-api: ## Run the API locally (needs `make up-deps`)
	go run ./cmd/api

run-worker: ## Run a worker locally
	go run ./cmd/worker

up-deps: ## Start only Postgres and RabbitMQ
	docker compose up -d postgres rabbitmq

up: ## Start everything with Docker Compose
	docker compose up -d --build

down: ## Stop everything (keeps data)
	docker compose down

reset: ## Stop everything and delete the database volume
	docker compose down -v

logs: ## Follow api and worker logs
	docker compose logs -f api worker

seed: ## Send the sample dataset to the running API
	./scripts/seed.sh

report: ## Download the quality report as CSV
	mkdir -p reports && curl -s "http://localhost:8080/api/v1/reports/quality?format=csv" -o reports/quality_report.csv && cat reports/quality_report.csv

clean:
	rm -rf bin coverage.out coverage.html reports
