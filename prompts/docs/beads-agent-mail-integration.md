# Beads + MCP Agent Mail Integration Design

## Executive Summary

This document outlines the architectural shift from YAML-based task management to a **Beads + MCP Agent Mail** system. This provides:

- **Beads**: Dependency-aware task graph with intelligent prioritization (PageRank, critical path analysis)
- **MCP Agent Mail**: Multi-agent coordination with file reservations, threaded messaging, and audit trails

```
┌─────────────────────────────────────────────────────────────────┐
│                     Human Supervisor                             │
│              (Web UI @ localhost:8765 / CLI)                     │
└─────────────────────────────────────────────────────────────────┘
                               │
┌─────────────────────────────────────────────────────────────────┐
│                   MCP Agent Mail Server                          │
│  ┌────────────────┐ ┌─────────────────┐ ┌──────────────────┐   │
│  │   Mailboxes    │ │ File Reservations│ │ Agent Registry   │   │
│  │  (bd-threads)  │ │  (edit leases)   │ │ (identities)     │   │
│  └────────────────┘ └─────────────────┘ └──────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
                               │
┌─────────────────────────────────────────────────────────────────┐
│                     Beads Task Graph                             │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  .beads/beads.jsonl - JSONL task storage                │   │
│  │  bd CLI - create, update, ready, dep                    │   │
│  │  bv CLI - robot-triage, robot-plan, robot-insights      │   │
│  └─────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
                               │
┌─────────────────────────────────────────────────────────────────┐
│                        AI Agents                                 │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐       │
│  │  Claude  │  │  Cursor/ │  │  Review  │  │ Research │       │
│  │   Code   │  │  Cline   │  │  Agent   │  │  Agent   │       │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘       │
└─────────────────────────────────────────────────────────────────┘
```

---

## What Changes

### Before (tasks.yaml)

```yaml
phases:
  - name: "Setup"
    tasks:
      - id: setup-auth
        name: "Implement auth"
        status: pending
        dependencies: [setup-db]
```

### After (Beads + Agent Mail)

```bash
# Tasks live in .beads/beads.jsonl
bd create "Implement auth" --deps bd-a1b2 -p 0

# Agent coordination via mail threads
# Thread ID = bead ID (bd-c3d4)
```

---

## Key Benefits Over tasks.yaml

| Feature | tasks.yaml | Beads + Agent Mail |
|---------|------------|-------------------|
| **Dependency Tracking** | Manual, error-prone | Automatic graph with cycle detection |
| **Task Prioritization** | Static priority field | PageRank, critical path, parallel track planning |
| **Ready Work Detection** | Manual scanning | `bd ready` / `bv --robot-triage` |
| **Multi-Agent Support** | None | File reservations, threaded messaging |
| **Merge Conflicts** | High risk | Hash-based IDs, JSONL append-only |
| **Progress Visibility** | Read YAML file | Web UI, robot flags, audit trail |
| **Context Preservation** | Token-heavy full file | Semantic summarization of completed tasks |

---

## New Directory Structure

```
project/
├── .beads/
│   ├── beads.jsonl          # Task graph (git-tracked)
│   └── .cache/              # SQLite cache (gitignored)
├── .agent-mail/             # Agent Mail artifacts (git-tracked)
│   ├── agents/              # Registered agent identities
│   ├── messages/            # Threaded conversations
│   ├── reservations/        # File lease records
│   └── attachments/         # Shared files
├── proompts/
│   ├── initial-prompt.md    # NEW: Creates beads, not YAML
│   ├── start-task.md        # NEW: Replaces get-next-task.md
│   ├── request-review.md    # NEW: Posts to agent mail
│   ├── act-on-review.md     # NEW: Reads from inbox
│   └── docs/
└── CLAUDE.md
```

---

## New Prompts

### 1. initial-prompt.md (Project Planning)

**Purpose**: Generate initial bead graph instead of tasks.yaml

**Key Changes**:
- Uses `bd create` commands instead of YAML structure
- Establishes dependencies with `bd dep add`
- Sets up project in Agent Mail with `ensure_project`
- Registers the planning agent's identity

**Output**: Series of `bd` commands that create the full task graph

### 2. start-task.md (Task Execution)

**Purpose**: Pick up ready work and begin execution

**Workflow**:
```bash
# 1. Get intelligent triage
bv --robot-triage

# 2. Select task from recommendations
bd update bd-xxxx --status in_progress

# 3. Reserve files (via Agent Mail MCP)
file_reservation_paths(project, agent, ["src/auth/**"], ttl=3600, exclusive=true)

# 4. Post start message to thread
send_message(project, agent, "bd-xxxx", "Starting auth implementation", "Beginning work on JWT auth system")
```

### 3. request-review.md (PR Review Request)

**Purpose**: Request async PR review via Agent Mail

**Workflow**:
```bash
# 1. Create review thread linked to bead
send_message(project, agent, "bd-xxxx-review", "Review Request", "PR #123 ready for review\n\n[diff summary]")

# 2. Human or review agent picks up from inbox
# 3. Response posted to same thread
```

### 4. act-on-review.md (Process Review Feedback)

**Purpose**: Read and act on review feedback from inbox

**Workflow**:
```bash
# 1. Check inbox for review responses
resource://inbox/{agent}

# 2. Parse feedback from thread
# 3. Reserve affected files
# 4. Make changes
# 5. Post completion to thread
```

---

## Quick Commands

### `/start` - Start a New Task

```bash
#!/bin/bash
# Get triage, let agent pick, reserve files, post start message

echo "=== Bead Triage ==="
bv --robot-triage | jq '.recommendations[:5]'

echo ""
echo "Select a bead ID to start working on, then I will:"
echo "1. Mark it in-progress"
echo "2. Reserve relevant files"
echo "3. Post start message to thread"
```

### `/review` - Request PR Review

```bash
#!/bin/bash
# Collect PR info, create review thread

BRANCH=$(git branch --show-current)
DIFF=$(git diff main..HEAD --stat)

echo "Creating review request for branch: $BRANCH"
echo "Thread will be created in Agent Mail"
echo "Review agent or human will respond to thread"
```

### `/act-review` - Act on Review Feedback

```bash
#!/bin/bash
# Check inbox, display pending reviews

echo "=== Pending Reviews ==="
# Query agent mail inbox for review threads
echo "Select a review to process"
```

---

## Onboarding Script Changes

### New: `get-the-vibes-going.sh`

```bash
#!/bin/bash
# Enhanced onboarding with Beads + Agent Mail

TARGET_DIR="$1"

# 1. Copy proompts (existing behavior)
cp -r "$SOURCE_DIR/proompts" "$TARGET_DIR/"

# 2. Initialize Beads
if [ ! -d "$TARGET_DIR/.beads" ]; then
    echo "Initializing Beads..."
    cd "$TARGET_DIR" && bd init
fi

# 3. Configure Agent Mail project
echo "Registering project with Agent Mail..."
# Uses MCP to call ensure_project

# 4. Set up pre-commit hook for file reservation checks
echo "Installing pre-commit hook..."
cat > "$TARGET_DIR/.git/hooks/pre-commit" << 'EOF'
#!/bin/bash
# Check for conflicting file reservations
# agent-mail reservation-check
EOF
chmod +x "$TARGET_DIR/.git/hooks/pre-commit"

# 5. Add to .gitignore
echo ".beads/.cache/" >> "$TARGET_DIR/.gitignore"

echo "Setup complete! Run: bv --robot-triage"
```

---

## Agent Identity Convention

Agents register memorable identities (adjective+noun):

| Agent Type | Example Identity | Purpose |
|------------|-----------------|---------|
| Planning | `BlueArchitect` | Initial task breakdown |
| Implementation | `GreenCastle` | Feature development |
| Review | `RedOwl` | Code review |
| Research | `SilverFox` | Documentation/research |

```bash
# Registration happens at session start
register_agent(project_key, "GreenCastle", {"role": "implementation"})
```

---

## Thread Convention

Thread IDs follow the pattern `bd-{bead_id}[-suffix]`:

| Pattern | Purpose |
|---------|---------|
| `bd-a1b2` | Main work thread for bead |
| `bd-a1b2-review` | Review discussion |
| `bd-a1b2-blocked` | Blocker escalation |
| `bd-a1b2-handoff` | Cross-agent handoff |

---

## File Reservation Patterns

```bash
# Exclusive reservation (no one else touches these)
file_reservation_paths(project, agent, ["src/auth/**", "tests/auth/**"], ttl=3600, exclusive=true)

# Shared reservation (coordination awareness)
file_reservation_paths(project, agent, ["docs/**"], ttl=1800, exclusive=false)

# Check before commit
file_reservation_conflicts(project, ["src/auth/jwt.ts"])
```

---

## Migration Path

### Phase 1: Parallel Operation
1. Keep existing tasks.yaml
2. Add Beads alongside: `bd import-yaml tasks.yaml` (if supported, else manual)
3. Start using `bd ready` for task selection
4. Gradually adopt Agent Mail for coordination

### Phase 2: Full Adoption
1. Archive tasks.yaml
2. Use Beads exclusively
3. All agents register identities
4. File reservations enforced via pre-commit

### Phase 3: Multi-Agent
1. Multiple agents work in parallel
2. Full thread-based coordination
3. Human oversight via Web UI
4. Audit trail for compliance

---

## Example Workflow: Implement Feature

```bash
# 1. Agent starts session
$ bv --robot-triage
{
  "recommendations": [
    {"id": "bd-a1b2", "title": "Implement JWT auth", "score": 0.95, "reason": "Critical path, all deps ready"}
  ]
}

# 2. Claim work
$ bd update bd-a1b2 --status in_progress

# 3. Reserve files (via MCP)
> file_reservation_paths("myproject", "GreenCastle", ["src/auth/**"], 3600, true)

# 4. Post start message (via MCP)
> send_message("myproject", "GreenCastle", "bd-a1b2", "Starting work", "Beginning JWT implementation")

# 5. Do the work...

# 6. Request review
> send_message("myproject", "GreenCastle", "bd-a1b2-review", "Review needed", "PR #45 ready")

# 7. Complete bead
$ bd update bd-a1b2 --status closed

# 8. Release reservation (auto on close, or manual)
> file_reservation_release("myproject", "GreenCastle", ["src/auth/**"])
```

---

## CLI Quick Reference

### Beads (`bd`)

| Command | Purpose |
|---------|---------|
| `bd init` | Initialize .beads in project |
| `bd create "Title" -p 0` | Create high-priority bead |
| `bd dep add child parent` | Add dependency |
| `bd ready` | List unblocked beads |
| `bd update ID --status STATUS` | Change status |
| `bd show ID` | View bead details |

### Beads Viewer (`bv`)

| Command | Purpose |
|---------|---------|
| `bv --robot-triage` | Get intelligent recommendations |
| `bv --robot-plan` | Parallel execution tracks |
| `bv --robot-insights` | PageRank, critical path |
| `bv --robot-next` | Single top pick |
| `bv --robot-priority` | Priority misalignment detection |

### Agent Mail (MCP Tools)

| Tool | Purpose |
|------|---------|
| `ensure_project(key, name)` | Register project |
| `register_agent(proj, name, profile)` | Create identity |
| `send_message(proj, from, thread, subj, body)` | Post to thread |
| `file_reservation_paths(...)` | Reserve files |
| `file_reservation_release(...)` | Release reservation |
| `resource://inbox/{agent}` | Check inbox |

---

## Open Questions

1. **Bead Import**: Does `bd` support importing from YAML, or do we script the migration?
2. **MCP Integration**: How do Claude Code and Cursor connect to the Agent Mail MCP server?
3. **Identity Persistence**: Should agent identities be project-specific or global?
4. **Review Agent**: Should we create a dedicated review agent, or have implementation agents cross-review?

---

## Next Steps

1. [ ] Create new `initial-prompt.md` for Beads-based planning
2. [ ] Create `start-task.md` with bv/bd integration
3. [ ] Create `request-review.md` and `act-on-review.md`
4. [ ] Update `get-the-vibes-going.sh` for Beads + Agent Mail setup
5. [ ] Create quick command scripts (`/start`, `/review`, `/act-review`)
6. [ ] Write CLAUDE.md section for MCP Agent Mail tools
7. [ ] Test multi-agent workflow with file reservations
