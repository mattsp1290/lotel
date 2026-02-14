#!/usr/bin/env bash
set -euo pipefail

if [ ! -d ".beads" ]; then
  bd init
fi

echo "Creating CLI refactor (DuckDB) task graph..."

# analysis roots (P0)
ANALYZE_CLI=$(bd create "Analyze current lotel CLI command structure and Docker-coupled start/stop behavior" -p 0 --label analysis --silent)
ANALYZE_COLLECTOR=$(bd create "Analyze collector config semantics, health endpoint behavior, and runtime assumptions" -p 0 --label analysis --silent)
ANALYZE_STORAGE=$(bd create "Analyze current telemetry disk outputs and define DuckDB-backed signal/service/date partition contract under \$HOME/.lotel/data" -p 0 --label analysis --silent)
ANALYZE_VERIFY=$(bd create "Analyze Docker-era verification/test assets and define replacement scope for Python end-to-end flow" -p 0 --label analysis --silent)
ANALYZE_RISKS=$(bd create "Analyze risks: subprocess lifecycle, stale PID state, metric temporality conversion, and file/version compatibility" -p 0 --label analysis --silent)

# early decision
DECIDE_DUCKDB=$(bd create "Finalize DuckDB embedding plan (driver/build constraints, schema boundaries, and migration notes)" -p 0 --label storage --silent)

# process + runtime implementation
PROC_LIFECYCLE=$(bd create "Implement collector subprocess lifecycle in CLI (start/stop/status) with crash-safe PID/state handling" -p 0 --label process --silent)
PROC_HEALTH=$(bd create "Implement health/readiness checks using collector health endpoint; fail readiness unless endpoint passes" -p 0 --label process --silent)
CMD_ENTRYPOINT=$(bd create "Add clear collector runtime entrypoint under cmd while keeping lotel CLI coherent for coding-agent usage" -p 1 --label process --silent)
PROC_RECOVERY=$(bd create "Implement stale lock/PID recovery and restart-safe behavior for interrupted collector sessions" -p 1 --label process --silent)

# storage + indexing implementation
STORAGE_LAYOUT=$(bd create "Implement signal/service/date partition manager at \$HOME/.lotel/data/<signal>/service.name=<svc>/YYYY/MM/DD" -p 1 --label storage --silent)
STORAGE_SCHEMA_TRACES=$(bd create "Implement DuckDB traces schema + indexes for service/time/trace lookup with deterministic ordering" -p 1 --label storage --silent)
STORAGE_SCHEMA_METRICS=$(bd create "Implement DuckDB metrics schema + indexes including temporality/type fields required for window aggregation" -p 1 --label storage --silent)
STORAGE_SCHEMA_LOGS=$(bd create "Implement DuckDB logs schema + indexes for service/time querying with structured attribute support" -p 1 --label storage --silent)
INGEST_TRACES=$(bd create "Implement traces ingestion from collector disk artifacts into traces partitions with idempotent writes" -p 1 --label storage --silent)
INGEST_METRICS=$(bd create "Implement metrics ingestion into metrics partitions preserving temporality and conversion metadata" -p 1 --label storage --silent)
INGEST_LOGS=$(bd create "Implement logs ingestion into logs partitions preserving timestamp/severity/resource attributes" -p 1 --label storage --silent)

# query UX implementation
QUERY_CONTRACT=$(bd create "Define and implement machine-readable query contract (JSON default, stable ordering, explicit exit codes, --output human optional)" -p 1 --label query --silent)
QUERY_TRACES=$(bd create "Implement traces query command with service.name first-class filtering and absolute/relative time windows" -p 1 --label query --silent)
QUERY_METRICS=$(bd create "Implement metrics query command with service.name filtering and absolute/relative time windows" -p 1 --label query --silent)
QUERY_LOGS=$(bd create "Implement logs query command with service.name filtering and absolute/relative time windows" -p 1 --label query --silent)
METRICS_AGG=$(bd create "Implement metrics avg|min|max aggregation with standardized temporality conversion rules and structured warnings" -p 0 --label query --silent)

# prune implementation
PRUNE_DRY_RUN=$(bd create "Implement prune planner with --older-than (hours/days) and dry-run reporting by partition/file/bytes" -p 1 --label prune --silent)
PRUNE_EXECUTE=$(bd create "Implement safe prune execution and reporting for telemetry partitions older than age threshold" -p 1 --label prune --silent)

# verification
VERIFY_OTLP_DIRECT=$(bd create "Add verification for direct OTLP ingestion (trace+metric+log) using UUID service.name and query assertions" -p 0 --label verify --silent)
VERIFY_TELEMETRYGEN=$(bd create "Add telemetrygen ingestion smoke validation for traces/metrics/logs as blocking verification" -p 0 --label verify --silent)
VERIFY_PYTHON_E2E=$(bd create "Replace Docker-centric verification with Python end-to-end flow (start->ingest->query->prune)" -p 0 --label verify --silent)
VERIFY_TESTS=$(bd create "Add Go/Python tests for subprocess health, query determinism, metrics aggregation edge cases, and prune safety" -p 1 --label verify --silent)

# cleanup + docs
CLEANUP_DOCKER=$(bd create "Remove Docker/compose/spin/signoz runtime paths and retire Docker-specific verification scripts (outside docs)" -p 2 --label cleanup --silent)
DOCS_UPDATE=$(bd create "Update docs for local collector workflow (start/stop/query/prune), JSON default output, and DuckDB troubleshooting" -p 2 --label docs --silent)
POLISH=$(bd create "Post-migration cleanup/polish: remove dead code paths, tighten errors, and improve operator messages" -p 3 --label cleanup --silent)

# dependencies
bd dep add "$DECIDE_DUCKDB" "$ANALYZE_STORAGE"
bd dep add "$DECIDE_DUCKDB" "$ANALYZE_RISKS"

bd dep add "$PROC_LIFECYCLE" "$ANALYZE_CLI"
bd dep add "$PROC_LIFECYCLE" "$ANALYZE_COLLECTOR"
bd dep add "$PROC_HEALTH" "$ANALYZE_COLLECTOR"
bd dep add "$PROC_HEALTH" "$PROC_LIFECYCLE"
bd dep add "$CMD_ENTRYPOINT" "$ANALYZE_CLI"
bd dep add "$PROC_RECOVERY" "$PROC_LIFECYCLE"
bd dep add "$PROC_RECOVERY" "$ANALYZE_RISKS"

bd dep add "$STORAGE_LAYOUT" "$ANALYZE_STORAGE"
bd dep add "$STORAGE_LAYOUT" "$DECIDE_DUCKDB"
bd dep add "$STORAGE_SCHEMA_TRACES" "$STORAGE_LAYOUT"
bd dep add "$STORAGE_SCHEMA_METRICS" "$STORAGE_LAYOUT"
bd dep add "$STORAGE_SCHEMA_LOGS" "$STORAGE_LAYOUT"
bd dep add "$INGEST_TRACES" "$STORAGE_SCHEMA_TRACES"
bd dep add "$INGEST_TRACES" "$ANALYZE_COLLECTOR"
bd dep add "$INGEST_METRICS" "$STORAGE_SCHEMA_METRICS"
bd dep add "$INGEST_METRICS" "$ANALYZE_COLLECTOR"
bd dep add "$INGEST_LOGS" "$STORAGE_SCHEMA_LOGS"
bd dep add "$INGEST_LOGS" "$ANALYZE_COLLECTOR"

bd dep add "$QUERY_CONTRACT" "$ANALYZE_CLI"
bd dep add "$QUERY_CONTRACT" "$ANALYZE_STORAGE"
bd dep add "$QUERY_TRACES" "$QUERY_CONTRACT"
bd dep add "$QUERY_TRACES" "$INGEST_TRACES"
bd dep add "$QUERY_METRICS" "$QUERY_CONTRACT"
bd dep add "$QUERY_METRICS" "$INGEST_METRICS"
bd dep add "$QUERY_LOGS" "$QUERY_CONTRACT"
bd dep add "$QUERY_LOGS" "$INGEST_LOGS"
bd dep add "$METRICS_AGG" "$QUERY_METRICS"
bd dep add "$METRICS_AGG" "$ANALYZE_RISKS"

bd dep add "$PRUNE_DRY_RUN" "$STORAGE_LAYOUT"
bd dep add "$PRUNE_DRY_RUN" "$ANALYZE_RISKS"
bd dep add "$PRUNE_EXECUTE" "$PRUNE_DRY_RUN"

bd dep add "$VERIFY_OTLP_DIRECT" "$PROC_HEALTH"
bd dep add "$VERIFY_OTLP_DIRECT" "$QUERY_TRACES"
bd dep add "$VERIFY_OTLP_DIRECT" "$QUERY_METRICS"
bd dep add "$VERIFY_OTLP_DIRECT" "$QUERY_LOGS"
bd dep add "$VERIFY_OTLP_DIRECT" "$METRICS_AGG"
bd dep add "$VERIFY_TELEMETRYGEN" "$PROC_HEALTH"
bd dep add "$VERIFY_TELEMETRYGEN" "$QUERY_TRACES"
bd dep add "$VERIFY_TELEMETRYGEN" "$QUERY_METRICS"
bd dep add "$VERIFY_TELEMETRYGEN" "$QUERY_LOGS"
bd dep add "$VERIFY_PYTHON_E2E" "$ANALYZE_VERIFY"
bd dep add "$VERIFY_PYTHON_E2E" "$VERIFY_OTLP_DIRECT"
bd dep add "$VERIFY_PYTHON_E2E" "$VERIFY_TELEMETRYGEN"
bd dep add "$VERIFY_PYTHON_E2E" "$PRUNE_EXECUTE"
bd dep add "$VERIFY_TESTS" "$VERIFY_PYTHON_E2E"

bd dep add "$CLEANUP_DOCKER" "$VERIFY_PYTHON_E2E"
bd dep add "$DOCS_UPDATE" "$VERIFY_PYTHON_E2E"
bd dep add "$POLISH" "$CLEANUP_DOCKER"
bd dep add "$POLISH" "$VERIFY_TESTS"

echo "Graph created."
echo "Next steps:"
echo "  chmod +x setup-beads.sh"
echo "  ./setup-beads.sh"
echo "  bd ready"
echo "  bv --robot-insights"
