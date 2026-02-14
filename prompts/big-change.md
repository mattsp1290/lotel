# Big Change Planning with Beads

## Agent Instructions

You are an expert software architect planning a significant change to an existing codebase. This task graph will be executed by AI agents working in parallel, coordinated through MCP Agent Mail with file reservations to prevent conflicts.

<quality_expectations>
Create a thorough, production-ready task graph that respects the existing codebase. Understand current patterns before proposing changes. Include analysis, implementation, migration, testing, and documentation tasks. Each task should be specific enough for an agent to execute independently without ambiguity.
</quality_expectations>

## Change Information

### Change Type
{Select one: NEW_FEATURE | REFACTOR | MIGRATION | PERFORMANCE | SECURITY | OTHER}

### Description
{PLEASE FILL THIS OUT - What is the goal of this change?}

### Links to Relevant Documentation
{External docs, RFCs, design docs, etc.}

### Affected Areas
{Which parts of the codebase will be touched? List directories, modules, or features}

### Success Criteria
{How do we know this change is complete and working?}

### Constraints
{Backwards compatibility requirements, deployment considerations, feature flags needed, etc.}

---

## Your Task

Before creating tasks, you must first understand the existing codebase. Then create a comprehensive **Beads task graph** using the `bd` CLI.

---

## Phase 0: Codebase Analysis (Do This First)

Before generating any tasks, thoroughly analyze:

### 1. Current Architecture
```bash
# Find relevant files
find . -name "*.ts" -o -name "*.tsx" | head -50
ls -la src/

# Understand module structure
cat package.json
cat tsconfig.json
```

### 2. Existing Patterns
Look for and document:
- How similar features are currently implemented
- Naming conventions used
- State management patterns
- API/service patterns
- Testing patterns and coverage
- Error handling conventions

### 3. Integration Points
Identify:
- What existing code will this change interact with?
- What APIs/interfaces must be maintained?
- What tests already exist that might break?
- What documentation references the affected code?

### 4. Risk Assessment
Consider:
- Which changes are highest risk?
- What could break silently?
- Are there performance implications?
- Security considerations?

Ignore:
- backwards compatibility

Simplicity and readability wins

---

## Output Format

Generate a shell script that creates the full task graph. The script should:

1. **Initialize Beads** (if not already initialized)
2. **Create analysis beads** (to understand current state)
3. **Create implementation beads** with appropriate priorities
4. **Create migration/rollout beads** if needed
5. **Establish dependencies** between beads
6. **Add labels** for phase grouping

### Example Output

```bash
#!/bin/bash
# Project: {PROJECT_NAME}
# Change: {CHANGE_DESCRIPTION}
# Generated: {DATE}

set -e

# Initialize beads if needed
if [ ! -d ".beads" ]; then
    bd init
fi

echo "Creating change beads..."

# ========================================
# Phase 1: Analysis & Planning
# ========================================

ANALYZE_CURRENT=$(bd create "Analyze current implementation of [affected area] and document patterns" -p 0 --label analysis --silent)

IDENTIFY_DEPS=$(bd create "Map all dependencies and consumers of [affected code]" -p 0 --label analysis --silent)

DESIGN_APPROACH=$(bd create "Document proposed changes and get approval before implementation" -p 0 --label analysis --silent)
bd dep add $DESIGN_APPROACH $ANALYZE_CURRENT
bd dep add $DESIGN_APPROACH $IDENTIFY_DEPS

# ========================================
# Phase 2: Preparation
# ========================================

ADD_TESTS=$(bd create "Add characterization tests for existing behavior before changes" -p 0 --label prep --silent)
bd dep add $ADD_TESTS $DESIGN_APPROACH

FEATURE_FLAG=$(bd create "Implement feature flag for gradual rollout" -p 1 --label prep --silent)
bd dep add $FEATURE_FLAG $DESIGN_APPROACH

# ========================================
# Phase 3: Core Implementation
# ========================================

IMPL_CORE=$(bd create "Implement [core change description]" -p 0 --label impl --silent)
bd dep add $IMPL_CORE $ADD_TESTS
bd dep add $IMPL_CORE $FEATURE_FLAG

IMPL_API=$(bd create "Update API layer for [change]" -p 1 --label impl --silent)
bd dep add $IMPL_API $IMPL_CORE

IMPL_UI=$(bd create "Update UI components for [change]" -p 1 --label impl --silent)
bd dep add $IMPL_UI $IMPL_API

# ========================================
# Phase 4: Testing & Validation
# ========================================

UNIT_TESTS=$(bd create "Add unit tests for new [feature/changes]" -p 0 --label testing --silent)
bd dep add $UNIT_TESTS $IMPL_CORE

INTEGRATION_TESTS=$(bd create "Add integration tests covering [scenarios]" -p 1 --label testing --silent)
bd dep add $INTEGRATION_TESTS $IMPL_API
bd dep add $INTEGRATION_TESTS $IMPL_UI

REGRESSION_CHECK=$(bd create "Verify no regressions in existing functionality" -p 0 --label testing --silent)
bd dep add $REGRESSION_CHECK $INTEGRATION_TESTS

# ========================================
# Phase 5: Migration & Cleanup
# ========================================

MIGRATE_DATA=$(bd create "Create data migration scripts if needed" -p 1 --label migration --silent)
bd dep add $MIGRATE_DATA $REGRESSION_CHECK

UPDATE_DOCS=$(bd create "Update documentation for [changed areas]" -p 2 --label docs --silent)
bd dep add $UPDATE_DOCS $REGRESSION_CHECK

CLEANUP=$(bd create "Remove deprecated code and feature flags after rollout" -p 3 --label cleanup --silent)
bd dep add $CLEANUP $MIGRATE_DATA
bd dep add $CLEANUP $UPDATE_DOCS

echo ""
echo "Bead graph created! View with:"
echo "  bv                    # Interactive TUI"
echo "  bv --robot-triage     # AI-friendly recommendations"
echo "  bd ready              # List unblocked tasks"
```

---

## Bead Creation Guidelines

### Priority Levels
- `-p 0` = Critical (blocking other work, or high-risk changes needing early validation)
- `-p 1` = High (important implementation work)
- `-p 2` = Medium (standard work)
- `-p 3` = Low (cleanup, nice-to-haves)

### Labels (Phase Grouping)
Use `--label` to group beads by phase:
- `analysis` - Understanding current state
- `prep` - Preparation work (tests, flags, scaffolding)
- `impl` - Core implementation
- `testing` - Test coverage
- `migration` - Data/code migration
- `docs` - Documentation updates
- `cleanup` - Post-rollout cleanup

### Dependency Rules
1. Analysis tasks should complete before implementation begins
2. Characterization tests should exist before changing code
3. Feature flags should be ready before risky changes
4. Integration tests depend on all related implementation
5. Cleanup tasks depend on successful validation

### Task Granularity
- Each bead should be completable in **under 750 lines of code changed**
- Prefer smaller, focused changes over large sweeping updates
- Split by file area when possible to enable parallel work

---

## Change-Specific Considerations

### For New Features
- Start with analysis of similar existing features
- Consider feature flag for gradual rollout
- Plan for A/B testing if relevant
- Include documentation and changelog updates

### For Refactors
- Add characterization tests first (capture current behavior)
- Consider strangler fig pattern for large changes
- Plan incremental migration path
- Ensure no behavior changes unless intentional

### For Migrations
- Create rollback plan as an explicit task
- Plan data validation checkpoints
- Consider dual-write period if applicable
- Include monitoring/alerting tasks

### For Performance Changes
- Add benchmarks before and after
- Include load testing tasks
- Plan gradual rollout with monitoring
- Have rollback criteria defined

---

## File Reservation Planning

For changes to existing code, be extra careful about reservations:

```bash
# Example reservation notes (add as bead descriptions)
# CAUTION: These files have many consumers
# Auth refactor: src/auth/**, tests/auth/** (coordinate with API team)
# Shared utils: src/lib/utils.ts (high contention - keep changes minimal)
```

---

## Risk Mitigation Tasks

Always consider including:

1. **Characterization Tests**: Capture existing behavior before changes
2. **Feature Flags**: Enable gradual rollout and quick rollback
3. **Monitoring**: Add metrics/logging for changed behavior
4. **Rollback Plan**: Document how to revert if issues arise
5. **Communication**: Notify affected teams/consumers

---

## Verification Steps

After generating the script:

1. **Review analysis phase**: Are you understanding before changing?
2. **Check test coverage**: Tests before and after implementation?
3. **Verify dependencies**: Do risky changes have safety nets?
4. **Run it**: `chmod +x setup-beads.sh && ./setup-beads.sh`
5. **Verify graph**: `bv --robot-insights` to check for cycles
6. **Review parallel work**: Ensure no file conflicts between parallel tracks

---

## Completeness Checklist

Ensure your task graph includes:

- [ ] Analysis of current implementation
- [ ] Documentation of proposed approach
- [ ] Characterization tests for existing behavior
- [ ] Feature flag or gradual rollout mechanism (if applicable)
- [ ] Core implementation broken into small units
- [ ] Unit tests for new/changed code
- [ ] Integration tests for affected workflows
- [ ] Regression testing plan
- [ ] Documentation updates
- [ ] Migration scripts (if data changes)
- [ ] Rollback plan
- [ ] Cleanup tasks for post-rollout
- [ ] Clear dependency chains with no cycles
