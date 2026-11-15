# Implementation evidence

Branch: `feat/genai-observability`. Base: `main`.

## Runtime verification

- Collector 0.165.0 component inventory includes `gen_ai_normalizer`,
  `spanmetrics`, `redaction`, and `transform`; no additional GenAI component.
- Default config and both content recipes pass collector `validate`.
- Docker recipe behavior tests pass for deletion and hashing. Content disappears
  from trace and log JSONL while token attributes remain.
- OpenInference observation produced `gen_ai.operation.name=chat`,
  `gen_ai.request.model`, `gen_ai.usage.input_tokens=11`, and
  `gen_ai.usage.output_tokens=7`.
- Spanmetrics JSONL file size remained unchanged across the idle observation.
- Deterministic E2E script passed 23 checks.
- Live OpenCode test passed 9 checks with 0 failures and 0 warnings using
  `glm-5.3-flash`: model calls, usage, decoded span content, structured events,
  histogram buckets, token metrics, spanmetrics, grouped usage, and leak scan.
- Missing-key SKIP path exits 0. Python compilation passes.
- Go tests, build, vet, and diff whitespace checks pass.

Raw local evidence and the pre-start snapshot are under
`~/.lotel/genai-implementation-evidence/`. Credential values are excluded.

## Scratch history acceptance gates

One full ingest run over the copied pre-upgrade history took 13.137 seconds.
It inserted 49,739 traces, 22,400 metric points, and 4,880 logs, with no skipped
lines or records. The second ingest inserted zero records for all signals.
The schema rebuild notice appeared only on the first run.

An unbounded `query genai usage` over that database returned seven groups in
0.068 seconds. These are single-run threshold checks, not comparative benchmarks.

## Evidence-driven changes to the plan

- Historical OTLP JSON includes numeric integer fields as well as decimal
  strings. The parser accepts both protobuf JSON encodings.
- This DuckDB build returns `1e21` from `json_extract_string` for a Go-encoded
  double of 1e21. The README and filter test use that observed form instead of
  the plan's `1e+21`. Filters still compare DuckDB's string form verbatim.
- The model catalog rejects Python's default urllib user agent with HTTP 403.
  A distinctive integration-test user agent succeeds.
- Beads onboarding ran. `bd ready` reports no beads database; the plan explicitly
  treats tracker initialization as outside this implementation.

## Review

The review gauntlet runs after the implementation commit. Its independently
generated reports and fixer summaries are recorded under `reviews/`.
