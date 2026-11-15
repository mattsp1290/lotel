# Complete OTLP storage

The goal is to ingest the same telemetry signals and fields as Collector v0.165.0.

## Existing behavior

The Collector already used v0.165.0. Its managed pipelines wrote traces, metrics,
and logs as JSONL. Incremental ingestion flattened selected fields into DuckDB.
Profiles had no pipeline. Handwritten projections omitted supported metadata,
exemplars, typed value identity, and exact numeric values.

## Selected shape

`otlp_envelopes` stores complete OTLP JSON with one resource group per row.
The four-signal registry owns resource, scope, and record names. Raw JSON values
retain every field and number without decoding them through floating point.
Top-level fields, including profile dictionaries, stay with each resource.

`QueryOTLP(db, signal, opts)` returns the payload and its signal, service, and
optional exact timestamp range. `query profiles` uses this API for profiles.
The existing query projections retain their behavior.

Service filtering and retention operate on resources. Profile attributes can
reference the dictionary string table, so service indexing resolves those
references without changing the stored payload. Time bounds select overlapping
resource ranges. They return complete envelopes and never rewrite sample or
datapoint arrays. Undated envelopes have null timestamp indexes.

Ingestion commits the complete payload, supported projections, and one file
cursor in a transaction. Unsupported projection decoding has a distinct report
count. Database failures roll back rows and progress together.

Schema v3 upgrades v2 additively. Existing rows and file offsets remain.
Complete OTLP storage uses the existing file position, so old pruned data does
not reappear automatically. Complete OTLP history begins with new ingestion.

Prune reports distinguish projected records from complete OTLP envelopes.
Age-based retention keeps an envelope until its latest timestamp is old.
Explicit all-data pruning includes undated and future-dated telemetry.

## Alternatives and synthesis

Two independent design candidates compared resource envelopes with contextual
per-record envelopes. A separate architecture review selected resource envelopes.
Per-record storage makes time selection finer but duplicates dictionaries and
requires more signal-specific splitting. Expanding handwritten types would
continue to omit future fields.

The synthesis uses resource envelopes with nullable unsigned nanosecond indexes.
It rejects receipt-time substitution for absent timestamps. External model
families prescribed by the workflow were unavailable. The candidate authors
and reviewer used the inherited model.

## Constraints

- Time filters and limits operate on resource envelopes.
- Shared profile dictionaries may contain unused entries from other resources.
- Existing v2 history retains its previous convenience projections.
- Profiles use an experimental protocol tied to the pinned Collector schema.
- The actual v0.165.0 HTTP profiles route is `/v1development/profiles`.
- Receiver formats remain configurable through custom Collector configuration.

## Principles applied

Model the Domain selected a signal registry and complete resource envelopes.
Foundational Thinking put source comparison and migration constraints before
implementation. Prove It Works required a live source-to-query comparison.
Sequence Work into Verifiable Units separated pipeline validation, storage
tests, and complete CLI verification.
