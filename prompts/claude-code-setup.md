# Claude Code Setup Prompt

## Prerequisites

This prompt should be executed AFTER `proompts/initial-prompt.md` has been completed and Beads has been initialized (`.beads/` directory exists with `beads.jsonl`).

## Agent Instructions

You are setting up a Claude Code environment for optimal AI-assisted development using the Beads task system. Your goal is to configure the workspace for maximum productivity when working with bead-based development using Claude Code CLI.

**Research Capabilities**: Claude Code has built-in `WebSearch` and `WebFetch` tools for real-time web research. Use these tools when you need current information about technologies, best practices, or solutions that might not be in your training data.

## Setup Tasks

### 1. Verify Project Structure

First, verify the following files and directories exist:
- `/.beads/` directory with `beads.jsonl`
- `/proompts/` directory with prompt files
- Run `bd list` to confirm beads are created

### 2. Create Claude Code Configuration

Create a `CLAUDE.md` file in the project root with project-specific instructions:

#### CLAUDE.md
```markdown
# Project-Specific Instructions for Claude Code

## Beads Task Management

This project uses **Beads** for dependency-aware task management. Tasks are stored in `.beads/beads.jsonl`.

### Quick Reference
- `bd ready` - List tasks ready to work on (all dependencies completed)
- `bd list` - List all tasks
- `bd show <id>` - View task details
- `bd start <id>` - Start working on a task
- `bd finish <id>` - Mark task complete
- `bd block <id> <reason>` - Mark task as blocked
- `bd graph` - View dependency graph
- `bd count` - Show task counts by status

### AI Triage Commands
- `bv --robot-triage` - Get AI-friendly task recommendations with scores
- `bv --robot-next` - Get single top priority pick
- `bv --robot-plan` - Get parallel execution tracks
- `bv --robot-insights` - PageRank, critical path analysis
- `bv --robot-priority` - Priority misalignment detection

### Workflow
1. Run `bd ready` or `bv --robot-triage` to find available work
2. Start the task: `bd start bd-XXX`
3. Implement the task
4. Mark complete: `bd finish bd-XXX`
5. Commit with bead ID: `git commit -m "bd-XXX: Description"`

### Branch Naming
Create branches with bead ID: `feature/bd-123-description`

## Research and Information Gathering
- Use WebSearch for real-time research when needed
- Use WebFetch to retrieve specific documentation pages
- Research current best practices before implementing new technologies
- Verify compatibility and versions of tools/libraries before use

## File Organization
- Prompt files and docs go in /proompts/
- Follow existing project structure patterns

## Code Standards
- Follow existing code style in the project
- Write tests for new features
- Document complex logic with comments

## Claude Code Workflow
- Use the TodoWrite tool to track multi-step implementations
- Use the Task tool for complex exploration and research
- Prefer Edit over Write for modifying existing files
- Make atomic commits with bead ID in message
```

### 3. Create Quick Reference Commands

Create `.claude/commands.md` for common Claude Code workflows:

```markdown
# Quick Reference Commands for Claude Code

## Beads Task Management

"Run `bd ready` and show me the available tasks I can work on."

"Show me the triage recommendations with `bv --robot-triage`."

"Show me details for bead bd-XXX using `bd show bd-XXX`."

"Start bead bd-XXX for me and summarize what needs to be implemented."

"I've finished bead bd-XXX. Mark it complete with `bd finish bd-XXX`."

"Show me the dependency graph with `bd graph`."

"What's the overall progress? Run `bd count` to see task counts."

"Show me the task graph and critical path with `bv --robot-insights`."

"What's the parallel execution plan? Run `bv --robot-plan`."

## Research and Development

"Research the current best practices for [TECHNOLOGY/APPROACH] using web search before we implement this."

"Fetch the documentation page at [URL] and summarize the key points."

"Search for recent documentation or tutorials on [TOPIC] to ensure we're using the latest approach."

"Find current compatibility information between [TOOL_A] and [TOOL_B]."

"Look up the latest version and installation instructions for [PACKAGE/TOOL]."

"Research common issues and solutions for [SPECIFIC_PROBLEM] before troubleshooting."

## Problem Solving

"Before implementing [FEATURE], research if there are existing solutions or libraries we should consider."

"Search for current examples of [PATTERN/IMPLEMENTATION] to guide our approach."

## Claude Code Specific

"Use the Explore agent to find all files related to [FEATURE]."

"Create a todo list to track the implementation of [FEATURE]."

"Run the tests and fix any failures."

"Commit these changes with an appropriate message referencing bead bd-XXX."
```

### 4. Create Bead Status Script

Create `scripts/bead-status.sh`:

```bash
#!/bin/bash
# Quick bead status checker

echo "=== Bead Status Summary ==="
echo ""

# Check if beads is initialized
if [ ! -d ".beads" ]; then
    echo "Beads not initialized. Run 'bd init' first."
    exit 1
fi

echo "Task Counts:"
bd count 2>/dev/null || echo "Unable to get counts"

echo ""
echo "=== In-Progress Beads ==="
bd list --status in_progress 2>/dev/null || echo "None"

echo ""
echo "=== Ready Beads (unblocked) ==="
bd ready 2>/dev/null | head -10

echo ""
echo "=== AI Triage Recommendation ==="
echo "Run 'bv --robot-triage' for intelligent task recommendations"
```

Make it executable:
```bash
chmod +x scripts/bead-status.sh
```

### 5. Create Git Configuration

Create or update `.gitignore` to include beads cache:

```
# OS
.DS_Store
Thumbs.db

# Dependencies
node_modules/
venv/
__pycache__/

# Build outputs
dist/
build/
*.egg-info/

# Logs
*.log
logs/

# Environment
.env
.env.local

# Testing
coverage/
.coverage
.pytest_cache/

# Temporary files
*.tmp
*.temp
.cache/

# Beads cache (SQLite)
.beads/.cache/
```

### 6. Create Claude Code Tools Reference

Create `.claude/tools.md`:

```markdown
# Claude Code Tools Available in This Workspace

## Built-in Research Tools

### WebSearch
Real-time web search capability for research and information gathering.

#### When to Use
- Researching current best practices
- Finding up-to-date documentation
- Checking compatibility between tools
- Looking up recent tutorials or examples
- Verifying current package versions
- Troubleshooting with recent solutions

### WebFetch
Fetch and analyze content from specific URLs.

#### When to Use
- Reading specific documentation pages
- Analyzing API references
- Reviewing tutorials or guides

## Task and Exploration Tools

### Task (with Explore agent)
Use for codebase exploration and complex searches.

#### When to Use
- Finding files by patterns
- Searching for keywords across the codebase
- Understanding codebase structure

### TodoWrite
Track multi-step tasks and show progress.

#### When to Use
- Planning complex implementations
- Breaking down features into steps
- Tracking progress on multi-file changes

## Beads Integration

### bv (Beads Viewer) Robot Flags
```bash
bv --robot-triage     # Intelligent recommendations with scores
bv --robot-next       # Single top priority pick
bv --robot-plan       # Parallel execution tracks
bv --robot-insights   # PageRank, critical path analysis
bv --robot-priority   # Priority misalignment detection
```

### bd (Beads) Commands
```bash
bd init               # Initialize beads in project
bd create "Title"     # Create new bead
bd ready              # List unblocked beads
bd list               # List all beads
bd show <id>          # View bead details
bd start <id>         # Start working on a bead
bd finish <id>        # Mark bead as complete
bd block <id> <reason>  # Mark bead as blocked
bd graph              # View dependency graph
bd count              # Show task counts by status
bd dep add <child> <parent>  # Add dependency
```

## Workflow Integration
- Use `bv --robot-triage` to find high-value work
- Use TodoWrite to break down bead work into steps
- Research dependencies with WebSearch before implementing
- Verify approaches are still current using web tools
```

### 7. Create README for Claude Code Users

Create `.claude/README.md`:

```markdown
# Claude Code Setup for Beads-Based Development

This directory contains Claude Code-specific configuration for working with the Beads task system.

## Quick Start

1. Open the project with `claude` command in terminal
2. Run `bd ready` or `bv --robot-triage` to find available work
3. Reference CLAUDE.md for project conventions
4. Use commands.md for quick reference prompts

## Useful Commands

- Find ready work: `bd ready`
- AI triage: `bv --robot-triage`
- View task: `bd show <id>`
- Start work: `bd start <id>`
- Complete work: `bd finish <id>`
- Check progress: `bd count`
- View graph: `bd graph` or `bv` (interactive TUI)

## Key Files

- `/.beads/beads.jsonl` - Task graph (git-tracked)
- `/proompts/` - Project prompts and documentation
- `/CLAUDE.md` - Project instructions for Claude Code
- `/.claude/tools.md` - Available tools and usage

## Tips

1. Always use `bd start <id>` before working on a task
2. Use `bv --robot-triage` for intelligent recommendations
3. Include bead ID in branch names: `feature/bd-123-description`
4. Include bead ID in commits: `bd-123: Description`
5. **Use WebSearch for research** before implementing unfamiliar tech
6. Use the TodoWrite tool for multi-step implementations
7. Use the Explore agent for codebase understanding
```

## Verification Steps

After setup, verify:

1. [ ] CLAUDE.md exists in project root
2. [ ] .claude/ directory with configuration files
3. [ ] Bead status script works: `./scripts/bead-status.sh`
4. [ ] Beads is working: `bd list` shows tasks
5. [ ] Ready tasks exist: `bd ready` shows available work

## Next Steps

1. Open the project with `claude` in terminal
2. Run `bv --robot-triage` to find recommended work
3. Use `bd show <id>` to review task requirements
4. Begin working through beads with `bd start <id>`
5. Refer to `CLAUDE.md` for project conventions

## Tips for Optimal Workflow

1. **Use Beads**: Always `bd start` before working, `bd finish` when done
2. **Use TodoWrite**: Track complex implementations with the built-in todo list
3. **Research First**: Use WebSearch to research unfamiliar technologies
4. **Explore Agent**: Use the Task tool with Explore agent for codebase navigation
5. **Atomic Commits**: Make atomic commits with bead ID references
6. **Stay Current**: Regularly research best practices for technologies you're using
7. **Plan Mode**: Use EnterPlanMode for non-trivial implementations
8. **Triage**: Start sessions with `bv --robot-triage` for intelligent task selection

## Claude Code Capabilities

Claude Code provides powerful built-in capabilities:

- **WebSearch**: Real-time web search for documentation and solutions
- **WebFetch**: Fetch specific web pages for analysis
- **TodoWrite**: Track multi-step task progress
- **Task (Explore)**: Comprehensive codebase exploration
- **Git Integration**: Commit, create PRs, and manage version control
- **File Operations**: Read, Write, Edit with intelligent context

## Beads + Claude Code Integration

Beads provides dependency-aware task management that works well with Claude Code:

- **Parallel Execution**: Multiple agents can work on independent beads
- **Dependency Tracking**: `bd ready` only shows tasks with completed dependencies
- **File Reservations**: Beads can track which files each task modifies
- **MCP Agent Mail**: Threads are auto-created as `bd-{id}` for coordination

## Beads Workflow

```
1. bv --robot-triage          # Get recommendations
2. bd start bd-XXX            # Claim the task
3. [Do the work]
4. bd finish bd-XXX           # Mark complete
5. git commit -m "bd-XXX: Description"
```

---

Your Claude Code environment is now optimized for bead-based development with enhanced research capabilities.
