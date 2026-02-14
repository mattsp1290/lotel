# CLI Refactor Planning with Beads (Go + OpenTelemetry)

## Agent Instructions

You are an expert software architect planning a large Go refactor in this repository.
Produce a production-ready Beads task graph for parallel AI execution.

<quality_expectations>
Understand existing code before proposing changes. Tasks must be concrete, independently executable, and safe for parallel work with file reservations. Cover analysis, implementation, tests, migration/cleanup, and docs.
</quality_expectations>

## Change Information

### Description
Refactor this repository from Docker/compose orchestration to a local CLI-driven OpenTelemetry workflow:

- `lotel start` / `lotel stop` must manage **only** a collector process (managed subprocess model).
- The collector must behave like existing repo configuration intent, but without Docker runtime dependency.
- The CLI must query telemetry data from disk under `$HOME/.local/data` for:
  - traces
  - metrics
  - logs
- Querying must support filtering by service tag (`service.name`) and metrics aggregations over a time range (`avg`, `min`, `max`).
- The CLI must prune stored telemetry by age (days/hours).
- Replace existing Docker-centric verification scripts with a new Python verification flow.

### Platform and Constraints
- Target OS: macOS + Linux only
- No backwards compatibility requirement
- No DB process dependency (for example, no Postgres service)
- Data must be persisted to disk; indexed on-disk structures are allowed
- Local development only; no auth/TLS requirements for now

### Required Cleanup
Outside `./docs`, remove hints/usages of:
- docker-compose
- spin
- signoz

Remove Docker-specific verification scripts under `scripts/verification`.
Compose files should be removed from active runtime paths (docs references are acceptable).

### Success Criteria
- CLI can start/stop a collector subprocess and report health/readiness
- CLI can read back traces/metrics/logs from disk by `service.name`
- Metrics query supports time range + `avg|min|max`
- CLI can prune telemetry by `--older-than` style age
- New Python verification script:
  - verifies collector is running
  - submits metric + log + trace with a UUID service tag
  - validates the CLI can read all three signals from disk

---

## Your Task

Before creating tasks, analyze the existing Go codebase and then generate a comprehensive **Beads task graph** using `bd`.

Output a shell script named `setup-beads.sh` that creates the full graph.

---

## Phase 0: Required Analysis (Do First)

Inspect and document:

### 1) Current CLI and Process Control
- Existing command structure in `cmd/` and `internal/`
- How `lotel start` / `lotel stop` currently work
- Existing dependency on Docker client/runtime

### 2) Current Collector Behavior
- Existing collector config semantics (receivers/processors/exporters/extensions/pipelines)
- Existing paths and formats under local `data/` directories
- Health endpoint assumptions and readiness behavior

### 3) Telemetry Data Access Model
- How traces/metrics/logs are currently exported to disk
- Feasible on-disk query model for agent-oriented CLI usage
- Candidate indexing approach that avoids an external DB service

### 4) Verification and Test Coverage
- Existing scripts and tests that assume Docker/compose
- What should be removed/replaced
- Gaps for end-to-end verification (start -> ingest -> query -> prune)

### 5) Risk Assessment
- Silent break risks in signal handling (especially metrics aggregation)
- Operational risks in subprocess lifecycle management (PID, stale lock files, restart behavior)
- File format/versioning and parsing risks

Prefer simplicity and readability over over-engineering.

---

## Output Requirements

Generate a script that:

1. Initializes Beads if needed
2. Creates analysis beads first
3. Creates implementation beads split for parallel work
4. Adds explicit verification/testing beads
5. Adds migration/cleanup beads for Docker-era assets
6. Adds documentation beads
7. Adds dependencies and labels so the graph is acyclic

### Priority Levels
- `-p 0`: critical path / blockers / high risk
- `-p 1`: high-priority implementation
- `-p 2`: standard implementation and docs
- `-p 3`: cleanup polish

### Labels
Use labels consistently:
- `analysis`
- `process`
- `storage`
- `query`
- `prune`
- `verify`
- `cleanup`
- `docs`

---

## Must-Have Task Areas in the Graph

Your graph must include explicit beads for:

1) Collector Process Management
- Introduce subprocess-based collector lifecycle from CLI
- Start/stop/status/health behavior
- PID/state management with crash-safe handling

2) Collector Runtime Entrypoint
- Add a clear command entrypoint under `cmd/` for server/process control concerns
- Ensure main CLI remains coherent for coding-agent usage

3) Disk Storage Format and Indexing
- Define stable on-disk layout under `$HOME/.local/data`
- Define parse/query contract for traces/metrics/logs
- Add on-disk indexing strategy (no external service process)

4) Query UX for Coding Agents
- Add deterministic, script-friendly query commands
- Require machine-readable JSON output by default for all query commands (`--output json` default behavior), with clear exit codes
- Service filter must be first-class (`service.name`)

5) Metrics Aggregation
- Time range query support
- `avg`, `min`, `max` aggregations
- Edge-case behavior for empty windows and mixed temporality

6) Pruning
- Age-based pruning in days/hours
- Dry-run mode and safe deletion reporting

7) Verification Rewrite (Python)
- Replace Docker-specific verification scripts
- End-to-end script using UUID service tag:
  - emit traces/metrics/logs
  - verify CLI query returns each signal from disk
  - verify prune behavior

8) Ingestion Tooling Validation
- Add tasks to validate both:
  - direct OTLP submission from Python
  - optional `telemetrygen`-based ingestion path for smoke testing (best-effort, not a release blocker)

9) Docker/Compose Decommission
- Remove or retire Docker/compose operational paths from code/scripts
- Keep docs-only references where intentionally retained

10) Documentation
- Update usage docs for new start/stop/query/prune flow
- Add troubleshooting section for local collector subprocess failures

---

## Bead Granularity and Dependency Rules

- Keep each bead focused and usually under ~750 LOC changed
- Prefer more small beads over fewer giant beads
- Block implementation on analysis results where needed
- Block cleanup on verification success
- Ensure parallel tracks do not contend on the same files

If multiple implementation strategies are possible, add an early decision bead that captures trade-offs and selected approach.

---

## Verification Criteria for the Planned Work

Include beads that enforce these checks:

- `lotel start` launches collector subprocess and reports healthy
- OTLP ingestion works for traces/metrics/logs
- Query commands return deterministic output for agent workflows
- Metrics aggregation (`avg|min|max`) works over explicit time windows
- `lotel prune --older-than` behavior is safe and test-covered
- Docker-dependent verification scripts are replaced by Python equivalents

---

## Script Skeleton (Use This Shape)

```bash
#!/usr/bin/env bash
set -euo pipefail

if [ ! -d ".beads" ]; then
  bd init
fi

echo "Creating CLI refactor task graph..."

# analysis
ANALYZE_CLI=$(bd create "Analyze current lotel CLI command/process architecture" -p 0 --label analysis --silent)
ANALYZE_COLLECTOR=$(bd create "Analyze collector config behavior to preserve without Docker runtime" -p 0 --label analysis --silent)
ANALYZE_STORAGE=$(bd create "Analyze disk telemetry format and define index/query contract" -p 0 --label analysis --silent)

# implementation (create many focused beads here)
# add dependencies with: bd dep add <child> <parent>

echo "Graph created. Next:"
echo "  bd ready"
echo "  bv --robot-insights"
```

---

## Final Checklist

Your generated graph is complete only if all are true:

- [ ] Analysis beads exist and are dependency roots
- [ ] Collector subprocess management is explicitly planned
- [ ] Query + metrics aggregation + prune are explicitly planned
- [ ] Python verification replacement is explicitly planned
- [ ] Optional `telemetrygen` validation path is explicitly planned as non-blocking
- [ ] Docker/compose cleanup is explicitly planned
- [ ] Docs updates are explicitly planned
- [ ] Dependency graph is acyclic and parallel-safe
