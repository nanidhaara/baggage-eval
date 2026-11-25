#!/usr/bin/env bash
# Sends the sample dataset to a running API and prints the results.
set -euo pipefail
API="${API:-http://localhost:8080}"

# The API only listens after Postgres and RabbitMQ are reachable; wait for it.
echo "==> Waiting for the API at $API ..."
for i in $(seq 1 60); do
  curl -sf "$API/healthz" >/dev/null && break
  [ "$i" = 60 ] && { echo "API not ready after 60s; check: docker compose logs api"; exit 1; }
  sleep 1
done

echo "==> Baggage records"
curl -s -X POST "$API/api/v1/baggage-records" -H 'Content-Type: application/json' \
  --data @testdata/baggage_records.json
echo
echo "==> Predictions"
curl -s -X POST "$API/api/v1/predictions" -H 'Content-Type: application/json' \
  --data @testdata/predictions.json
echo
echo "==> Waiting for the worker..."
sleep 3
echo "==> Quality report"
curl -s "$API/api/v1/reports/quality"
echo
