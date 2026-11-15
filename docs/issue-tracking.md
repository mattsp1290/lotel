# Issue tracking with Beans

Lotel uses the `bn` CLI from [Beans](https://github.com/mattsp1290/beans).
Issues live in the shared Git hub at `~/.beans/hub/projects/lotel`.
They are not files in the lotel repository.

## Setup

Install `bn` using the Beans README. On a machine without a hub, clone your
existing hub once:

```bash
bn init <hub-remote>
```

From the lotel checkout, verify project resolution and read the workflow:

```bash
bn status
bn prime
bn ready
```

The project is `lotel`, linked to `https://github.com/mattsp1290/lotel`.
If an existing checkout does not resolve correctly, run `bn project link lotel`.

## Work on an issue

```bash
bn create "Describe the work" -p 2 -l tooling --silent
bn show <id>
bn update <id> --claim
bn note <id> "Progress and verification evidence"
bn close <id> -r "Completed in <commit>"
bn sync
```

Mutating commands commit and push the hub by default. `bn sync` synchronizes the
hub independently of `git push` in the lotel checkout. Do not add hub files to
lotel commits. Run `bn doctor` to check hub health.

For dependencies, `bn dep add CHILD PARENT` makes the child wait for the parent.
Use `bn dep cycles` to check the graph.

## Migration on 2026-11-15

The migration imported all 31 closed issues and 51 dependency links from
`.beads/issues.jsonl` using `bn import bd`. Original issue IDs remain valid.
The import passed its dry run with no warnings, rejected records, or unresolved
links, and pushed hub commit `bc654ef98814db76e6f424c53cbda4ad2dba0df9`.

The importer does not copy comments. The migration preserved all six comments
as notes containing the original author, timestamp, and full text, then ran
`bn sync`. Titles, statuses, priorities, descriptions, dependency targets, and
comment text were checked against the legacy export.

Closed issues are archived in the hub. Query them with:

```bash
bn list --closed --limit 0
bn show lotel-36k
```

The legacy export SHA-256 is
`696867214bc6121778139329514f0f50980da168be12ad275b43e0961ff7261b`.

The repository's old `.beads` files and merge-driver rule were removed after
verification. The original export remains recoverable from lotel Git history.
Older implementation plans mention the previous tracker as historical evidence;
the current workflow is in [AGENTS.md](../AGENTS.md).
