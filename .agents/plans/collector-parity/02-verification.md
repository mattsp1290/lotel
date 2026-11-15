# Verification evidence

## Automated checks

The following commands completed successfully on this change.

```bash
go test ./...
go build ./...
go vet ./...
LOTEL_DOCKER_TESTS=1 go test ./internal/config -count=1
git diff --check
```

Storage tests cover exact JSON fidelity for four signals and all five metric
types, uint64 timestamps and counts, unknown fields, typed values, profile
dictionary service lookup, empty envelopes, timestamp-free logs, time and
service filters, incremental ingestion, partial lines, transaction rollback,
additive v2 migration, and dated, undated, and future-data retention.

## Live CLI checks

The existing verification script completed with 23 passed and zero failed.
Its service tag was `verify-8beff2bf`. This covered the established query paths,
GenAI normalization, spanmetrics, aggregation, incremental ingestion, and prune.
The script stopped the Collector. It was restarted afterward.

The complete OTLP verification script then passed with service tag
`verify-otlp-c77225727f2a` against `otel/opentelemetry-collector-contrib:0.165.0`.

```bash
go build -o lotel ./cmd/lotel
./lotel start --wait
LOTEL_BIN=./lotel python3 scripts/verify_otlp.py
./lotel health
```

It compared complete queried resource envelopes with actual exported JSONL for
traces, metrics, logs, and profiles. It checked all five metric kinds, exact
integer values above 2^53, maximum uint64 counts, exemplars, the profiles command,
and unchanged multiplicities of existing envelopes after another ingestion.
New spanmetrics output can arrive between ingestion calls.

The final health check passed. Verification data remains under its unique service
tag. The Collector remains running.

## Review and corrections

An independent architecture review accepted the resource-envelope design.
The comment review found no actionable issues. A separate correctness review
found no additional actionable issues after the profile dictionary correction.
Reviewers used the inherited model because the configured external model
families were unavailable. The external cleanup and CLI control skills were
not installed. Direct diff review and live CLI checks covered those actions.

Live verification found two useful corrections. The release's source uses
`/v1development/profiles`, although its README described `/v1/profiles`.
Profile resource attributes use dictionary indexes after export, so service
lookup now resolves those indexes. Both corrections are reflected in the
verification script. The dictionary behavior also has a storage regression test.
