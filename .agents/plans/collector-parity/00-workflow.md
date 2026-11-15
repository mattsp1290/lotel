# Collector data parity

The goal is to ingest every telemetry signal supported by Collector v0.165.0 and retain its complete exported OTLP data.

## Workflow

- [x] Read the Principles section of the poteto-mode skill.
- [x] Phase A: Frame.
- [x] Phase B: Design the workflow.
- [x] Phase C: Run the loop.
- [x] Phase D: Keep the audit trail.
- [x] Phase E: Verify and hand back.

## Grounding

The existing Collector image already matches the latest release, v0.165.0. The managed pipeline lacks profiles. Handwritten ingestion structs discard fields from the three existing signals. Existing DuckDB rows are convenience projections, rather than a complete OTLP representation.

The acceptance predicate is structural equality between exported JSONL and queried OTLP data for all four signals. Verification also covers incremental ingestion, partial lines, exact numbers, profile dictionaries, schema metadata, retention, and existing query behavior.

## Throughput checkpoint

- Blocking first steps. Confirm upstream release and capabilities, capture current behavior, and compare two storage designs before implementation.
- Independent workstreams. Collector configuration and launch changes can run separately from storage. CLI and live verification follow the storage API decision.
- Shared mutable state. One owner changes storage and its migration. Collector configuration has a separate owner. No parallel writes to the same files.
- Smallest safe decomposition. Use one storage owner, one pipeline owner, then one independent review. Keep the existing query projections compatible.

## Verification and delivery

1. Add the preservation and query path with additive migration. Run storage tests.
2. Enable profiles collection and prove configuration against the pinned Collector.
3. Exercise all four signals through the live Collector and compare query output to exported data.
4. Review changes independently and resolve proven findings.
5. Run `go test ./...` and `go build ./...`, commit, update Beans, and push.

Do not delete existing telemetry during migration. Do not claim support for every receiver protocol. Receivers remain configurable through the existing custom Collector configuration.
