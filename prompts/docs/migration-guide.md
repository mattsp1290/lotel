# Migration Guide: tasks.yaml to Beads

This guide covers migrating existing `tasks.yaml` files to the Beads task graph format.

## Overview

Beads is a git-backed task tracker with dependency-aware graphs. It replaces static YAML task files with a dynamic system that tracks dependencies, status, and supports AI agent coordination.

## Prerequisites

### Install Beads CLI (bd)

```bash
# Via Go
go install github.com/steveyegge/beads/cmd/bd@latest

# Verify installation
bd --version
```

### Install Claude Code (for migration)

```bash
npm install -g @anthropic-ai/claude-code
```

## Migration Methods

### Method 1: Claude Code Migration (Recommended)

This method uses Claude Code to interpret ANY YAML structure and migrate it reliably.

```bash
# Basic migration
./scripts/migrate-tasks-to-beads.sh path/to/tasks.yaml

# With verification pass (recommended)
./scripts/migrate-tasks-to-beads.sh path/to/tasks.yaml --verify

# Multiple files
./scripts/migrate-tasks-to-beads.sh tasks1.yaml tasks2.yaml --verify
```

**How it works:**
1. The script sends your YAML to Claude Code with migration instructions
2. Claude interprets the structure (phases, tasks, dependencies, priorities)
3. Claude executes `bd create` commands to create each bead
4. Claude adds dependencies with `bd dep add`
5. If `--verify` is used, Claude does a second pass to ensure completeness

**Supported YAML structures:**
- `phases[].tasks[]` - Array of phases with task arrays
- Top-level `tasks[]` - Flat task array
- `phase1_xxx:`, `phase2_xxx:` keys - Phase objects as keys
- Any custom structure - Claude interprets semantics

### Method 2: Python Preview Tool (Optional)

For quick preview of common YAML structures:

```bash
# Preview what would be migrated (dry run)
python scripts/migrate-tasks-to-beads.py tasks.yaml

# Generate shell script for manual review
python scripts/migrate-tasks-to-beads.py tasks.yaml -o migrate.sh

# Output as JSON
python scripts/migrate-tasks-to-beads.py tasks.yaml --json
```

**Note:** This only works for common structures. Use Method 1 for reliable migration.

## Migration via Onboarding Script

The `get-the-vibes-going.sh` script includes migration support:

```bash
# Set up project with migration
./get-the-vibes-going.sh /path/to/project --migrate
```

This will:
1. Copy proompts to the target project
2. Initialize Beads
3. Find and migrate any `tasks.yaml` using Claude Code

## Post-Migration

### Verify Migration

```bash
# List all beads
bd list

# Check task count
bd list --json | jq length

# View ready tasks (unblocked)
bd ready

# Visualize dependency graph
bd graph --all
```

### Install Beads Viewer (Optional)

For AI-optimized task recommendations:

```bash
go install github.com/Dicklesworthstone/beads_viewer@latest

# Get AI recommendations
bv --robot-triage

# See parallel execution tracks
bv --robot-plan

# Get insights (PageRank, critical path)
bv --robot-insights
```

## YAML to Beads Mapping

| YAML Field | Beads Equivalent |
|------------|------------------|
| `name` / `description` | Bead title |
| `priority: critical` | `-p 0` |
| `priority: high` | `-p 1` |
| `priority: medium` | `-p 2` |
| `priority: low` | `-p 3` |
| `status: pending` | `--status open` |
| `status: in-progress` | `--status in_progress` |
| `status: completed` | `--status closed` |
| `dependencies: [task-id]` | `bd dep add child parent` |
| Phase/category name | `--label phase-name` |

## Troubleshooting

### "bd: command not found"

Ensure Go bin is in your PATH:
```bash
export PATH="$HOME/go/bin:$PATH"
# Or wherever go env GOPATH points
```

### "Claude Code CLI not found"

Install Claude Code:
```bash
npm install -g @anthropic-ai/claude-code
```

### Migration finds 0 tasks

Your YAML structure may be unusual. The Claude-based migration handles this automatically. If using the Python tool, switch to `migrate-tasks-to-beads.sh`.

### Duplicate tasks after migration

If you run migration multiple times, duplicates may be created. Clear and re-migrate:
```bash
rm -rf .beads
bd init
./scripts/migrate-tasks-to-beads.sh tasks.yaml --verify
```

## Example

**Before (tasks.yaml):**
```yaml
phases:
  - name: "Phase 1: Setup"
    tasks:
      - id: setup-1
        name: "Initialize project structure"
        priority: high
        status: pending
      - id: setup-2
        name: "Configure CI/CD"
        priority: medium
        dependencies: [setup-1]
```

**After (Beads):**
```bash
$ bd list
○ proj-abc [● P1] [task] [setup] - Initialize project structure
○ proj-def [● P2] [task] [setup] - Configure CI/CD
    └── blocked by: proj-abc

$ bd ready
proj-abc: Initialize project structure [P1]
```

## Next Steps

After migration:
1. Use `bv --robot-triage` to get AI-recommended next tasks
2. Follow the workflow in `proompts/start-task.md`
3. Request reviews via `proompts/request-review.md`
4. Act on feedback via `proompts/act-on-review.md`
