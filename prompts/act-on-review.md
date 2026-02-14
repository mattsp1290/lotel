# Act on Review Feedback Protocol

## Purpose

Process code review feedback received through MCP Agent Mail and systematically address each item.

---

## Step 1: Check Inbox for Reviews

```
# Get all messages in inbox
resource://inbox/YourAgentIdentity

# Or search for review threads
search_messages(
    project_key="project-name",
    query="Review",
    thread_pattern="*-review"
)
```

---

## Step 2: Read Review Feedback

Retrieve the full thread:

```
get_thread_messages(
    project_key="project-name",
    thread_id="bd-XXXX-review"
)
```

---

## Step 3: Parse Feedback into Action Items

Review feedback typically contains:

### Categories of Feedback

| Category | Action | Priority |
|----------|--------|----------|
| **Blocking** | Must fix before merge | Critical |
| **Suggestion** | Should consider, discuss if disagree | High |
| **Nitpick** | Optional style/preference | Low |
| **Question** | Respond with clarification | Medium |
| **Praise** | No action needed | - |

### Example Review Response

```markdown
## Review: bd-XXXX-review

**Reviewer**: RedOwl
**Verdict**: Changes Requested

### Blocking Issues

1. **Security**: Token not validated before use in `jwt.ts:45`
   - The `validateToken()` call happens after we extract claims
   - This could allow forged tokens to pass initial checks

2. **Bug**: Refresh token expiry check uses wrong units
   ```typescript
   // Current (wrong)
   if (token.exp < Date.now())
   // Should be
   if (token.exp * 1000 < Date.now())
   ```

### Suggestions

3. Consider adding rate limiting on refresh endpoint
4. The error messages could be more specific for debugging

### Questions

5. Why RS256 over ES256? Just curious about the choice.

### Nitpicks

6. Typo in comment line 78: "authenication"
```

---

## Step 4: Reserve Files Again

If your reservations expired or were released:

```
file_reservation_paths(
    project_key="project-name",
    agent_name="YourAgentIdentity",
    patterns=["src/auth/**"],
    ttl_seconds=3600,
    exclusive=true
)
```

---

## Step 5: Address Each Item

Work through feedback systematically:

### For Blocking Issues

```bash
# Fix the issue
# Run tests
npm test -- src/auth

# Commit the fix
git commit -m "fix(auth): validate token before extracting claims

Addresses review feedback from RedOwl:
- Move validateToken() before claims extraction
- Fix timestamp unit conversion (seconds -> ms)

Bead: bd-XXXX"
```

### For Suggestions

Either implement or respond with reasoning:

```
send_message(
    project_key="project-name",
    from_agent="YourAgentIdentity",
    thread_id="bd-XXXX-review",
    subject="RE: Suggestion - Rate Limiting",
    body="""
Good point about rate limiting. I've created a follow-up bead:

**bd-YYYY**: Add rate limiting to auth endpoints

This keeps the current PR focused on core JWT functionality.
Agree/disagree?
"""
)
```

### For Questions

Respond in the thread:

```
send_message(
    ...,
    subject="RE: RS256 vs ES256",
    body="""
Chose RS256 because:
1. Better tooling support in our JWT library
2. Existing key infrastructure uses RSA
3. Performance difference is negligible for our scale

Happy to switch if there's a strong preference for ES256.
"""
)
```

### For Nitpicks

Fix if trivial, otherwise note for later:

```bash
# Quick fix
sed -i 's/authenication/authentication/' src/auth/jwt.ts
git commit -m "fix: typo in auth comment"
```

---

## Step 6: Post Resolution Summary

After addressing all items:

```
send_message(
    project_key="project-name",
    from_agent="YourAgentIdentity",
    thread_id="bd-XXXX-review",
    subject="Review Feedback Addressed",
    body="""
## Feedback Resolution

All items from @RedOwl's review have been addressed:

### Blocking Issues
- [x] #1 Token validation order - Fixed in commit abc123
- [x] #2 Timestamp units - Fixed in commit abc123

### Suggestions
- [x] #3 Rate limiting - Created bd-YYYY for follow-up
- [x] #4 Error messages - Improved in commit def456

### Questions
- [x] #5 RS256 choice - Responded with rationale

### Nitpicks
- [x] #6 Typo - Fixed in commit ghi789

---

**Ready for re-review** or merge approval.

**New commits since review**:
- abc123 - fix(auth): validate token before extracting claims
- def456 - fix(auth): improve error messages
- ghi789 - fix: typo in auth comment
"""
)
```

---

## Step 7: Request Re-Review (if needed)

If changes were significant:

```
send_message(
    ...,
    subject="Re-Review Request",
    body="Significant changes made. @RedOwl please take another look when available."
)
```

---

## Step 8: Merge and Complete

Once approved:

```bash
# Merge PR
gh pr merge --squash

# Update bead
bd update bd-XXXX --status closed --note "Merged PR #123"

# Release reservations
file_reservation_release(project_key, agent_name, patterns)

# Post completion
send_message(
    ...,
    thread_id="bd-XXXX",
    subject="COMPLETE: Merged",
    body="PR #123 merged to main. Bead closed."
)
```

---

## Feedback Triage Checklist

When processing review feedback:

- [ ] Read entire review before starting changes
- [ ] Identify blocking vs non-blocking items
- [ ] Re-reserve files if needed
- [ ] Address blocking issues first
- [ ] Run tests after each significant change
- [ ] Respond to questions in thread
- [ ] Create follow-up beads for out-of-scope suggestions
- [ ] Post resolution summary
- [ ] Request re-review if needed
- [ ] Merge and close bead when approved

---

## Quick Reference

```bash
# Check inbox
resource://inbox/YourAgentIdentity

# Get thread
get_thread_messages(project, "bd-XXXX-review")

# After fixes
git push
gh pr view

# Complete
bd update bd-XXXX --status closed
```
