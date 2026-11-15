# Native collection and ingestion

The user wants Go lotel to support useful lotel-rs features except embedding, using OpenTelemetry Go libraries where possible.

## Source comparison

Rust commit `77270b2971422571434b2dc77e38581b1da89618` implements three missing capabilities in `crates/lotel-cli/src/main.rs`, `crates/lotel-cli/src/daemon.rs`, and `crates/lotel-collector/src/ingestion.rs`.

- A native collector starts without Docker.
- An initial ingestion pass and periodic passes make JSONL queryable automatically.
- `ingest --full` rereads retained JSONL instead of resuming cursors.

The Go CLI already provides queries, aggregation, retention, and broader GenAI and attribute filters. The collector-data-parity work subsequently landed in main. This branch integrates its profiles storage and complete OTLP queries, including atomic full rebuilding of all four signals and their envelopes.

## Alternatives

| Design | Benefit | Cost |
| --- | --- | --- |
| Bundled official Go Collector factories | One executable, direct library reuse, official protocol handling | Go 1.26 and a larger dependency graph; finite component registry |
| Installed official contrib executable | Full contrib component registry and fewer Go dependencies | Requires another installation and process lifecycle |

Two independent sketches and a separate design review favored bundled components. Docker remains the default for existing custom component compatibility. Native collection is additive through `start --backend native`.

## Domain and ownership

The persisted runtime state distinguishes native and Docker instances. Legacy state without a backend remains Docker state. The lifecycle owner manages a detached native child and authenticates private control requests. A Collector health endpoint belonging to another process cannot prove readiness.

The native runtime composes official OTLP, batch, GenAI normalizer, transform, spanmetrics, file, and health components. Transform preserves the existing hash and redaction recipes. Configuration parsing extracts lotel's typed ingestion settings before upstream Collector validation. Only file exporter paths rooted at `/data` map to the host data directory.

`ingestion.Options` contains `Enabled` and a positive `Interval`. Each pass opens, ingests, and closes DuckDB. It never uses the process singleton. Failures are reported without stopping collection. Shutdown flushes Collector exporters before final ingestion. Disabled ingestion applies to startup, periodic, and final passes.

`storage.ReingestAll` owns one transaction covering all indexed telemetry and cursors. It reuses the same parser and insertion path as incremental ingestion. Raw JSONL is retained. Storage errors roll back the rebuild. Retained malformed records follow existing skip-and-report behavior.

## Verification

The gated `TestNativeCLI` uses a built executable, temporary HOME, isolated ports, and a PATH without Docker. It exercises HTTP JSON, HTTP protobuf, gRPC, automatic query visibility, GenAI usage, concurrent query processes, config restart, occupied ports, shutdown flush, and full rebuild idempotency.

Focused tests verify transaction rollback and cross-process DuckDB availability. Native configuration tests cover path translation, interval validation, official factories, and existing recipes.

## Model coverage

The configured Claude and Grok families were unavailable. Design and implementation reviews used separate agents on the parent model. Independent model-family coverage remains unavailable.
