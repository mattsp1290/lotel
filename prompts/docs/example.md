# Example: Todo App Task Breakdown Analysis

This document analyzes the example todo app task file to demonstrate how to structure projects effectively.

## Project Overview

**File**: `/proompts/tasks/example-todo-app.yaml`  
**Project Type**: Simple Todo Application  
**Tech Stack**: React, TypeScript, Vite, CSS Modules

## Key Patterns Demonstrated

### 1. Phase Organization

The todo app example shows ideal phase progression:
- **Phase 1**: Project Setup (foundation)
- **Phase 2**: Core Components (building blocks)
- **Phase 3**: State Management (functionality)
- **Phase 4**: Styling (presentation)
- **Phase 5**: Testing (quality assurance)
- **Phase 6**: Polish & Deploy (finalization)

This logical flow ensures each phase builds on the previous one.

### 2. Task Dependencies

Notable dependency chains in the example:

```
init-project
  ├── setup-structure
  ├── setup-css
  │   └── create-base-styles
  │       └── style-components
  └── setup-vitest
      └── write-unit-tests
          └── write-integration-tests
              └── build-production
                  └── deploy-app
```

### 3. Priority Distribution

The example demonstrates proper priority assignment:
- **Critical** (5 tasks): Core functionality that blocks other work
- **High** (6 tasks): Important features and setup
- **Medium** (5 tasks): Enhancements and standard features
- **Low** (2 tasks): Nice-to-have polish items

### 4. Task Granularity

Each task is:
- Completable in 1-3 days
- Has a single, clear deliverable
- Can be tested independently
- Builds on previous work logically

## Lessons for New Projects

### DO:
1. **Start with setup tasks**: Always begin with project initialization
2. **Build incrementally**: Each task should add visible value
3. **Include testing early**: Don't save all testing for the end
4. **Document as you go**: Include documentation tasks throughout

### DON'T:
1. **Create mega-tasks**: Break down large features into smaller tasks
2. **Ignore dependencies**: Always map out task relationships
3. **Skip documentation**: Every phase should have relevant docs
4. **Forget deployment**: Include deployment tasks from the start

## Adapting the Pattern

When creating your own task file:

1. **Replace the tech stack**: Swap React/TypeScript for your technologies
2. **Adjust phases**: Add phases for your specific needs (e.g., "API Integration", "Database Setup")
3. **Scale complexity**: Add more tasks for larger projects, but maintain the pattern
4. **Keep the structure**: Use the same YAML format and sections

## Example Modifications

### For a Backend API:
```yaml
phases:
  - name: "Phase 1: API Setup"
    tasks:
      - id: init-express
        name: "Initialize Express server"
        # ... similar pattern
```

### For a Full-Stack App:
```yaml
phases:
  - name: "Phase 2: Backend Development"
    # ... backend tasks
  - name: "Phase 3: Frontend Development"  
    # ... frontend tasks
  - name: "Phase 4: Integration"
    # ... connecting frontend to backend
```

## Quick Reference

Common task patterns from the example:

- **Setup pattern**: `init-* → setup-* → configure-*`
- **Feature pattern**: `create-types → create-component → implement-logic → wire-up`
- **Testing pattern**: `setup-testing → write-unit-tests → write-integration-tests`
- **Deployment pattern**: `build-production → deploy-app`

## Using This Example

1. Copy `/proompts/tasks/example-todo-app.yaml` to a new file
2. Rename it to match your project: `your-project-name.yaml`
3. Update the metadata section
4. Modify phases to match your project structure
5. Adjust tasks while keeping the same format
6. Ensure dependencies make sense for your flow

Remember: This example is intentionally simple to serve as a clear template. Real projects may have 50-200+ tasks, but should follow the same organizational principles.
