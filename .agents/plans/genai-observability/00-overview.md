# GenAI observability for lotel — Overview

Status: Implemented on the feature branch. Acceptance evidence and plan corrections are in [07](07-implementation-evidence.md). Review gauntlet pending.

## Request

> We want to support all of the llm observability features the otel collector supports. We can make an optional python integration test that uses the opencode go subscription token that exports from ~/.profile

## Application context

```json
{
  "application_context": {
    "has_active_users": false,
    "backward_compatibility_required": false,
    "feature_flags": "not-applicable",
    "confirmation_digest": "5f2ec49e9628bac090201fc25db00acd9fef9aaf8215971f5af39fd1aca1e71f",
    "confirmed_at": "2026-11-15T03:50:11Z"
  }
}
```

Implications:

- The plan may change the DuckDB schema, the JSON shape of every `lotel query` command, the `lotel ingest` output, and the generated collector config without shims.
- An existing `~/.lotel/data/lotel.db` is rebuilt from the raw JSONL files on first open after upgrade (see [02](02-storage-schema-and-ingest.md#schema-versioning-and-rebuild)). This is a convenience, not a compatibility promise.
- A header-less `~/.lotel/collector-config.yaml` is backed up and replaced (see [01](01-collector-pipeline.md#managed-config-refresh-resolvepaths)).
- A running collector with a stale image or config is restarted by `lotel start`.
- No feature flags. Each behavior change ships directly.

## Change type and affected areas

Inferred from the request and repository:

- **Change type:** feature (new GenAI capability) plus a refactor of the ingest and storage layer. That layer currently loses most of the OTLP data that GenAI telemetry depends on.
- **Affected areas:**
  - `internal/config/` holds the generated collector config.
  - `internal/collector/` holds the pinned image and the container run.
  - `internal/storage/` holds the schema, ingest, query and prune code.
  - `cmd/lotel/main.go` holds the CLI surface.
  - `scripts/` holds the end-to-end and integration scripts.
  - `README.md` and `AGENTS.md` hold the documentation.

## What "the LLM observability features the collector supports" means

Facts were verified on 2026-11-14 against upstream sources. Citations are in [01](01-collector-pipeline.md#upstream-facts).

| Collector or semconv capability | Status upstream | lotel today | Target |
|---|---|---|---|
| `gen_ai_normalizer` processor: OpenInference and OpenLLMetry span attributes → OTel GenAI | contrib, alpha, traces only, semconv 1.40.0, added in v0.152.0 | Not configured. The locally cached `:latest` image is 18 months old and predates the processor. | Pin the image and enable both built-in sources |
| `spanmetrics` connector: RED metrics from spans | contrib, generic | Not configured | Enable it with `gen_ai.*` dimensions |
| `redaction` and `transform` processors, used for prompt and content removal | contrib, generic | Not configured | Ship validated opt-in recipe configs |
| GenAI span attributes: scalar, `string[]` and `any` (structured messages) | semconv Development status | `flattenAttrs` drops arrays, kvlists and bytes to `""` | Store typed attribute JSON |
| GenAI events as LogRecords with `eventName` and structured attributes or body | proto `LogRecord.event_name` | `eventName` is not parsed. The body is scalar only. | Store and filter by `event_name`. Store a structured body. |
| Span events, links, resource and scope | OTLP core | Dropped | Store all of them |
| GenAI histograms: token usage, duration, TTFC/TTFT, buckets | semconv v1.41 plus unreleased main | Histogram stores only `sum`, without count or buckets. Exponential histograms and summaries are dropped. | Store count, sum, min, max, bounds, buckets and exponential buckets |
| GenAI-specific receivers, exporters, connectors or OTTL functions | **None exist upstream** | — | Nothing to add |

The collector has no other GenAI-specific components. The rest of "support" is ingest fidelity plus GenAI-aware queries, so that the telemetry the collector forwards becomes useful to an agent.

## Success criteria

1. `lotel start` runs `otel/opentelemetry-collector-contrib:0.165.0`. Its default config passes `otelcol-contrib validate`, and that config includes `gen_ai_normalizer`, `spanmetrics` and the existing file exporters.
2. A synthetic OpenInference span sent to `:4318` is queryable after `lotel ingest`. Its `gen_ai.usage.input_tokens` attribute must be set by the normalizer.
3. A structured `gen_ai.input.messages` (`arrayValue`) value round-trips through ingest and query as a JSON array, not a string.
4. A LogRecord with `eventName: gen_ai.client.inference.operation.details` can be filtered with `lotel query logs --event-name ...`. The query returns its structured attributes, `trace_id` and `span_id`.
5. Histogram data points expose `count`, `sum`, `min`, `max`, `explicit_bounds` and `bucket_counts`. `len(bucket_counts) == len(explicit_bounds) + 1` must hold.
6. `lotel query genai calls` and `lotel query genai usage` return per-call and grouped token, latency and error data. They work for both v1.40/v1.41 and legacy attribute names.
7. Running `lotel ingest` twice over unchanged files inserts each record once. The Go fixture test proves this for all three signals. A live run proves it for traces and logs. The spanmetrics idle gate proves that an idle collector stops appending metrics.
8. `go test ./...`, `go build ./...` and `go vet ./...` pass.
9. `python3 scripts/verify.py` passes, including the new deterministic GenAI section. It needs no LLM credentials.
10. `scripts/genai_integration.py` runs only when `OPENCODE_GO_API_KEY` is set. With the key set, it makes real OpenCode Go calls through the official OTel OpenAI instrumentation and asserts these behaviors on real data:
    - structured content and JSON-string content decoding;
    - GenAI event logs;
    - histogram buckets;
    - spanmetrics;
    - `query genai` usage.

    Without the key, it prints `SKIP` and exits 0. The live half is claimed only after a real run passes.

## Scope

In scope:

- Collector config and image pin.
- Full-fidelity OTLP JSON ingest.
- Schema v2.
- Idempotent incremental ingest.
- GenAI query commands.
- Generic attribute and event filters.
- Opt-in redaction recipes.
- Deterministic E2E coverage.
- The optional live LLM integration test.
- Documentation.

## Non-goals

- New collector components, a custom collector build, or OTTL extensions.
- A UI, cost estimation in dollars, or pricing tables.
- Instrumenting lotel itself.
- Mapping unreleased semconv-main metric names to v1.41 names. Both are stored verbatim. Only `query genai` reads span attributes, which are stable across versions.
- The Go-to-Rust rewrite in `docs/prompts/rewrite-lotel-go-to-rust-otel-collector.md`. This plan targets the current Go code. Schema and CLI contracts are documented language-neutrally so a rewrite can match them.
- Uploading content externally (`*_ref` attributes are stored as plain strings).

## Constraints

- Single host, local Docker. This is unchanged from `README.md#scope`.
- CGO DuckDB (`github.com/marcboeker/go-duckdb v1.8.5`).
- The OpenCode Go key is read only from the environment variable `OPENCODE_GO_API_KEY`. Nothing may print, log, persist or record it in telemetry.
- Quality gates come from `AGENTS.md`: `go test ./...` and `go build ./...`.

## Repository-grounded findings that drive the design

- `internal/config/config.go:15` `DefaultConfig` has no GenAI processing. `ResolvePaths` writes the default config **only when it is absent** (`config.go:96`), so existing users would never get a new config. A managed-config refresh is required.
  - On the developer machine, `~/.lotel/collector-config.yaml` is a header-less pre-Docker file. Its exporter paths point at host paths (`~/.lotel/data/...`), which are wrong inside the container.
- `collector.Start` returns early when a container is running (`collector.go:131-134`), so a stale `latest` collector survives an upgrade.
- `internal/collector/collector.go:42` `defaultImage = "otel/opentelemetry-collector-contrib:latest"`. `docker run` does not re-pull `latest`, and the locally cached `latest` is 18 months old, which predates `gen_ai_normalizer`. The image must be pinned.
- `internal/storage/ingest.go` `otlpValue` decodes only `stringValue`, `intValue`, `boolValue` and `doubleValue`. `flattenAttrs` produces `map[string]string`, so structured GenAI content becomes `""`.
- The `ingestTraces` span struct has no `events`, `links`, `traceState` or `scope`. The `ingestLogs` struct has no `eventName`, and its body is scalar only.
- `extractDataPoints` handles only `sum`, `gauge` and `histogram`, and keeps only `histogram.sum`. `exponentialHistogram` and `summary` are silently dropped.
- `IngestAll` re-reads whole files with no offset tracking. `TestIngestIdempotent` (`internal/storage/ingest_test.go:117`) currently *asserts* duplication. Every `lotel ingest` in the AGENTS.md loop therefore duplicates all history held in the per-signal JSONL file.
- The `file/*` exporters set no `append:` (`config.go:31-39`). The file exporter defaults to truncating on start, so **every collector start wipes the raw JSONL**. Any telemetry not yet ingested is lost, and a raw-JSONL rebuild is impossible after a restart. WP1 adds `append: true` and snapshots existing history before the first restart.
- `bufio.Scanner` has a 10 MiB line cap (`ingest.go:58`) and stops silently on longer lines (`scanner.Err()` is never checked). GenAI batches carrying full message content can approach that cap.
- `cmd/lotel/main.go` shares the `service/since/until/limit` variables across four subcommands. New subcommands must keep using `parseQueryOpts` (`main.go:287`).
- `scripts/verify.py` is a plain-`requests` E2E script with a `test(name, fn)` pass/fail harness. The new deterministic GenAI checks extend it.
- Baseline on 2026-11-14: `go test ./...` and `go vet ./...` pass on a clean `main` at `1f75e93`.
- Beads: `AGENTS.md` names `bd`, but `bd ready` reports "no beads database found". Issue tracking is not a prerequisite for this plan.

## Key decisions

| # | Decision | Reason | Rejected alternative |
|---|---|---|---|
| D1 | Pin `otel/opentelemetry-collector-contrib:0.165.0` (constant `defaultImage`) | `latest` is non-reproducible and stale locally. The normalizer requires ≥ v0.154 to be alpha. | Keep `latest` and `docker pull` on start: slow, non-deterministic |
| D2 | A lotel-managed config with a version header. Any older or header-less file is backed up to `.bak.<ts>` and replaced. Custom config belongs in `./lotel-collector.yaml`. `lotel start` restarts a running collector whose image or config hash changed. | Existing files, including the developer's broken pre-Docker file, must gain GenAI processing. Compatibility is not required, and the backup preserves any edits. | Keep header-less files: leaves GenAI processing silently off |
| D3 | Store attributes as typed JSON (`map[string]any`), not `map[string]string` | Structured `gen_ai.input.messages` is `any`-typed. The Python instrumentation emits JSON *strings* on spans and *structured* values on events, so both shapes must survive. | A separate content table: more joins for no gain at local scale |
| D4 | Schema versioning with drop-and-rebuild from raw JSONL | Compat is not required. Raw JSONL is the source of truth, so the rebuild is lossless as long as the files exist. | `ALTER TABLE` migrations: unneeded complexity |
| D5 | Incremental ingest with a per-file byte offset in an `ingest_state` table | Fixes duplication. Makes the agent loop's repeated `ingest` cheap. | Content-hash dedupe: costlier, and still re-reads everything |
| D6 | `query genai` derives usage from **span attributes**, not metrics | Span `gen_ai.usage.*` names are stable from v1.37 to main. The metric names diverge between v1.41 and main. Spans exist even when an app emits no metrics. | Metric-based usage: breaks across semconv versions |
| D7 | Enable `spanmetrics` with GenAI dimensions by default, using **delta** temporality, a 5 s flush and a 5 m expiration | Gives per-model call and latency metrics for instrumentations that emit no metrics. Delta temporality stops idle re-emission into the append-only JSONL. | Recipe-only: hides a supported feature. Cumulative temporality: unbounded growth. |
| D8 | Redaction and transform are **opt-in recipes**, not the default | Local debugging needs content. lotel is single-host. | Redact by default: defeats prompt debugging |
| D9 | The live test is a PEP 723 `uv run` script that skips without the key | Self-contained dependencies, matches the `scripts/verify.py` style, no repo-wide Python toolchain | A pytest suite with a venv: adds a toolchain the repo lacks |
| D10 | Add `append: true` to every file exporter | The file exporter truncates on start by default. Restarts would otherwise wipe un-ingested telemetry and the raw history that D4 rebuilds from. | Keep truncation: data loss on every auto-restart |

## Change model

Before:

```
app ─OTLP─▶ collector(latest: otlp→batch→file) ─▶ *.jsonl ─ingest(full re-read, lossy)─▶ DuckDB(v1) ─▶ query traces|metrics|logs
```

After:

```
app ─OTLP─▶ collector(0.165.0)
              traces : otlp → gen_ai_normalizer → [opt-in content recipe] → batch → file/traces(append), spanmetrics(delta)
              metrics: otlp, spanmetrics → batch → file/metrics(append)
              logs   : otlp → [opt-in content recipe] → batch → file/logs(append)
          ─▶ *.jsonl ─ingest(offset-tracked, lossless)─▶ DuckDB(v2: typed attrs, events, links,
                                                           resource/scope, event_name, histogram buckets)
          ─▶ query traces|metrics|logs (+ --name/--attr/--event-name) | query genai calls|usage
```

## Risks and assumptions

- **R1 (assumption):** `gen_ai_normalizer` at 0.165.0 accepts `sources: [{name: openinference}, {name: openllmetry}]`, as its README states. The `validate` gate in [01](01-collector-pipeline.md) proves it before anything else depends on it. The exact keys the normalizer writes are unverified. The normalizer observation gate in WP1 records them, and `query genai` selection matches any `gen_ai.*` key.
- **R2:** GenAI semconv is in Development status and has no tagged release. Attribute names can change. Mitigation: `query genai` uses an alias table (see [03](03-query-surface.md#attribute-alias-table)) and stores everything verbatim.
- **R3:** The OpenCode Go model catalog changes often. The live test reads `LOTEL_IT_OPENCODE_MODEL` and pre-checks `/v1/models`.
- **R4:** The Python 3.14 wheels for the OTel and openai packages are unverified. The script pins `requires-python` and documents `uv run --python 3.12` as the fallback.
- **R5:** Docker pulls 0.165.0 on first start, which needs network access. `start --wait` keeps its 30 s timeout. Pull time is outside that timeout because `docker run -d` blocks until pulled. `WaitHealthy` begins after the pull.
- **R6:** the first ingest after the rebuild re-reads all raw history, about 56 MB on the developer machine. The WP4 performance gate measures this on a scratch copy and requires under 60 s. The scripts use a 300 s ingest timeout.
- **R7:** the developer's existing raw history survives only if it is snapshotted before the first collector restart (WP1 step zero). History is truncated on any start with a config that lacks `append: true`.

## Stop/go gates

- **G0** (start of WP1): the component inventory of the pinned image lists `gen_ai_normalizer`, `spanmetrics`, `redaction` and `transform`, and no other GenAI component. If any other appears, extend the plan.
- **G1** (after WP1): `docker run --rm -v <cfg>:/c.yaml otel/opentelemetry-collector-contrib:0.165.0 validate --config=/c.yaml` exits 0. The normalizer observation gate and the spanmetrics idle gate also pass. If any fails, stop and fix the config before any storage work merges.
- **G2** (after WP4): `go test ./internal/storage/...` passes with the GenAI fixture assertions.
- **G3** (after WP7): `scripts/verify.py` passes against a real collector.

## Unresolved decisions

No blocking decisions remain.

Non-blocking:

- **N1:** The default `LOTEL_IT_OPENCODE_MODEL` is `glm-5.3-flash`, a Chat Completions model from `/v1/models` on 2026-11-14. The model has `reasoning.supported: true`, so the live test uses `max_tokens=256` to leave room for reasoning. The implementer may substitute any model whose native API is chat completions if that one disappears. Owner: implementer.
- **N2:** Issue tracker choice (`bd` vs `bn`) for follow-ups. Owner: user. This does not affect implementation.

## Document map

| File | Purpose |
|---|---|
| [00-overview.md](00-overview.md) | This file: context, decisions, gates |
| [01-collector-pipeline.md](01-collector-pipeline.md) | WP1: image pin, managed config, normalizer, spanmetrics, redaction recipes |
| [02-storage-schema-and-ingest.md](02-storage-schema-and-ingest.md) | WP2–WP4, WP6: OTLP model, schema v2, incremental lossless ingest, prune |
| [03-query-surface.md](03-query-surface.md) | WP5: query output changes, new filters, `query genai` |
| [04-verification-and-opencode-test.md](04-verification-and-opencode-test.md) | WP7–WP8: deterministic E2E, optional live OpenCode Go test |
| [05-documentation.md](05-documentation.md) | WP9: README and AGENTS.md changes |
| [06-execution-handoff.md](06-execution-handoff.md) | Ordered packages, verification, definition of done |
