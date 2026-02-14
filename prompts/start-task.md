# Start Task Protocol

## Mission

You are an AI agent ready to pick up the next optimal task from the Beads graph. This protocol ensures you:
1. Select the highest-impact ready work
2. Properly reserve files to prevent conflicts
3. Announce your work to the team via Agent Mail
4. Execute with full context and clarity

---

## Phase 1: Intelligent Triage

First, get the current state of the task graph:

```bash
# Get comprehensive triage (recommended)
bv --robot-triage

# Or for just the top pick
bv --robot-next

# Or see all ready work
bd ready --json
```

### Interpreting Triage Output

The triage output includes:
- **recommendations**: Ranked list of tasks with scores and reasoning
- **quick_wins**: Low-effort, high-impact tasks
- **blockers_to_clear**: Tasks that unblock many others
- **project_health**: Overall graph metrics

**Selection Criteria** (in order):
1. Blockers that unblock critical paths
2. High PageRank items (many things depend on them)
3. Items matching your expertise/current context
4. Quick wins if you're between major tasks

---

## Phase 2: Claim the Work

Once you've selected a bead:

```bash
# Mark it in-progress
bd update bd-XXXX --status in_progress

# View full details
bd show bd-XXXX
```

**Important**: Only claim ONE bead at a time. Finish or explicitly hand off before claiming another.

---

## Phase 3: Reserve Files

Before touching any files, declare your intent via MCP Agent Mail:

```
# Via MCP tool call
file_reservation_paths(
    project_key="project-name",
    agent_name="YourAgentIdentity",
    patterns=["src/auth/**", "tests/auth/**"],
    ttl_seconds=3600,
    exclusive=true
)
```

### Reservation Patterns

| Work Type | Suggested Patterns |
|-----------|-------------------|
| Component | `src/components/Name/**`, `tests/components/Name/**` |
| API route | `src/api/resource/**`, `tests/api/resource/**` |
| Hook | `src/hooks/useHook*`, `tests/hooks/useHook*` |
| Service | `src/services/name/**`, `tests/services/name/**` |
| Config | `src/config/name*`, specific config files |

### If Conflicts Exist

```bash
# Check for existing reservations
file_reservation_check(project_key, patterns)
```

If files are reserved:
1. Check the reservation holder's identity
2. Post to their work thread asking about timeline
3. Pick a different bead, or work on non-conflicting parts

---

## Phase 4: Announce Start

Post to the bead's thread so the team knows you're working:

```
send_message(
    project_key="project-name",
    from_agent="YourAgentIdentity",
    thread_id="bd-XXXX",
    subject="Starting: [Bead Title]",
    body="""
## Work Starting

**Bead**: bd-XXXX
**Agent**: YourAgentIdentity
**Files Reserved**: src/auth/**, tests/auth/**

### Plan
1. Implement JWT token validation
2. Add refresh token rotation
3. Write unit tests
4. Update API documentation

### Expected Duration
~2 hours

### Dependencies Verified
- [x] bd-YYYY (API client) is complete
- [x] bd-ZZZZ (auth context) is complete
"""
)
```

---

## Phase 5: Execute

Now do the work. During execution:

### Progress Updates (for long tasks)
Post updates to the thread every significant milestone:

```
send_message(
    project_key="project-name",
    from_agent="YourAgentIdentity",
    thread_id="bd-XXXX",
    subject="Progress: JWT validation complete",
    body="Token validation implemented and tested. Moving to refresh token rotation."
)
```

### If Blocked
Create a blocker thread and potentially create a new bead:

```bash
# Create blocking issue
bd create "Unexpected API incompatibility in auth endpoint" -p 0 --label blocked
bd dep add bd-XXXX bd-NEWBLOCKER

# Notify in thread
send_message(..., thread_id="bd-XXXX-blocked", subject="BLOCKED: API incompatibility", ...)
```

### If Scope Changes
If you discover the task is larger than expected:

```bash
# Split into sub-beads
bd create "Part 1: Basic JWT validation" -p 0
bd create "Part 2: Refresh token rotation" -p 1
bd dep add bd-PART2 bd-PART1

# Update original to reference new structure
bd update bd-XXXX --status closed --note "Split into bd-PART1 and bd-PART2"
```

---

## Phase 6: Complete

When finished:

```bash
# 1. Run tests to verify
npm test -- --related src/auth

# 2. Mark complete
bd update bd-XXXX --status closed

# 3. Release reservations (usually automatic, but can be explicit)
file_reservation_release(project_key, agent_name, patterns)

# 4. Post completion
send_message(
    ...,
    thread_id="bd-XXXX",
    subject="COMPLETE: [Bead Title]",
    body="""
## Work Complete

**Bead**: bd-XXXX
**Agent**: YourAgentIdentity

### Summary
- Implemented JWT validation with RS256
- Added refresh token rotation (7-day expiry)
- 12 unit tests added (all passing)

### Files Changed
- src/auth/jwt.ts (new)
- src/auth/refresh.ts (new)
- src/hooks/useAuth.ts (modified)
- tests/auth/*.test.ts (new)

### Follow-up Needed
- bd-AAAA is now unblocked
- Consider adding rate limiting (created bd-BBBB)

### Commit
abc1234 - feat(auth): implement JWT validation and refresh tokens
"""
)
```

---

## Quick Reference

```bash
# Triage
bv --robot-triage              # Full recommendations
bv --robot-next                # Just top pick
bd ready                       # All unblocked

# Claim
bd update ID --status in_progress
bd show ID

# Complete
bd update ID --status closed

# Graph analysis
bv --robot-insights            # PageRank, critical path
bv --robot-plan                # Parallel execution tracks
```

---

## Checklist Before Starting

- [ ] Ran `bv --robot-triage` to get recommendations
- [ ] Selected bead based on impact/readiness
- [ ] Marked bead as `in_progress`
- [ ] Reserved relevant file patterns
- [ ] Posted start message to thread
- [ ] Verified all dependencies are actually complete
- [ ] Have clear understanding of acceptance criteria

---

Now go execute with excellence.
