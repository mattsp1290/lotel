# Execution handoff

Implementation is on the feature branch. See [acceptance evidence](07-implementation-evidence.md). The packages below describe the original execution order. Each package is one reviewable PR, unless the Parallelization column says otherwise.

## Work packages

| # | Package | Spec | Changes | Depends on | Parallelization |
|---|---|---|---|---|---|
| WP1 | Collector pipeline | [01](01-collector-pipeline.md) | `internal/collector/collector.go` (`defaultImage`, `State.ConfigSHA256`, `restartDecision`, `Start`); `internal/collector/collector_test.go`; `.gitignore` (`lotel` → `/lotel`); `internal/config/config.go` (`DefaultConfig`, managed header, backup and replace, `Notices`); `internal/config/config_test.go`; `internal/config/examples_test.go` (new); `examples/collector/*.yaml` (new). Step zero: snapshot `~/.lotel/data` before any collector start. | — | Can run in parallel with WP2 |
| WP2 | OTLP model and typed AnyValue | [02 §WP2](02-storage-schema-and-ingest.md#wp2--otlp-model-and-typed-anyvalue) | `internal/storage/otlp.go` (new), `internal/storage/otlp_test.go` (new), `internal/storage/ingest.go` (types moved out; `flattenAttrs` kept) | — | Can run in parallel with WP1 |
| WP3 | Schema v2 and rebuild | [02 §WP3](02-storage-schema-and-ingest.md#wp3--schema-v2-versioning-and-rebuild) | `internal/storage/db.go`, `internal/storage/storage_test.go` | WP2 | Sequential |
| WP4 | Lossless incremental ingest | [02 §WP4](02-storage-schema-and-ingest.md#wp4--lossless-incremental-ingest) | `internal/storage/ingest.go`, `internal/storage/ingest_test.go`, `internal/storage/testdata/genai/*.jsonl` (new), `internal/storage/query.go` (`Attributes` retype and unmarshal error checks), `cmd/lotel/main.go` (`openDB`, `ingestCmd`) | WP3 | Sequential. **Ship WP3 and WP4 in one PR.** v2 tables without the v2 ingest leave the new columns empty. |
| WP6 | Prune on v2 | [02 §WP6](02-storage-schema-and-ingest.md#wp6--prune-with-schema-v2) | `internal/storage/prune.go` (doc comment), `storage_test.go` `TestPrune` | WP3 | Fold into the WP3 or WP4 PR |
| WP5 | Query surface and `query genai` | [03](03-query-surface.md) | `internal/storage/query.go`, `internal/storage/genai.go` (new), `internal/storage/genai_test.go` (new), `cmd/lotel/main.go` | WP4 | Sequential |
| WP7 | Deterministic GenAI E2E | [04 §WP7](04-verification-and-opencode-test.md#wp7--extend-scriptsverifypy-with-a-deterministic-genai-section) | `scripts/verify.py` | WP1, WP5 | Sequential |
| WP8 | Optional OpenCode Go live test | [04 §WP8](04-verification-and-opencode-test.md#wp8--optional-live-test-scriptsgenai_integrationpy-new) | `scripts/genai_integration.py` (new), `scripts/genai_integration.py.lock` (new), `.gitignore` (`.venv-it/`) | WP7 | Sequential |
| WP9 | Documentation | [05](05-documentation.md) | `README.md`, `AGENTS.md` | WP8 | Last |

Coupling note: WP3, WP4 and WP5 all touch `internal/storage/*_test.go` and `cmd/lotel/main.go`. Do not run them in parallel.

## Per-package verification

Every package must pass these commands, run as separate, individually checked steps:

```
go build ./...
go vet ./...
go test ./...
```

| Package | Additional verification |
|---|---|
| WP1 | **G0**, then **G1**: run the component inventory, the normalizer observation, the spanmetrics idle and the restart checks in [01 §Tests](01-collector-pipeline.md#tests-and-acceptance). Run `LOTEL_DOCKER_TESTS=1 go test ./internal/config/ -run TestExampleConfigsValidate -v`. Then run `go build -o lotel ./cmd/lotel`, then `./lotel stop`, then `./lotel start --wait`, then `./lotel health`. Finally run `docker inspect -f '{{.Config.Image}}' lotel-collector`, which must print the `0.165.0` tag. |
| WP2 | `go test ./internal/storage/ -run TestAnyValue -v` |
| WP3 | `go test ./internal/storage/ -run TestMigrate -v`. Then do a manual rebuild check in a scratch home: copy the WP1 snapshot (including its v1 `lotel.db`) to `<scratch>/home/.lotel/data`. Run `HOME=<scratch>/home ./lotel ingest` twice. The stderr rebuild notice appears on the first run only. |
| WP4 | **G2.** `go test ./internal/storage/ -run 'TestIngest\|TestMigrate' -v` passes, including the long-line test. Then run the performance gate: a full ingest of a copy of the real history finishes in under 60 s. |
| WP5 | `go test ./internal/storage/ -run 'TestQueryGenAI\|TestQueryFilters' -v`. Then the manual CLI checks in [03 §Tests](03-query-surface.md#tests-and-acceptance), and the `genai usage` performance gate (under 5 s). |
| WP7 | **G3.** `go build -o lotel ./cmd/lotel`, then `LOTEL_BIN=./lotel python3 scripts/verify.py` exits 0. |
| WP8 | First, `env -u OPENCODE_GO_API_KEY uv run scripts/genai_integration.py` prints SKIP and exits 0. Then run `zsh -c 'source ~/.profile && LOTEL_BIN=./lotel uv run scripts/genai_integration.py'`, which must exit 0. Also run `python3 -m py_compile scripts/genai_integration.py`. |
| WP9 | Run the documentation acceptance checks in [05](05-documentation.md#acceptance). |

## Integration and regression gates

- The existing tests in `internal/collector` and `internal/config` stay green.
- The existing `verify.py` checks stay green. Its prune section still deletes the run's data.
- Use a fixture that v1 fully supports: sum, gauge and histogram metrics; logs with `timeUnixNano` set; ordinary spans. Ingest it once. Then `lotel query traces|metrics|logs` without the new flags returns the same row count and order as v1 did. Only the JSON fields are added or retyped, which the confirmed application context allows. Expect row counts to change on other data by design: dedupe, new metric types, and the log timestamp fallback.
- After `./lotel ingest` runs twice in a row with the collector stopped, the second report shows `records: 0` for every signal. With the collector running, service-scoped counts are unchanged, as in the WP7 check.
- `git status --ignored --porcelain` shows no ignored `.go`, `.py`, `.yaml` or `.jsonl` source or fixture files under `cmd/`, `internal/`, `scripts/` or `examples/`.
- Run `git status` after any test run. Remove untracked binaries and generated `*.db` files, and never commit them. `.gitignore` already covers `lotel`, `*.db` and `*.test`.

## Definition of done

1. All success criteria 1–10 in [00-overview.md](00-overview.md#success-criteria) are demonstrated by the commands above.
2. Gates G0, G1, G2 and G3 pass. Then one of two conditions holds:
   - The WP8 live run passed at least once, and its output summary is recorded in the PR description with no secrets.
   - WP8 is explicitly marked "live run unverified" for the user to run, with the SKIP path and `py_compile` passing.
3. `go test ./...`, `go build ./...` and `go vet ./...` are clean on the final branch.
4. `README.md` and `AGENTS.md` match the shipped behavior.
5. The key-leak scan passed, and no file in the diff contains credential values.
6. The repository review workflow (dual review → fix → re-verify) ran on the final branch before merge.

## Deferred work and follow-ups

- Correct aggregation for cumulative histograms in `lotel query aggregate`.
- Mapping between semconv-main metric names and v1.41 names, once `semantic-conventions-genai` cuts a release.
- Exemplars, and `droppedAttributesCount`.
- Cost estimation per model.
- An Anthropic-API variant of the live test, using OpenCode Go's `/anthropic/v1/messages` with an Anthropic instrumentation, once an OTel-official Anthropic GenAI instrumentation exists.
- The GenAI contract (schema v2 columns and the `query genai` JSON) must be carried into the Rust rewrite prompt in `docs/prompts/rewrite-lotel-go-to-rust-otel-collector.md`.
- Issue tracking: the `AGENTS.md` `bd` instructions currently fail ("no beads database found"). The user decides whether to initialize `bd` or move to `bn`.
