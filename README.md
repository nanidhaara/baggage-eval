# AI-Assisted Baggage Data-Quality Evaluation Platform

A Go microservice project that checks AI-generated baggage classifications.
It ingests baggage records and model predictions, validates them, labels each
prediction (`CORRECT`, `INCORRECT`, `INCOMPLETE`, `REVIEW`, `LOW_CONFIDENCE`)
asynchronously through RabbitMQ, stores reviewer feedback, and produces
quality reports in JSON or CSV.

Independent, self-directed prototype.

## How this was built

I designed the system: the services, the evaluation rules and labels, and the
data model. The code was written with the help of Claude Code, an AI coding
assistant. I ran and verified everything on my own machine: unit tests, the
Ginkgo/Gomega functional suite, Postgres integration tests, and the full stack
in Docker with PostgreSQL and RabbitMQ.

## Architecture

```
  client ──► API service (cmd/api) ──► PostgreSQL
                 │   validate, store,       ▲
                 │   create PENDING         │
                 ▼   evaluation             │
            RabbitMQ  evaluation.jobs       │
                 │                          │
                 ▼                          │
         Worker service (cmd/worker) ───────┘
           PENDING → PROCESSING → COMPLETED
           temporary error → retry with backoff (max 3)
           permanent error / max attempts → FAILED + evaluation.jobs.dlq
```

## Project structure

```
baggage-eval/
├── cmd/
│   ├── api/main.go            # REST service entry point
│   └── worker/main.go         # queue consumer entry point
├── internal/
│   ├── domain/                # shared types: records, predictions, labels, statuses
│   ├── config/                # environment-based configuration
│   ├── validation/            # input validation rules
│   ├── evaluation/            # labelling rules (pure functions, no I/O)
│   ├── report/                # accuracy, error rates, per-category stats, CSV
│   ├── repository/            # Repository interface + Postgres + in-memory
│   ├── queue/                 # Publisher/Consumer interfaces + in-memory queue
│   │   └── rabbitmq/          # RabbitMQ implementation
│   ├── worker/                # job processing, retries, dead-lettering
│   └── api/                   # HTTP handlers and middleware
├── migrations/                # PostgreSQL schema
├── tests/                     # Ginkgo/Gomega functional tests
├── testdata/                  # sample baggage records and AI predictions
├── docs/
│   ├── annotation-guidelines.md
│   └── aws-deployment.md
├── scripts/seed.sh
├── Dockerfile
├── docker-compose.yml
├── Makefile
└── .env.example
```

## Getting started

Requirements: Go 1.22+, Docker Desktop, `make`, `curl`.

```bash
cp .env.example .env
make deps            # go mod tidy: downloads modules and creates go.sum
make test            # unit + functional tests (no Docker needed)
make up              # Postgres, RabbitMQ, API, 2 workers
make seed            # send the sample dataset (waits for the API)
make report          # quality report as CSV
```

RabbitMQ management UI: http://localhost:15672 (guest / guest).

On Windows without `make`: `docker compose up -d --build`, then
`powershell -ExecutionPolicy Bypass -File scripts\seed.ps1`.

Per-category `accuracy` is `null` when a category has no CORRECT or INCORRECT
labels yet (only REVIEW, LOW_CONFIDENCE or INCOMPLETE), meaning "not measured", not 0%.

To run the Go services outside Docker (easier for debugging in VS Code):

```bash
make up-deps         # only Postgres + RabbitMQ
make run-api         # terminal 1
make run-worker      # terminal 2
```

## API

| Method | Path | Purpose |
|---|---|---|
| GET | `/healthz` | Health check |
| POST | `/api/v1/baggage-records` | Ingest a JSON array of baggage records |
| POST | `/api/v1/predictions` | Ingest a JSON array of AI predictions; queues evaluations |
| GET | `/api/v1/evaluations?status=&label=&limit=` | List evaluations |
| GET | `/api/v1/evaluations/{id}` | One evaluation with label, reason, quality score |
| POST | `/api/v1/evaluations/{id}/feedback` | Reviewer feedback; `correct_category` triggers re-evaluation |
| POST | `/api/v1/evaluations/{id}/requeue` | Re-publish a PENDING or FAILED evaluation |
| GET | `/api/v1/reports/quality?format=json\|csv` | Quality metrics |

Example feedback:

```bash
curl -X POST localhost:8080/api/v1/evaluations/<id>/feedback \
  -H 'Content-Type: application/json' \
  -d '{"reviewer":"sthuthi","label":"INCORRECT","correct_category":"HARD_CASE","comment":"aluminium shell case"}'
```

Ingestion endpoints accept batches. Each item is accepted or rejected
individually; rejected items (validation errors, duplicates, unknown bag ids)
are returned in the response and stored in `processing_errors`.

## Design decisions

- **Interfaces for storage and queue** so the API and worker are tested
  without Docker (in-memory implementations), and Postgres/RabbitMQ are swappable
  (e.g. Google Pub/Sub could implement the same `queue.Publisher`/`Consumer`).
- **Idempotent worker**: a duplicate message for a COMPLETED evaluation is skipped.
- **Retries**: temporary errors are retried with exponential backoff
  (`RETRY_BACKOFF` × 1, 2, 4…); missing data is a permanent failure and goes
  straight to the dead-letter queue.
- **Rules as pure functions** in `internal/evaluation`, documented in
  `docs/annotation-guidelines.md`, so decisions are reproducible.
- **Structured JSON logs** (`log/slog`) with request duration and evaluation ids.
- **Database constraints**: foreign keys, unique prediction per evaluation,
  status/confidence check constraints, indexes on baggage id, status, label,
  category and created time.

## Tests

| Command | What it runs | Needs Docker |
|---|---|---|
| `go test -cover ./internal/...` | Unit tests | No |
| `go test -v ./tests/...` | Ginkgo/Gomega functional suite | No |
| `go test -tags integration -v ./internal/repository/` | Postgres repository against the real database | Yes (`docker compose up -d postgres`) |

- `internal/**/_test.go` — Go table-driven unit tests: validation, every rule
  branch and threshold boundary, report maths, retry/dead-letter/idempotency,
  HTTP handlers.
- `tests/` — Ginkgo/Gomega functional suite running the whole pipeline on the
  sample dataset: labels, report numbers, reviewer re-evaluation, temporary
  database failure, duplicate batches.
- `internal/repository/postgres_integration_test.go` — runs the Postgres
  repository against the real database: round trips, duplicate and foreign-key
  errors, filters, feedback transaction, ground truth lookup.
