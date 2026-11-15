# WP7–WP8 — Deterministic E2E and the optional OpenCode Go integration test

## WP7 — Extend `scripts/verify.py` with a deterministic GenAI section

### Goal

This section proves the whole pipeline with no LLM credentials:

```
collector normalizer + spanmetrics → JSONL → ingest v2 → query genai
```

Prerequisites: WP1 and WP5 merged.

### Repository evidence

- `scripts/verify.py` uses `requests.post` to `{OTLP_HTTP}/v1/<signal>`.
- It uses the `test(name, fn)` pass/fail counters, a `SERVICE_NAME = f"verify-{uuid8}"` isolation tag, and `LOTEL_BIN` (default `lotel`).
- Its sections are numbered `[0/5]`…`[5/5]`. The prune section deletes this run's data, and the stop section stops the collector unconditionally.
- Its docstring still says "otelcol-contrib installed and on PATH". That line is stale since the Docker switch (commit `4e3f887`).

### Change surface

| Path | Symbol | Change |
|---|---|---|
| `scripts/verify.py` | docstring | Replace the otelcol-contrib prerequisite with "Docker running" |
| `scripts/verify.py` | `run()` | Accept a `timeout` argument. Use 300 s for `ingest`, because the first ingest after a schema rebuild re-reads all history. Keep 30 s for the other commands. |
| `scripts/verify.py` | `start_collector` | Unchanged call. `lotel start` now restarts a stale collector (WP1). |
| `scripts/verify.py` | `check_collector_pinned` (new) | `docker inspect -f '{{.Config.Image}}' lotel-collector` must end with `:0.165.0`. Otherwise fail with "stale collector; WP1 restart logic missing". Also run `lotel status`. If the running collector's `config_path` (from `~/.lotel/collector.state`) is not the default `~/.lotel/collector-config.yaml`, fail early with "collector running with custom config <path>; stop it first". |
| `scripts/verify.py` | `ingest_until(predicate, timeout=30)` (new) | Loops `lotel ingest` plus a query every 2 s until `predicate(result)` is true or the timeout passes. This replaces the fixed `time.sleep(3)` for the GenAI checks. |
| `scripts/verify.py` | `send_genai_openinference_span`, `send_genai_native_span`, `send_genai_event_log`, `send_genai_histogram` (new) | Synthetic payloads (below) |
| `scripts/verify.py` | `check_*` functions (new) | Assertions (below) |
| `scripts/verify.py` | `main` | Insert a new `GenAI` section after the query section and before prune. Renumber the sections to `/6`. |

### Payloads

Every payload uses `SERVICE_NAME`. Payload 2 and later use `MODEL = "verify-model-<uuid8>"`. Payload 1 uses a distinct `OI_MODEL = "verify-oi-<uuid8>"`, so a model mapping by the normalizer cannot change the `MODEL` usage row.

1. **OpenInference LLM span** (this tests the normalizer):
   - `openinference.span.kind=LLM`, `llm.model_name=OI_MODEL`, `llm.token_count.prompt` (intValue 11), `llm.token_count.completion` (intValue 7).
   - No `gen_ai.*` keys.
2. **Native GenAI chat span:**
   - Name `chat MODEL`, kind 3 (CLIENT).
   - `gen_ai.operation.name=chat`, `gen_ai.provider.name=openai`, `gen_ai.request.model=MODEL`.
   - `gen_ai.usage.input_tokens=5`, `gen_ai.usage.output_tokens=3`.
   - `gen_ai.input.messages` as an `arrayValue` of `kvlistValue` with `role=user` and `parts=[{type:text, content:"hello <uuid8>"}]`.
   - One span event `gen_ai.content.test`.
3. **GenAI event log record:**
   - `eventName=gen_ai.client.inference.operation.details`, `severityNumber=5`.
   - `traceId` and `spanId` equal to payload 2.
   - Attribute `gen_ai.output.messages` as an `arrayValue`.
4. **Token-usage histogram:**
   - `gen_ai.client.token.usage`, unit `{token}`, temporality 1 (delta).
   - One point with `gen_ai.token.type=input` and `gen_ai.request.model=MODEL`.
   - `explicitBounds=[1,4,16,64,256,1024]`, `bucketCounts=["0","0","1","0","0","0","0"]`, `count="1"`, `sum=5`.

### Assertions

- `check_normalizer_tokens`: `query genai calls --service S` contains a call whose `input_tokens == 11` and `output_tokens == 7`. This proves `gen_ai_normalizer` ran.
  - Selection matches any `gen_ai.*` key, so the span is found even if the normalizer writes no operation or provider key.
  - If `request_model == OI_MODEL` also holds, print `info: normalizer mapped model`. Do not fail on the model, because the exact OpenInference→model mapping is unverified.
- `check_native_call`:
  - A call with `request_model == MODEL` exists, with `provider == "openai"`, `input_tokens == 5`, and `event_count == 1`.
  - With `--content`, its `input_messages[0]["parts"][0]["content"] == "hello <uuid8>"`.
- `check_event_log`: `query logs --service S --event-name gen_ai.client.inference.operation.details` returns one record. Its `span_id` equals payload 2's span ID, and its `attributes["gen_ai.output.messages"]` is a list.
- `check_histogram`: `query metrics --service S --name gen_ai.client.token.usage` returns one point with `bucket_counts == [0,0,1,0,0,0,0]`, `explicit_bounds` of length 6, and `count == 1`.
- `check_spanmetrics`: `query metrics --service S --name traces.span.metrics.calls --attr gen_ai.request.model=MODEL` returns at least one point.
  - Poll for up to 30 s, because the connector flushes every 5 s.
  - If the connector names its metric differently at the pinned version, take the name from the pinned README and update both this check and [05](05-documentation.md).
- `check_usage`: `query genai usage --service S --group-by model` has a row for `MODEL` with `input_tokens == 5` and `calls == 1`.
- `check_ingest_idempotent`:
  1. Run `ingest` and record `len(query traces --service S)` and `len(query logs --service S)`.
  2. Run `ingest` again. Both service-scoped counts must be unchanged.
  - Do not assert global `records == 0`. Other OTLP producers on the machine can legitimately add records between the two runs.
  - Metrics are excluded on purpose. The live spanmetrics connector can legitimately emit between the two runs. Metrics idempotency is proven by the Go fixture test `TestIngestIdempotent` and by the WP1 spanmetrics idle gate.

### Acceptance

These two commands must run in sequence:

```
go build -o lotel ./cmd/lotel
LOTEL_BIN=./lotel python3 scripts/verify.py
```

The run must exit 0. All existing checks must still pass. All GenAI checks must report `PASS`. This is **Gate G3**.

## WP8 — Optional live test: `scripts/genai_integration.py` (new)

### Goal

The test makes real LLM calls through OpenCode Go, using the official OTel OpenAI instrumentation. It then proves that lotel captures and queries real GenAI spans, events, metrics and spanmetrics end to end.

The test is optional:

- It is never part of `go test ./...`.
- It skips cleanly when the key is absent.

Prerequisites: WP7 merged.

### OpenCode Go facts

Verified 2026-11-14 at https://opencode.ai/docs/go/ and by unauthenticated probes:

- **OpenAI-compatible base URL:** `https://opencode.ai/inference/go/openai/v1`. Chat Completions is served at `/chat/completions`.
- **Model catalog:** `https://opencode.ai/inference/go/v1/models`. It needs no auth and reports each model's native API.
- **Auth:** `Authorization: Bearer <key>`. A bad key returns "Invalid credential". This is the default auth of the openai SDK when `api_key` is set.
- **Required header:** `x-opencode-session`. Without it the request fails with HTTP 400 `MissingSessionID`. The docs also ask for a distinctive `User-Agent`.
- **Not yet verified:** a successful authenticated completion. The first run of WP8 verifies it.

### Python instrumentation facts

Verified 2026-11-14:

- **Package:** `opentelemetry-instrumentation-genai-openai==1.3b0`.
  - Import: `from opentelemetry.instrumentation.genai.openai import OpenAIInstrumentor`.
  - It supersedes `opentelemetry-instrumentation-openai-v2`.
  - It depends on `opentelemetry-util-genai`.
- **Content capture:** set `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT=span_and_event`.
- **What it emits:**
  - Spans carry content as **JSON strings**.
  - It emits a LogRecord with `event_name=gen_ai.client.inference.operation.details` and **structured** content.
  - Metrics follow the v1.41 conventions: `gen_ai.client.operation.duration`, and `gen_ai.client.token.usage` with `gen_ai.token.type`.
- **Exporters:** `opentelemetry-sdk==1.45.2` and `opentelemetry-exporter-otlp-proto-http==1.45.2`.

### Change surface

| Path | Purpose |
|---|---|
| `scripts/genai_integration.py` (new) | PEP 723 script, run with `uv run` |
| `scripts/genai_integration.py.lock` (new, generated by `uv lock --script scripts/genai_integration.py`) | Reproducible dependency resolution. Commit it. |

PEP 723 header (illustrative):

```python
# /// script
# requires-python = ">=3.10"
# dependencies = [
#   "openai>=1.60",
#   "opentelemetry-sdk==1.45.2",
#   "opentelemetry-exporter-otlp-proto-http==1.45.2",
#   "opentelemetry-instrumentation-genai-openai==1.3b0",
# ]
# ///
```

If `uv lock` fails on Python 3.14 because of missing wheels, add `--python 3.12` to the lock and run commands. Document that requirement in the script docstring and in [05](05-documentation.md).

### Environment contract

| Variable | Default | Meaning |
|---|---|---|
| `OPENCODE_GO_API_KEY` | (unset) | The secret. It is exported by the user's `~/.profile`. If it is unset or empty, print `SKIP: OPENCODE_GO_API_KEY not set` and exit 0. |
| `LOTEL_IT_REQUIRE` | unset | When it is `1`, a missing key is a failure (exit 1) instead of a skip |
| `LOTEL_IT_OPENCODE_MODEL` | `glm-5.3-flash` | A model whose native API is chat completions |
| `LOTEL_IT_OPENCODE_BASE_URL` | `https://opencode.ai/inference/go/openai/v1` | Override for endpoint moves |
| `LOTEL_BIN` | `lotel` | Same as `verify.py` |
| `OTLP_HTTP_ENDPOINT` | `http://localhost:4318` | Same as `verify.py` |

Invocation, documented in the script docstring and the README:

```
go build -o lotel ./cmd/lotel
zsh -c 'source ~/.profile && LOTEL_BIN=./lotel uv run scripts/genai_integration.py'
```

Prerequisite: `uv` on PATH. Fallback without `uv`:

```
python3 -m venv .venv-it
.venv-it/bin/pip install <the dependencies from the PEP 723 header>
.venv-it/bin/python scripts/genai_integration.py
```

`.venv-it/` must be added to `.gitignore` in WP8.

### Secret handling (hard requirements)

- Read the key only from `os.environ["OPENCODE_GO_API_KEY"]`. Never parse `~/.profile` from Python.
- Never print, log or write the key, or any prefix or suffix of it.
- Wrap all exception output in a `redact(text)` helper. It replaces any occurrence of the key with `<redacted>` before printing.
- The instrumentation does not record HTTP headers. After ingest, the script still checks this: it reads the bytes of every `*.jsonl` under `~/.lotel/data/{traces,metrics,logs}/` and fails if the key's bytes appear anywhere. The failure message must not echo the key.
- Never add the key to OTel resource or span attributes.
- Run every `lotel` subprocess with `env = {k: v for k, v in os.environ.items() if k != "OPENCODE_GO_API_KEY"}`.
- The leak scan covers these byte sources. The failure message names only the file, never the key.
  - `<data dir>/{traces,metrics,logs}/*.jsonl`
  - `~/.lotel/collector.log`
  - `<data dir>/lotel.db` and `<data dir>/lotel.db.wal` (if present)
  - `docker logs lotel-collector` output
  - The data dir is `~/.lotel/data`, matching `config.DataPath()` in `internal/config/config.go`.

### Flow

1. **Key gate:** apply the skip or require rule above.
2. **Collector:**
   - Run the same custom-config check as `verify.py` `check_collector_pinned`. If a collector runs with a non-default `config_path`, fail before changing anything.
   - Run `lotel health`. If it is not healthy, run `lotel start --wait` and set `started_here = True`.
   - If it is healthy, still run `lotel start --wait`. WP1 makes this a no-op for a current collector and a restart for a stale one.
   - Then assert the pinned image with `docker inspect`, as in WP7 `check_collector_pinned`. Do this before spending any API calls.
   - On a non-zero exit, fail with the stderr output.
3. **Model preflight:**
   - `GET <base without /openai/v1>/v1/models` with a 10 s timeout. Derive this URL from `LOTEL_IT_OPENCODE_BASE_URL` by replacing the trailing `/openai/v1` with `/v1/models`.
   - Fail with a clear message if the request errors or the model is absent. The message lists up to 10 available model IDs.
   - Do not fail when the model's reported native API is not chat completions. Warn instead, because the docs say translation happens server side.
3b. **Auth pre-gate:**
   - Make one uninstrumented chat call with `max_tokens=256`, using a plain `openai.OpenAI` client before `instrument()`.
   - On HTTP 401 or 403, fail with `auth rejected (HTTP <code>)`.
   - On HTTP 400 or 404, fail with `request rejected (HTTP <code>): <redacted error type/code>`.
   - Never print the response body unredacted.
   - This separates credential and endpoint problems from telemetry problems before the instrumented run.
4. **OTel setup:**
   - Set `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT=span_and_event` before calling `instrument()`.
   - Build `Resource({"service.name": S})` with `S = f"genai-it-{uuid8}"`.
   - Create a `TracerProvider` with a `BatchSpanProcessor(OTLPSpanExporter(endpoint=<ep>/v1/traces))`.
   - Create a `MeterProvider` with a `PeriodicExportingMetricReader(OTLPMetricExporter(<ep>/v1/metrics), export_interval_millis=2000)`.
   - Create a `LoggerProvider` with a `BatchLogRecordProcessor(OTLPLogExporter(<ep>/v1/logs))`.
   - Set all three providers globally, then call `OpenAIInstrumentor().instrument()`.
   - Verify the exact log-provider import path (`opentelemetry.sdk._logs`) against the pinned SDK version.
5. **Client:**
   - `openai.OpenAI(base_url=..., api_key=key, default_headers={"x-opencode-session": f"lotel-it-{uuid8}", "User-Agent": "lotel-genai-integration/1.0"}, timeout=60, max_retries=0)`. No retries, so each scenario call produces exactly one span.
6. **Scenario:** run these calls under one manual parent span named `lotel-it scenario`, using a script tracer:
   - **A:** a non-streaming chat. The user message is ``"Reply with exactly this token and nothing else: <nonce>"``. Use `max_tokens=256` and `temperature=0`.
   - **B:** a streaming chat with `stream_options={"include_usage": True}` and `max_tokens=256`. Consume the whole stream.
   - **C:** a chat with one `tools` definition (`get_weather(city: string)`), `tool_choice="auto"` and `max_tokens=256`. The budget leaves room for the model's reasoning tokens. Accept either a tool call or a text answer.
7. **Flush:** call `force_flush()` and then `shutdown()` on all three providers.
8. **Poll:** for up to 45 s, every 3 s, run `lotel ingest` (300 s subprocess timeout) and then `lotel query genai calls --service S`. Stop when at least 3 calls exist.
9. **Assertions:** use the same `test(name, fn)` harness as `verify.py`. Copy the harness into the script, because the scripts stay independent.
   1. At least 3 calls exist with `request_model == MODEL` and the scenario span's trace ID. Each has `operation == "chat"` and a non-empty `provider`. Print the observed span names and providers.
   2. Call A has `input_tokens > 0` and `output_tokens > 0`.
      - For call B, missing usage is a `WARN`, not a failure, because providers vary on `include_usage`.
   3. `query genai calls --service S --content`:
      - A's `input_messages` is a list whose serialized form contains `<nonce>`.
      - `content_source == "span"`.
      - This proves that JSON-string span content is decoded.
   4. `query logs --service S --event-name gen_ai.client.inference.operation.details` returns at least 1 record.
      - One record's `span_id` equals call A's span ID.
      - Its `attributes["gen_ai.input.messages"]` is a list. This proves structured event content survives.
   5. `query metrics --service S --name gen_ai.client.operation.duration` returns at least 1 histogram point with `len(bucket_counts) == len(explicit_bounds)+1`.
      - If none is found, accept `gen_ai.client.inference.duration`, the semconv-main name, and print which name was found.
   6. `query metrics --service S --name gen_ai.client.token.usage --attr gen_ai.token.type=input` returns at least 1 point.
      - Alternatively, accept `gen_ai.client.inference.usage.input_tokens`.
   7. `query metrics --service S --name traces.span.metrics.calls --attr gen_ai.request.model=MODEL` returns at least 1 point. Poll for this check within the same 45 s budget.
   8. The `query genai usage --service S --group-by model` row for MODEL has `input_tokens` equal to the sum of the per-call `input_tokens`.
   9. The key-leak scan described under Secret handling passes.
10. **Cleanup** (in `finally`):
    - Run `lotel prune --older-than 0h --service S`.
    - If `started_here` is set, run `lotel stop`.
11. **Exit:** 0 when everything passed or the run was skipped. Exit 1 otherwise. Print `Results: N passed, M failed, K warnings`.

### Acceptance

- `env -u OPENCODE_GO_API_KEY uv run scripts/genai_integration.py` prints `SKIP` and exits 0. Use `env -u` because the agent's shell may already export the key from the profile. No network calls happen and the collector is not started.
- With the key set, the full run exits 0 and reports every assertion as `PASS`.
- **If the implementer has no access to the key:** record WP8 as "live run unverified" in the PR and in the handoff. The SKIP path and `py_compile` must still pass. Success criterion 10's live half then stays open for the user to run. Do not claim it.
- Run `python3 -m py_compile scripts/genai_integration.py`. It must succeed.
- `grep -n "OPENCODE_GO_API_KEY" scripts/genai_integration.py` shows only environment reads, docs, and the skip message.

### Risks

- **Model removed from the catalog:** the preflight fails with the list of available model IDs. Set `LOTEL_IT_OPENCODE_MODEL` to one of them.
- **Rate limits:** OpenCode Go has 5-hour and weekly caps. The test makes 3 small calls per run.
- **Instrumentation drift:** if 1.3b0 changes its metric names, assertions 5 and 6 accept both the v1.41 names and the semconv-main names. Any other drift fails loudly with the names that were found.
- **`provider` value:** the instrumentation keys off the OpenAI client, so it likely reports `openai` even against OpenCode. The test asserts only that it is non-empty and prints the value.
