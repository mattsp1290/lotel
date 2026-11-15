# Rust feature parity

The user wants useful lotel-rs features in Go lotel, excluding embedding, with official OpenTelemetry Go libraries where possible.

## Feature workflow

- [x] 1. `how` over the affected subsystem.
- [x] 2. `architect` for parallel design exploration.
- [x] 3. Write the throughput checkpoint as four todo items.
- [x] 4. Delegate code-writing to a subagent using your configured feature model.
- [x] 5. Verify on the matching surface.
- [x] 6. Rebase into small, ordered commits. Stack follow-ups.
- [x] 7. If the design is contested, `interrogate` before shipping. Skip because both sketches and the independent judge agreed.
- [x] 8. Run **Opening a PR**. Publish a draft against main after the quality gates and independent reviews.

## Throughput checkpoint

- Blocking first steps. Pin the Rust comparison, identify implemented gaps, inspect official Collector APIs, and compare bundled and external native runtime designs.
- Independent workstreams. Native collection owns lifecycle, configuration, and dependencies. Storage owns full rebuild and periodic ingestion. Documentation and CLI verification follow their contracts.
- Shared mutable state. Use this isolated worktree because another chat owns collector data parity. One writer owns each code area. Automatic ingestion closes its DuckDB connection after each pass.
- Smallest safe decomposition. Two implementation owners cover the runtime and storage. Integrate CLI changes after the contracts agree. Use an independent review after real CLI verification.

## Comparison

Rust reference is `77270b2971422571434b2dc77e38581b1da89618`.
Implemented gaps are native collection, configured periodic ingestion, and full re-ingestion. Embedding is excluded. Query and GenAI functionality already exceed the Rust implementation.

## Architecture phases

- [x] Ground.
- [x] Sketch. Compare bundled official Go components with an external official binary.
- [x] Agree. Choose based on behavior preservation and maintenance.
- [x] Implement.
- [x] Scrap. Skip because the selected architecture passed real CLI checks.

## Arena phases

- [x] Frame. Grade official component reuse, no-Docker operation, current pipeline compatibility, lifecycle safety, and maintenance cost.
- [x] Fan out.
- [x] Cross-judge.
- [x] Pick.
- [x] Graft. Preserve transform recipes, authenticated readiness, exporter flush before final indexing, and disabled ingestion at every lifecycle stage.
- [x] Verify.

Configured Claude and Grok models are unavailable in this runtime. Delegates use the parent model. This limits independent model coverage.

The requested cursor-team-kit CLI control and deslop helpers are not installed. Actual executable tests, scoped comment review, independent correctness review, and manual diff inspection cover those checks here.

## Review outcomes

Storage review found no actionable issues. Runtime review reproduced a concurrent status race. Status no longer deletes lifecycle state. A fresh executable passed the original concurrent reproduction after the fix. Added a regression test for read-only status behavior.

The native CLI test passed with HTTP JSON, HTTP protobuf, gRPC, canonical and OpenInference GenAI fields, time-bounded queries, automatic ingestion, disabled ingestion, rebuild idempotency, occupied ports, config restart, shutdown flush, crash recovery, and fresh-home queries. Collector DryRun validated the managed config and both transform recipes.

Final quality gates passed with `go test ./...`, `go build ./...`, `go vet ./...`, and `git diff --check`. Storage and runtime received independent reviews. No actionable findings remain.
