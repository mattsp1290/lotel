# Big Change Planning with Beads

## Agent Instructions

You are an expert software architect planning a significant change to an existing codebase. This task graph will be executed by AI agents working in parallel, coordinated through MCP Agent Mail with file reservations to prevent conflicts.

<quality_expectations>
Create a thorough, production-ready task graph that respects the existing codebase. Understand current patterns before proposing changes. Include analysis, implementation, migration, testing, and documentation tasks. Each task should be specific enough for an agent to execute independently without ambiguity.
</quality_expectations>

## Change Information

### Change Type
NEW_FEATURE

### Description
Add periodic automatic ingestion (every 2-5 minutes, configurable) that runs alongside the OTLP collector server. When the collector is running, it should automatically ingest JSONL data into DuckDB on a timer so telemetry data is continuously available for querying without manual `lotel ingest` calls.

### Links to Relevant Documentation
N/A

### Affected Areas
All three workspace crates:
- `crates/lotel-collector/` — pipeline orchestration, config, new ingestion task module
- `crates/lotel-storage/` — incremental ingestion with byte offset tracking
- `crates/lotel-cli/` — existing CLI (minor changes)

### Success Criteria
Integration test that verifies:
1. DuckDB is auto-created by the ingestion task
2. Data appears in DuckDB after the ingestion interval
3. Subsequent data is ingested incrementally without duplicates
4. Graceful shutdown (no leaked threads/resources)

### Constraints
N/A

---

## Your Task

Before creating tasks, you must first understand the existing codebase. Then create a comprehensive **Beads task graph** using the `bd` CLI.

---

## Output Format

```bash
#!/bin/bash
# Project: lotel
# Change: Periodic automatic ingestion alongside OTLP collector
# Generated: 2026-03-21

set -e

if [ ! -d ".beads" ]; then
    bd init
fi

echo "Creating periodic ingestion beads..."

# ========================================
# Phase 1: Analysis
# ========================================

ANALYZE=$(bd create "Analyze current ingestion pipeline and DuckDB concurrency model" -p 0 --label analysis --silent)

# ========================================
# Phase 2: Storage - Incremental Ingestion
# ========================================

EXTRACT_LINES=$(bd create "Extract pub(crate) per-line ingestion functions from ingest.rs for reuse" -p 0 --label impl --silent)
bd dep add $EXTRACT_LINES $ANALYZE

INCREMENTAL=$(bd create "Create IncrementalIngester with byte offset tracking in ingest_incremental.rs" -p 0 --label impl --silent)
bd dep add $INCREMENTAL $EXTRACT_LINES

# ========================================
# Phase 3: Collector - Config & Task
# ========================================

CONFIG=$(bd create "Add IngestionConfig struct, parse_duration function, update DEFAULT_CONFIG" -p 0 --label impl --silent)
bd dep add $CONFIG $ANALYZE

DEPS=$(bd create "Move lotel-storage from dev-dependencies to production dependencies in lotel-collector" -p 0 --label impl --silent)
bd dep add $DEPS $INCREMENTAL

TASK=$(bd create "Create ingestion.rs module with dedicated OS thread + channel pattern for DuckDB work" -p 0 --label impl --silent)
bd dep add $TASK $DEPS
bd dep add $TASK $CONFIG

PIPELINE=$(bd create "Wire ingestion task into Pipeline::run() with conditional spawn based on config" -p 0 --label impl --silent)
bd dep add $PIPELINE $TASK

# ========================================
# Phase 4: Testing
# ========================================

UNIT_TESTS=$(bd create "Add unit tests for IncrementalIngester (no duplicates, appended data, missing files)" -p 0 --label testing --silent)
bd dep add $UNIT_TESTS $INCREMENTAL

CONFIG_TESTS=$(bd create "Add tests for IngestionConfig parsing, backward compatibility, parse_duration" -p 0 --label testing --silent)
bd dep add $CONFIG_TESTS $CONFIG

INTEGRATION=$(bd create "Add periodic_ingestion_roundtrip integration test verifying auto-ingestion and no duplicates" -p 0 --label testing --silent)
bd dep add $INTEGRATION $PIPELINE

# ========================================
# Phase 5: Cleanup
# ========================================

CLIPPY=$(bd create "Fix all clippy warnings across workspace (collapsible_if, new_without_default, etc)" -p 1 --label cleanup --silent)
bd dep add $CLIPPY $INTEGRATION

echo ""
echo "Bead graph created! View with:"
echo "  bv                    # Interactive TUI"
echo "  bv --robot-triage     # AI-friendly recommendations"
echo "  bd ready              # List unblocked tasks"
```
