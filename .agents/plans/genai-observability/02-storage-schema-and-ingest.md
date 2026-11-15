# WP2–WP4, WP6 — Storage: OTLP model, schema v2, lossless incremental ingest, prune

These packages make `lotel ingest` keep everything GenAI telemetry depends on and ingest each record exactly once.

## Repository evidence

- `internal/storage/ingest.go` holds the OTLP decode structs.
  - The `otlpAttr`/`otlpValue` scalar-only decode lives there.
  - `flattenAttrs` returns `map[string]string`.
  - Per-signal ingest uses `bufio.Scanner` with a 10 MiB cap and never checks `scanner.Err()`.
- `internal/storage/db.go` `migrate` uses `CREATE TABLE IF NOT EXISTS` with no version record. `DB()` is a `sync.Once` singleton. `OpenDB(path)` is used by the tests.
- `internal/storage/prune.go` `Prune` hard-codes `signals` and `timeCols`.
- `internal/storage/ingest_test.go:222` `writeTestJSONL` writes fixtures under `<tmp>/data/<signal>/<signal>.jsonl`.
- `internal/storage/storage_test.go:18` inserts rows with explicit v1 column lists. Those tests must change with the schema.

OTLP JSON shape facts come from pdata v1.71.0 `JSONMarshaler`, which the contrib 0.165 file exporter uses:

- `intValue`, all `*TimeUnixNano`, `count`, `bucketCounts[]` and `zeroCount` are **decimal strings**.
- Doubles are numbers. `"NaN"`, `"Infinity"` and `"-Infinity"` appear as strings.
- `bytesValue` is base64.
- `arrayValue` has the form `{values:[AnyValue]}`. `kvlistValue` has the form `{values:[{key,value}]}`.
- Span `events[]` is `{timeUnixNano,name,attributes}`. `links[]` is `{traceId,spanId,traceState,attributes,flags}`.
- LogRecord `eventName`. Histogram `{count,sum,min,max,bucketCounts,explicitBounds,startTimeUnixNano,timeUnixNano}`.
- `exponentialHistogram` `{scale,zeroCount,zeroThreshold,positive{offset,bucketCounts},negative{...}}`. Summary `{count,sum,quantileValues[{quantile,value}]}`.
- `scope{name,version,attributes}`. `schemaUrl` appears at both the resource and scope levels.

## WP2 — OTLP model and typed AnyValue

### Change surface

| Path | Symbol | Change |
|---|---|---|
| `internal/storage/otlp.go` (new) | `otlpAttr`, `otlpValue`, `otlpNano`, metric structs | Move these from `ingest.go`. Extend `otlpValue` with `BytesValue *string`, `ArrayValue *otlpArray`, `KvlistValue *otlpKvlist`. `DoubleValue` becomes `*otlpDouble`. |
| `internal/storage/otlp.go` | `otlpDouble` (new) | `UnmarshalJSON` accepts a JSON number or the strings `"NaN"`, `"Infinity"` and `"-Infinity"`. It records whether the value is finite. **Every OTLP double field uses it:** `doubleValue`, `asDouble`, histogram `sum`/`min`/`max`, `explicitBounds[]`, quantile `value`, and exponential `sum`/`min`/`max`/`zeroThreshold`. Without it, one NaN fails `json.Unmarshal` for the entire batch line. |
| `internal/storage/otlp.go` | `(otlpValue) Any() any` (new) | Typed conversion (table below) |
| `internal/storage/otlp.go` | `(otlpValue) String() string` | Keep for scalars. Structured values return the compact JSON of `Any()`. |
| `internal/storage/otlp.go` | `attrsToMap([]otlpAttr) map[string]any` (new) | Replaces `flattenAttrs`. The last duplicate key wins. |
| `internal/storage/otlp.go` | `otlpResource`, `otlpScope` (new) | `{attributes}` and `{name,version,attributes}` |
| `internal/storage/otlp.go` | `otlpSpanEvent`, `otlpSpanLink`, `otlpExpHistogram`, `otlpSummary` (new) | Shapes as listed above |
| `internal/storage/ingest.go` | `flattenAttrs` | **Keep in WP2.** It still has three callers. WP4 deletes it after replacing every caller with `attrsToMap`. |

`Any()` conversion:

| OTLP | Go value stored in JSON |
|---|---|
| `stringValue` | `string` |
| `boolValue` | `bool` |
| `intValue` (decimal string) | `int64` via `strconv.ParseInt`. On parse failure, keep the original string. |
| `doubleValue` number | `float64` |
| `doubleValue` `"NaN"`, `"Infinity"` or `"-Infinity"` | that string. JSON has no NaN. |
| `bytesValue` | base64 `string`, unchanged |
| `arrayValue` | `[]any` of the converted elements |
| `kvlistValue` | `map[string]any` |
| empty `{}` | `nil` |

Invariant: `json.Marshal(attrsToMap(x))` never fails.

Do **not** parse JSON-string attribute values at ingest. Storage is verbatim. JSON-string decoding of GenAI content happens only in `query genai` (see [03](03-query-surface.md)).

### Tests (WP2)

`internal/storage/otlp_test.go` (new) uses a table test over every row of the conversion table. It must include:

- nested `arrayValue` containing a `kvlistValue`;
- an int64 of `9007199254740993`, which must round-trip exactly;
- `"NaN"`;
- an empty value;
- `otlpDouble` decoding of a number, `"NaN"`, `"Infinity"` and `"-Infinity"`.

Name the table test `TestAnyValue`. The WP2 gate command in [06](06-execution-handoff.md) runs it by this name.

Storage rule for non-finite doubles:

- In typed SQL columns (`value`, `sum`, `min`, `max`), they are stored as SQL NULL.
- In JSON columns (`explicit_bounds`, `quantiles`), they are stored as the string form.
- In attributes, they keep the string form from the conversion table.

## WP3 — Schema v2, versioning and rebuild

### Change surface

| Path | Symbol | Change |
|---|---|---|
| `internal/storage/db.go` | `schemaVersion` (new const) | `2` |
| `internal/storage/db.go` | `migrate` | Rewrite to follow the rebuild procedure |
| `internal/storage/db.go` | `SchemaRebuilt` (new exported package var, bool) | `migrate` sets it true **only when it dropped existing v1 tables**, not on a fresh DB. The CLI prints the notice from a shared helper that every command uses to open the DB. |

### Schema versioning and rebuild

1. `CREATE TABLE IF NOT EXISTS schema_meta (key VARCHAR PRIMARY KEY, value VARCHAR)`.
2. Read `schema_version`.
   - If it is equal to `2`, create any missing tables with `IF NOT EXISTS` and return.
   - If it is absent or less than 2, check whether a `traces` table already exists (`information_schema.tables`). Drop `traces`, `metrics`, `logs` and `ingest_state` if they exist. Create the v2 tables. Upsert `schema_version=2`. Set `SchemaRebuilt=true` only if `traces` existed before the drop.
   - If it is greater than 2, return the error `database schema v<N> is newer than this lotel (v2); upgrade lotel or delete <path>`.
3. Run the whole procedure in one transaction.

Because `ingest_state` is dropped, the next `lotel ingest` re-reads all raw JSONL. That rebuilds history, including rows that `lotel prune` had deleted. Document this in [05](05-documentation.md).

### v2 tables

Keep the current column names where they exist. New columns are marked ★.

`traces`:

- `trace_id`, `span_id`, `parent_span_id`, ★`trace_state` VARCHAR
- `name`, `kind`, `start_time`, `end_time`, `duration_ns`
- `status_code`, ★`status_message` VARCHAR
- `service_name`, `attributes` JSON
- ★`resource_attributes` JSON, ★`scope_name` VARCHAR, ★`scope_version` VARCHAR, ★`schema_url` VARCHAR
- ★`events` JSON: an array of `{time, name, attributes}` with `time` in RFC3339Nano UTC
- ★`links` JSON: an array of `{trace_id, span_id, trace_state, attributes}`
- `date`

`logs`:

- `timestamp`, ★`observed_timestamp`
- `severity`, `severity_number`
- ★`event_name` VARCHAR
- `body` VARCHAR: scalars use their string form. Structured bodies use compact JSON.
- ★`body_json` JSON: the typed `Any()` value. NULL when the body is empty.
- `service_name`, `trace_id`, `span_id`, ★`flags` INTEGER
- `attributes` JSON, ★`resource_attributes` JSON, ★`scope_name`, ★`scope_version`
- `date`

Rule: when `timeUnixNano` is 0, `timestamp` uses `observedTimeUnixNano`. If both are 0, the record is not inserted and is counted in `SkippedRecords`. `date` derives from `timestamp`.

`metrics`:

- `metric_name`, `metric_type` (`sum` | `gauge` | `histogram` | ★`exponential_histogram` | ★`summary`)
- ★`description`
- `value` DOUBLE: holds the point value for sum and gauge, and `sum` for the other three. This keeps `AggregateMetrics` meaningful.
- ★`start_timestamp`, `timestamp`, `service_name`
- `aggregation_temporality`, `is_monotonic`, `unit`
- ★`count` BIGINT, ★`sum` DOUBLE, ★`min` DOUBLE, ★`max` DOUBLE
- ★`explicit_bounds` JSON, ★`bucket_counts` JSON: an array of int64
- ★`exp_scale` INTEGER, ★`exp_zero_count` BIGINT, ★`exp_positive` JSON, ★`exp_negative` JSON: each `{offset, bucket_counts}`
- ★`quantiles` JSON: `[{quantile, value}]`
- `attributes`, ★`resource_attributes`, ★`scope_name`, ★`scope_version`
- `date`

`ingest_state` (new):

- `path` VARCHAR PRIMARY KEY: the absolute file path
- `byte_offset` BIGINT. Do not use the name `offset`, which is an SQL keyword.
- `fingerprint` VARCHAR: lowercase hex SHA-256 of the file's first `fingerprint_len` bytes at the time of the last ingest
- `fingerprint_len` INTEGER: `min(size, 4096)` at the time of the last ingest
- `updated_at` TIMESTAMP

### Tests (WP3)

- `TestMigrate_FreshCreatesV2` creates a new DB. It asserts `schema_version=2`, that every v2 column exists (query `information_schema.columns`), and that `SchemaRebuilt == false`.
- Columns named `count`, `sum`, `min` and `max` must be double-quoted in every DDL statement and every SELECT. `TestMigrate_FreshCreatesV2` must insert and select one metrics row that sets all four.
- `TestMigrate_RebuildsV1` builds a temp DB with the v1 DDL (copy the current DDL into the test) and one row. After `OpenDB`, it asserts that the tables are empty with v2 columns and that `SchemaRebuilt` is true.
- `TestMigrate_RejectsNewer` sets `schema_version=3`. It asserts that `OpenDB` returns the error.
- Update `storage_test.go` direct inserts to v2 column lists.

## WP4 — Lossless incremental ingest

### Change surface

| Path | Symbol | Change |
|---|---|---|
| `internal/storage/ingest.go` | `IngestAll(db *sql.DB, dataPath string) (*IngestReport, error)` | The signature gains the report return |
| `internal/storage/ingest.go` | `IngestReport` (new) | `{Signals map[string]*SignalReport}` with JSON tag `signals` |
| `internal/storage/ingest.go` | `SignalReport` (new) | `{Files int, Lines int, Records int, SkippedLines int, SkippedRecords int, Bytes int64}` with snake_case JSON tags. `SkippedLines` counts malformed JSON lines. `SkippedRecords` counts valid lines' records that were rejected, for example a log with no timestamps. |
| `internal/storage/ingest.go` | `ingestFile(db, signal, path string, rep *SignalReport) error` (new) | Offset-aware reader shared by all signals |
| `internal/storage/ingest.go` | `signalIngester` (new interface) | `prepare(tx *sql.Tx) error`, `decode(line []byte) (records int, skipped int, err error)`, `close() error`. `prepare` and `close` run once per file transaction. |
| `internal/storage/ingest.go` | `traceIngester`, `metricIngester`, `logIngester` (new) | Replace `ingestTraces`, `ingestMetrics` and `ingestLogs`. They populate every v2 column and use `attrsToMap`. Delete `flattenAttrs`. |
| `internal/storage/query.go` | `TraceResult`, `MetricResult`, `LogResult` `.Attributes` | **Retype to `map[string]any` in WP4, not WP5.** Typed JSON written by WP4 fails to unmarshal into `map[string]string`, and today's code ignores that error at `query.go:97,141,184`, so the attributes would silently come back empty. Also check those `json.Unmarshal` errors and return them wrapped. |
| `cmd/lotel/main.go` | `openDB() (*sql.DB, error)` (new helper) | Wraps `storage.DB()`. If `storage.SchemaRebuilt` is true, it prints the stderr notice once: `lotel: database rebuilt for schema v2; run 'lotel ingest' to reload raw telemetry`. Every command that calls `storage.DB()` uses this helper instead. |
| `internal/storage/ingest.go` | `extractDataPoints` | Add `exponentialHistogram` and `summary`. Histograms carry count, sum, min, max, bounds and buckets. |
| `cmd/lotel/main.go` | `ingestCmd` | Print `IngestReport` with `printJSON` instead of `Ingestion complete.` Open the DB through `openDB()`. |

### File discovery

For each signal in `traces`, `metrics` and `logs`, ingest exactly `<dataPath>/<signal>/<signal>.jsonl`, as today. A missing file is not an error.

File-exporter rotation is **unsupported**. A rotated file gets a new name and would be fully re-ingested, which creates duplicates. The default config does not enable rotation. Document this in [05](05-documentation.md).

### `ingestFile` algorithm

```
open file; size := stat.Size()
row := SELECT byte_offset, fingerprint, fingerprint_len FROM ingest_state WHERE path=?
start := 0
if row exists
   && row.byte_offset <= size
   && sha256(first row.fingerprint_len bytes of file) == row.fingerprint:
     start = row.offset
fpLen := min(size, 4096); fp := sha256(first fpLen bytes)
seek(start); reader := bufio.NewReaderSize(f, 1<<20)
BEGIN tx
loop: line, err := reader.ReadBytes('\n')
   if err == io.EOF: break   // partial trailing line: NOT consumed, offset excludes it
   consumed += len(line)
   decode line → on JSON error: SkippedLines++ ; continue
   insert records
UPSERT ingest_state(path, start+consumed, fp, fpLen, now)
COMMIT
```

Rules:

- `ingest_state` stores `fingerprint_len`, the number of bytes hashed. On a later run, hash that many leading bytes of the current file and compare. If the result differs, or the stored `byte_offset` is greater than the current size, the file was replaced or truncated. Restart from 0.
- Per-row inserts use prepared statements inside the file transaction.
- **Performance gate:** a full ingest of the developer's real history must finish in under 60 s. The history is about 56 MB across the three JSONL files. Run it as `HOME=<scratch>/home ./lotel ingest`, with `<scratch>/home/.lotel/data` copied from the WP1 snapshot (`<scratch>/lotel-data-snapshot`). Never touch the real `~/.lotel/data`. If the gate fails, switch inserts to the go-duckdb Appender, issuing `BEGIN` and `COMMIT` on the same raw connection so the `ingest_state` upsert stays atomic. Record the measured time in the PR.
- Insert rows and update `ingest_state` in **one transaction per file**. A crash before commit re-ingests that file's new tail on the next run, with no duplicates and no loss.
- `ReadBytes` has no line-length cap. This removes the 10 MiB `bufio.Scanner` limit and its silent stop.
- Return any non-EOF read error.
- A malformed JSON line is skipped and counted. It is never fatal. This matches today's tolerance and is now visible in the report.

### Decode details

- `service_name` keeps using `extractServiceName` over resource attributes. The default stays `"unknown"`.
- Store `resource_attributes` and the scope fields on every row from that resource or scope block.
- `duration_ns` is computed as today.
- Metric `count` and `bucket_counts` arrive as decimal strings. Parse them to int64. On a parse error, store NULL for that field and keep the row.

### Tests (WP4)

Use fixtures under `internal/storage/testdata/genai/` (new). Keep the fixture files **flat** in that directory. `.gitignore` ignores any directory named `traces/`, `metrics/`, `logs/` or `data/`, so never create those subdirectories under `testdata`. Tests copy the flat files into `<tmp>/data/<signal>/<signal>.jsonl` at runtime.

Fixture files:

- `traces.jsonl`. One line must contain at least these spans:
  - **(a)** A native OTel chat span. `gen_ai.input.messages` is a **JSON string**, matching the Python span shape. It also carries `gen_ai.usage.input_tokens` (intValue), `gen_ai.response.finish_reasons` (arrayValue of strings), one span event and one link.
  - **(b)** A span whose `gen_ai.output.messages` is an `arrayValue` of `kvlistValue`, matching the semconv-main shape.
  - **(c)** An `execute_tool` span that is a child of (a).
  - **(d)** A span with legacy `gen_ai.system` and `gen_ai.usage.prompt_tokens`/`completion_tokens` attributes.
- `logs.jsonl`: a record with `eventName: gen_ai.client.inference.operation.details`, `severityNumber: 5`, structured `gen_ai.input.messages` (arrayValue) in its attributes, a `kvlistValue` body, and trace and span IDs that match span (a).
- `metrics.jsonl`: these metrics:
  - `gen_ai.client.token.usage` histogram, with bounds `[1,4,16,...]`, two points (`gen_ai.token.type` input and output), and bucket_counts of length bounds+1;
  - `gen_ai.client.operation.duration` histogram;
  - one `exponentialHistogram`;
  - one `summary`;
  - one `sum`.
- Write the fixtures in the exact pdata JSON form described under Repository evidence.

Test cases:

0. Update `TestMigrateAndQuery` (`storage_test.go:18`) to type-assert `results[0].Attributes["http.method"].(string)`.
1. `TestIngestGenAIFixtures` copies the fixtures into `<tmp>/data/...`, runs `IngestAll`, and asserts each of these via SQL or the query functions:
   - span (a) `attributes['gen_ai.input.messages']` is a JSON string;
   - span (b) `gen_ai.output.messages` is a JSON array;
   - `events` has one element with the right name, and `links` has one element;
   - the log has `event_name`, `body_json` is an object, and its attributes hold the array;
   - the histogram row has `len(bucket_counts) == len(explicit_bounds)+1`, and `count` and `sum` match;
   - the exponential histogram and summary rows exist with `exp_*` and `quantiles` populated;
   - `resource_attributes` and `scope_name` are populated.
2. `TestIngestIdempotent` is **rewritten**. It uses the fixture files for all three signals, including metrics. A double ingest leaves every count unchanged, and the second report shows `Records: 0` for every signal.
3. `TestIngestIncrementalAppend` ingests, appends one line, and ingests again. Only the new record is added.
4. `TestIngestPartialTrailingLine` writes a final line without `\n`. It is not ingested. After the newline and the rest of the line are appended, it is ingested exactly once.
5. `TestIngestTruncatedFileRestarts` ingests, replaces the file with different content, and ingests again. The new content is ingested from 0.
6. `TestIngestMalformedLineCounted` uses one garbage line between two valid ones. `SkippedLines == 1` and both valid records are present.
7. `TestIngestLongLine` uses a single line larger than 11 MiB (generated in the test). It is ingested.
8. `TestIngestTypedAttributesQueryable` ingests the fixture through `QueryTraces`. `Attributes["gen_ai.usage.input_tokens"]` is a number (`float64` after JSON decode), not missing.
9. `TestIngestNonFiniteDoubles` uses one metrics line holding a NaN gauge point plus a normal sum metric. Both rows are inserted, and the gauge `value` is NULL. `SkippedLines == 0`.

## WP6 — Prune with schema v2

### Change surface

`internal/storage/prune.go` keeps its logic. The table names and time columns are unchanged.

Add to the `Prune` doc comment: pruned rows can reappear after a schema rebuild, because raw JSONL is retained.

### Tests

Update `TestPrune` (`storage_test.go:98`) inserts to the v2 columns. Behavior assertions are unchanged.

## Exclusions

- Exemplars are not stored.
- Span `droppedAttributesCount` is not stored.
- Profiles signal is not ingested.
- JSONL files are not deleted or rotated by lotel.
