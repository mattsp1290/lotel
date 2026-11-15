# WP9 — Documentation and agent workflow

## Goal

A user or coding agent can learn the following from `README.md` and `AGENTS.md` alone:

- how to instrument an LLM app for lotel;
- which collector GenAI features are active;
- how to query GenAI telemetry;
- how to redact content;
- how to run both verification scripts.

Prerequisites: WP1–WP8 merged. The documentation must describe the shipped behavior, not this plan.

## Repository evidence

- `README.md` has these sections: `## Commands`, `## Query Options`, `## Output Contract`, `### AI Coding Agent Workflows`, `## Architecture`, `## Data Storage`, `## Configuration`, `## Verification` and `## Requirements`.
- `AGENTS.md` describes workflows A, B and C with `./lotel query ...` examples. It also has a `## Key paths` list.

## Changes

`README.md`:

1. **Commands table:**
   - Add the rows `lotel query genai calls` and `lotel query genai usage`.
   - Change the `lotel ingest` description to "Incrementally ingest new JSONL telemetry (JSON report)".
2. **Query Options:**
   - Add `--name`, `--trace-id`, `--event-name` and `--attr key=value` (repeatable), and say which commands accept each flag.
   - Document the `--attr` string-form matching rules: integers match, `1.0` does not match `1`, and nulls never match.
   - Add the `genai` flags.
   - State that `query genai calls` orders newest first.
3. **New `## GenAI / LLM observability` section** (anchor `#genai`, which WP1's stderr warning references). It contains:
   - What the collector does: `gen_ai_normalizer` with the OpenInference and OpenLLMetry sources, and `spanmetrics` with its GenAI dimensions. Say that spanmetrics covers all spans.
   - What lotel stores: typed attributes, span events and links, `event_name`, and histogram buckets.
   - A Python quickstart with environment variables:
     - `OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318`
     - `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`
     - `OTEL_LOGS_EXPORTER=otlp`
     - `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT=span_and_event`
     - the package `opentelemetry-instrumentation-genai-openai`
   - Example `query genai calls`, `--content` and `query genai usage` commands, with a trimmed sample output.
   - Redaction: copy `examples/collector/genai-redact-content.yaml` or `genai-hash-content.yaml` to `./lotel-collector.yaml`, then run `lotel stop && lotel start --wait`.
   - A semconv note: GenAI conventions are in Development status. lotel stores attributes verbatim and resolves the aliases listed in `internal/storage/genai.go`.
4. **Configuration:**
   - Document the managed-config header.
   - Document that a header-less or older file is backed up to `collector-config.yaml.bak.<ts>` and replaced.
   - State that custom config belongs in `./lotel-collector.yaml`.
   - Document the pinned image tag.
   - Document that `lotel start` restarts a running collector whose image or config changed.
   - Note that spanmetrics uses delta temporality.
   - Note that file-exporter rotation is unsupported.
   - Note that the file exporters use `append: true`. Raw JSONL survives restarts and grows until the user deletes it.
   - Warn that a custom `./lotel-collector.yaml` must also set `append: true`.
5. **Data Storage:**
   - Schema v2.
   - Rebuild-on-upgrade from raw JSONL, including the note that pruned rows reappear after a rebuild.
   - `ingest_state` offset tracking.
6. **Verification:**
   - Fix the `verify.py` prerequisites.
   - Add an "Optional live LLM test" subsection with the `zsh -c 'source ~/.profile && LOTEL_BIN=./lotel uv run scripts/genai_integration.py'` command.
   - State the skip behavior and list the `LOTEL_IT_*` variables.
   - Note the `uv` prerequisite and the venv fallback.
   - Add the Python 3.12 note if WP8 needed it.

`AGENTS.md`:

1. **Workflow A:**
   - Add the step: "For LLM features, confirm `./lotel query genai calls --service <svc> --since 10m` lists the expected model calls with non-null token counts."
2. **Workflow B:**
   - Add `./lotel query genai calls --errors-only --content --since 30m`.
   - Add `./lotel query logs --event-name gen_ai.client.inference.operation.details`.
3. **Workflow C checklist:**
   - Add: "LLM calls emit `gen_ai.operation.name`, `gen_ai.provider.name` and `gen_ai.usage.*` attributes."
4. **Key paths:**
   - Add `internal/storage/genai.go`, `examples/collector/`, `scripts/genai_integration.py`.
5. **Core command surface list:**
   - Add the two `genai` commands.

## Acceptance

- Every command shown in `README.md` and `AGENTS.md` runs successfully against a collector populated by `scripts/verify.py`. Check this manually by copy-pasting each command.
- `grep -n "otelcol-contrib installed" README.md scripts/verify.py` returns nothing.
- Every relative link in `README.md` resolves to an existing file.
