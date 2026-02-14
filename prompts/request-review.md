# Request PR Review Protocol

## Purpose

Request an asynchronous code review through MCP Agent Mail. This creates a review thread that can be picked up by a dedicated review agent or human reviewer.

---

## Prerequisites

Before requesting review:
- [ ] Work is committed to a feature branch
- [ ] Tests pass locally
- [ ] Bead is complete or at a reviewable checkpoint
- [ ] Self-review completed (no obvious issues)

---

## Step 1: Gather Context

```bash
# Get branch info
BRANCH=$(git branch --show-current)
BASE_BRANCH="main"

# Get commit summary
git log $BASE_BRANCH..HEAD --oneline

# Get diff stats
git diff $BASE_BRANCH..HEAD --stat

# Get full diff (for review context)
git diff $BASE_BRANCH..HEAD
```

---

## Step 2: Create Review Thread

Post a review request to Agent Mail:

```
send_message(
    project_key="project-name",
    from_agent="YourAgentIdentity",
    thread_id="bd-XXXX-review",
    subject="Review Request: [Feature/Fix Description]",
    body="""
## Review Request

**Bead**: bd-XXXX
**Branch**: feature/auth-jwt
**PR**: #123 (if created)
**Author**: YourAgentIdentity

---

### Summary

Implements JWT authentication with refresh token rotation.

### Changes

| File | Change Type | Description |
|------|-------------|-------------|
| src/auth/jwt.ts | New | JWT validation utilities |
| src/auth/refresh.ts | New | Refresh token rotation |
| src/hooks/useAuth.ts | Modified | Added token refresh logic |
| tests/auth/*.test.ts | New | Unit tests (12 tests) |

### Key Decisions

1. **RS256 over HS256**: Chose asymmetric signing for better security in distributed systems
2. **7-day refresh tokens**: Balance between security and UX
3. **Token rotation on use**: Each refresh invalidates the old token

### Testing

```bash
npm test -- src/auth
# 12 tests passing
```

### Areas of Concern

- [ ] Is the token expiry handling correct for edge cases?
- [ ] Should we add rate limiting on the refresh endpoint?

### Diff Summary

```
 src/auth/jwt.ts       | 145 +++++++++++++++
 src/auth/refresh.ts   |  89 +++++++++
 src/hooks/useAuth.ts  |  34 +++-
 tests/auth/jwt.test.ts| 112 ++++++++++++
 4 files changed, 378 insertions(+), 2 deletions(-)
```

---

**Requested Reviewers**: @RedOwl (review agent) or any available reviewer

**Priority**: Normal
**Blocking**: bd-AAAA, bd-BBBB depend on this
"""
)
```

---

## Step 3: Attach Diff (Optional)

For complex changes, attach the full diff:

```
# Create attachment with diff
git diff main..HEAD > /tmp/pr-diff.patch

# Attach to thread (via MCP)
attach_file(
    project_key="project-name",
    thread_id="bd-XXXX-review",
    file_path="/tmp/pr-diff.patch",
    description="Full diff for review"
)
```

---

## Step 4: Wait for Review

The review will come back in the same thread. Check periodically:

```
# Check inbox for responses
resource://inbox/YourAgentIdentity

# Or query specific thread
get_thread_messages(project_key, "bd-XXXX-review")
```

---

## Review Request Template (Minimal)

For quick reviews:

```
send_message(
    project_key="project-name",
    from_agent="YourAgentIdentity",
    thread_id="bd-XXXX-review",
    subject="Quick Review: [Description]",
    body="""
**Bead**: bd-XXXX
**Branch**: feature/small-fix
**Changes**: 3 files, ~50 lines

Small fix for auth token parsing edge case.

```diff
- if (token.exp < Date.now()) {
+ if (token.exp * 1000 < Date.now()) {
```

Please verify the timestamp conversion is correct.
"""
)
```

---

## For Golang Projects

Reference the 100-go-mistakes patterns in your review request:

```markdown
### Go-Specific Review Notes

Please check against common Go mistakes:
- [ ] Error handling (mistakes #48-52)
- [ ] Concurrency patterns (mistakes #55-72)
- [ ] Memory/performance (mistakes #91-98)

Reference: $HOME/git/100-go-mistakes/
```

---

## What Happens Next

1. **Review Agent** (e.g., `RedOwl`) or human picks up from their inbox
2. They post feedback to the same `bd-XXXX-review` thread
3. You receive notification in your inbox
4. Use `/act-review` protocol to process feedback

---

## Quick Commands

```bash
# Create PR on GitHub (if not done)
gh pr create --title "feat(auth): implement JWT validation" --body "See bd-XXXX"

# Link PR to bead
bd update bd-XXXX --note "PR #123"
```
