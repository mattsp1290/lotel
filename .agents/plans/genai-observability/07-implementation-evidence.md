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

## Review and final acceptance

The standard review is in
`reviews/feat-genai-observability-2026-11-14-233556-0cc6f916c16f/`.
Signal Integrity and Operational Maintainability both requested changes.
The fixer resolved two Important findings reported by both reviewers and one
suggestion, with no skipped or manual items. Checkpoint `a4068fa` was pushed
and its remote SHA matched local HEAD.

The thermonuclear review is in
`reviews/feat-genai-observability-thermo-2026-11-14-234031-a4068fa284a1/`.
It used the complete upstream rubric pinned to
`bf0c7cd80abdbf7fe47b634978a7d1b4071f71a7`:
https://raw.githubusercontent.com/cursor/plugins/bf0c7cd80abdbf7fe47b634978a7d1b4071f71a7/cursor-team-kit/skills/thermo-nuclear-code-quality-review/SKILL.md

The fixer resolved two Important structural findings, with no skipped or manual
items. Checkpoint `0f79304` was pushed and its remote SHA matched local HEAD.
Review artifacts remain local under the repository's existing ignore convention.

Final source validation passed:
- `go test ./...`, `go build ./...`, `go vet ./...`, and `git diff --check`.
- Deterministic collector E2E: 23 passed, 0 failed.
- Live OpenCode test after both review fixes: 9 passed, 0 failed, 0 warnings.
- Python compilation and missing-key SKIP.
- Docker recipe validation and behavior tests.
- Raw JSONL sentinel survived stop/start and was reported as a skipped line.
- A different config path warned without changing the container ID.
- README relative links resolved. Repository credential scan found no key.
- Git pull reported already up to date.

The installed Beads CLI has no `sync` command, and no repository beads database
exists. No issue was available to close. The plan excludes tracker initialization
from implementation, so Git push proceeds independently.
