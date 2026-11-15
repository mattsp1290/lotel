# WP5 — Query surface: full-fidelity output, filters and `query genai`

## Goal

Agents can retrieve every stored GenAI signal and answer the common questions in a single command:

- Which model calls happened?
- How many tokens did they use?
- Which calls failed?
- What prompt or response was sent?

Prerequisite: WP3 and WP4 merged. Schema v2 must exist, and WP4 has already retyped `Attributes` to `map[string]any`.

## Repository evidence

- `internal/storage/query.go`:
  - The structs `TraceResult`, `MetricResult` and `LogResult` all have `Attributes map[string]string`.
  - `buildWhere(&query, opts, timeCol)` appends the service and time filters.
  - `QueryOptions{Service, Since, Until, Limit}`.
- `cmd/lotel/main.go:118-225`: a `query` parent with `traces`, `metrics`, `logs` and `aggregate` subcommands. The shared flags are bound in a loop at `main.go:218-223`. `parseQueryOpts` is at `main.go:287`.
- `.gitignore:7` (`lotel`) ignores every file under `cmd/lotel/` except the tracked `main.go`. WP1 changes it to `/lotel`. If WP5 runs before that, put new CLI code in `main.go` or fix `.gitignore` first.
- `cmd/lotel` has no tests (`[no test files]`).

## Change surface

| Path | Symbol | Change |
|---|---|---|
| `internal/storage/query.go` | `QueryOptions` | Add `Name string`, `TraceID string`, `EventName string`, `Attrs []AttrFilter` |
| `internal/storage/query.go` | `AttrFilter` (new) | `{Key, Value string}` |
| `internal/storage/query.go` | `buildWhere` | Adds the new filters (rules below) |
| `internal/storage/query.go` | `TraceResult` | (`Attributes` was already retyped in WP4.) Add `TraceState`, `StatusMessage`, `ResourceAttributes map[string]any`, `ScopeName`, `ScopeVersion`, `SchemaURL`, `Events []SpanEvent`, `Links []SpanLink`. |
| `internal/storage/query.go` | `SpanEvent`, `SpanLink` (new) | JSON tags match the stored JSON in [02](02-storage-schema-and-ingest.md#v2-tables) |
| `internal/storage/query.go` | `LogResult` | Add `ObservedTimestamp`, `EventName`, `BodyValue any` (tag `body_value`), `Flags`, `ResourceAttributes`, `ScopeName`, `ScopeVersion`. |
| `internal/storage/query.go` | `MetricResult` | Add `Description`, `StartTimestamp`, `Count *int64`, `Sum`, `Min`, `Max *float64`, `ExplicitBounds []float64`, `BucketCounts []int64`, `ExpScale *int`, `ExpZeroCount *int64`, `ExpPositive`, `ExpNegative *ExpBuckets`, `Quantiles []Quantile`, `ResourceAttributes`, `ScopeName`, `ScopeVersion`. Use `omitempty` on every new field. |
| `internal/storage/genai.go` (new) | `GenAICall`, `GenAIUsageRow`, `GenAIOptions`, `QueryGenAICalls`, `QueryGenAIUsage` | Defined below |
| `internal/storage/genai_test.go` (new) | — | Tests below |
| `cmd/lotel/main.go` | new flags, and a new `genai` subcommand with `calls` and `usage` children | Defined below |
| `cmd/lotel/main.go` | `parseAttrFlags([]string) ([]storage.AttrFilter, error)` (new) | Splits on the first `=`. An empty key is an error. |

### Filter semantics

| Flag | Commands | SQL |
|---|---|---|
| `--name` | traces (span name), metrics (metric name) | `name = ?` / `metric_name = ?` |
| `--trace-id` | traces, logs | `trace_id = ?` (lowercase hex) |
| `--event-name` | logs | `event_name = ?` |
| `--attr key=value` (repeatable, AND) | traces, metrics, logs | `json_extract_string(attributes, ?) = ?`, with the path **bound as a parameter**. The path is `'$."' + escape(key) + '"'`. `escape` replaces `\` with `\\` first, then `"` with `\"`. Keys containing control characters are rejected with an error. |

Semantics:

- The filter compares the **string form** produced by `json_extract_string`.
  - An int64 of 12 matches `=12`.
  - Integral doubles are stored by Go's JSON encoder without a fraction, so a double 1.0 matches `=1`. Large doubles use exponent form, so 1e21 matches only `=1e+21`. The review confirmed this with go-duckdb v1.8.5.
  - JSON null and missing keys never match.
  - A structured value matches only its compact JSON text.
- Document these rules in the `--attr` help text and in the README.
- During review, these behaviors were confirmed with go-duckdb v1.8.5:
  - Bound paths work.
  - `$."gen_ai.usage.input_tokens"` works.
  - `"` doubling fails with a `JSON path error`.
  - Backslash escaping works.

Keep the existing shared flag variables. Bind each new flag only on the commands listed in the table above.

## `lotel query genai calls`

Selection:

- SQL selects spans from `traces` where `CAST(attributes AS VARCHAR) LIKE '%"gen\_ai.%' ESCAPE '\'`, meaning any `gen_ai.*` key.
  - The selection does not depend on which keys the normalizer writes. WP1's normalizer observation gate records those keys.
  - A non-GenAI span whose *value* contains the text `"gen_ai.` would also match. Go then drops any candidate that has no `gen_ai.` key in its decoded attribute map.
- The query also applies `--service`, `--since`, `--until` and `--trace-id`.
- Ordering is **`start_time DESC`**, newest first. This command is for debugging recent runs, so `--limit N` returns the newest N calls. Document that this differs from the `ASC` order of the generic query commands.
- **SQL extraction:** each alias field is read with `COALESCE(json_extract_string(attributes, '$."k1"'), json_extract_string(attributes, '$."k2"'), ...)`, following the alias table below. Go never loads the full `attributes` JSON unless `--content` is set.
- `event_count` comes from one `LEFT JOIN (SELECT span_id, count(*) AS n FROM logs WHERE event_name LIKE 'gen\_ai.%' ESCAPE '\' GROUP BY span_id)`.
- `--model`, `--provider`, `--operation`, `--conversation-id` and `--errors-only` are applied in SQL on the coalesced expressions. Then `--limit` is applied in SQL.
- **Performance gate:** run `HOME=<scratch>/home ./lotel query genai usage` with **no `--since`**, against the scratch DB rebuilt in the WP4 performance gate. It must finish in under 5 s. Record the time in the PR. The snapshot data is months old, so a `--since` window would match nothing.

### Attribute alias table

`GenAICall` fields are resolved from span attributes. The first present key wins.

| Field (JSON tag) | Keys in priority order |
|---|---|
| `operation` | `gen_ai.operation.name` |
| `provider` | `gen_ai.provider.name`, `gen_ai.system` |
| `request_model` | `gen_ai.request.model` |
| `response_model` | `gen_ai.response.model` |
| `response_id` | `gen_ai.response.id` |
| `finish_reasons` ([]string) | `gen_ai.response.finish_reasons`. Accept an array, or a string holding a JSON array. |
| `conversation_id` | `gen_ai.conversation.id` |
| `agent_name` | `gen_ai.agent.name` |
| `tool_name` | `gen_ai.tool.name` |
| `input_tokens` | `gen_ai.usage.input_tokens`, `gen_ai.usage.prompt_tokens` |
| `output_tokens` | `gen_ai.usage.output_tokens`, `gen_ai.usage.completion_tokens` |
| `cache_read_input_tokens` | `gen_ai.usage.cache_read.input_tokens` |
| `cache_write_input_tokens` | `gen_ai.usage.cache_write.input_tokens`, `gen_ai.usage.cache_creation.input_tokens` |
| `reasoning_output_tokens` | `gen_ai.usage.reasoning.output_tokens` |
| `error_type` | `error.type`. If absent and `status_code == 2`, use `"error"`. |

Token fields are `*int64`. The value is coerced from int64, from float64 when it is integral, or from a decimal string. Any other value leaves the field nil.

Each call also has these fields:

- `trace_id`, `span_id`, `parent_span_id`, `span_name`, `service_name`, `start_time`
- `duration_ms` (float64)
- `status_code`
- `event_count`: the number of `logs` rows with the same `span_id` whose `event_name` starts with `gen_ai.`

### Content (`--content`)

Content is omitted by default to keep the output small. With `--content`, the call fields `input_messages`, `output_messages`, `system_instructions`, `tool_definitions`, `tool_call_arguments` and `tool_call_result` are populated from the matching `gen_ai.*` keys:

1. Use the span attribute first. If its value is a string that parses as a JSON array or object, emit the parsed value. Otherwise emit the value as-is.
2. If the span lacks the key, use the attributes of the `gen_ai.client.inference.operation.details` log event with the same `span_id`. Then fall back to that event's `body_value` when it is an object holding the key.
3. Set `content_source` to `"span"`, `"event"` or omit it.

Flags:

- `--service`, `--since`, `--until`, `--limit`, `--trace-id`
- `--model`: matches `request_model` or `response_model`
- `--provider`, `--operation`, `--conversation-id`
- `--errors-only`: `error_type != ""`
- `--content`

## `lotel query genai usage`

The command reuses the `QueryGenAICalls` SQL (no limit, no content columns) and groups the results in Go. Only the extracted scalar fields are loaded.

- `--group-by` takes a comma-separated list from `provider`, `model`, `operation`, `service` and `conversation`. The default is `provider,model,operation`.
  - `model` resolves to `response_model`, then falls back to `request_model`.
  - A missing group value becomes `""`.
- Each output row (`GenAIUsageRow`) contains:
  - the group fields;
  - `calls` and `errors`;
  - the sum of each token field, where nil counts as 0;
  - `calls_missing_usage`: the number of calls whose `input_tokens` and `output_tokens` are both nil;
  - `total_tokens = input + output`;
  - `duration_ms {avg, p50, p95, max}` using nearest-rank percentiles.
- Rows are ordered by `total_tokens DESC`, then by group fields `ASC`.
- Flags are the same filters as `calls`, without `--content` or `--limit`.

## `lotel query aggregate`

The behavior is unchanged. It still aggregates the `value` column, which holds the per-point `sum` for histograms. Add one line to its `Short` help text: for histograms with cumulative temporality, use `query metrics --name <m>` and read the latest point. Fixing cumulative aggregation is out of scope.

## Tests and acceptance

`internal/storage/genai_test.go` (new) ingests the WP4 fixtures from `internal/storage/testdata/genai/`:

1. `TestQueryGenAICalls_Aliases`:
   - Span (a) resolves `provider`, `operation`, `input_tokens` and `finish_reasons`.
   - Span (d) resolves `provider` from `gen_ai.system`, and the token values from `prompt_tokens` and `completion_tokens`.
   - Span (c) has `tool_name`.
2. `TestQueryGenAICalls_ContentParsing` runs with content enabled:
   - Span (a) `input_messages` is a decoded `[]any`, not a string.
   - Span (b) `output_messages` is `[]any`.
   - Span (a) `event_count == 1`.
3. `TestQueryGenAICalls_ContentFromEvent` covers a span without content attributes whose log event carries them: `content_source == "event"`.
4. `TestQueryGenAIUsage_Grouping` checks exact token sums per provider and model group, the `errors` count, `calls_missing_usage`, and that p50 is no greater than p95, which is no greater than max.
5. `TestQueryFilters`:
   - `--attr gen_ai.request.model=<m>` on traces;
   - `--event-name` on logs;
   - `--name` on metrics;
   - `--trace-id` on traces and logs;
   - two `--attr` filters combine with AND;
   - a key containing `.`, `"` and `\` matches an exact fixture attribute with that key;
   - a key with a control character returns an error;
   - an integral double attribute matches `=1`, and a `1e21` attribute matches `=1e+21`.
6. `TestQueryGenAICalls_OrderAndLimit`: `--limit 1` returns the newest call.
7. `TestQueryGenAICalls_SelectsNormalizedOnly` uses a span with only `gen_ai.usage.input_tokens` and `gen_ai.request.model`. It is selected.

CLI acceptance (manual, after ingesting the fixtures through a real collector in WP7):

- `./lotel query genai calls --since 15m | jq '.[0].provider'` prints a non-empty string.
- `./lotel query genai usage --since 15m` prints a JSON array.
- An empty result prints `[]`, not `null`. Fix this **in the CLI only** with `if results == nil { results = []T{} }` before `printJSON`, for the existing three query commands and the new `genai` commands. The storage functions keep returning nil slices, because the existing `TestQueryEmptyDB` (`ingest_test.go:139`) asserts nil.

## Exclusions

- No cost or pricing calculation.
- No streaming or tail mode.
- No full-text search over message content. Use `--content` with `jq`.
