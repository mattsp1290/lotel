# Task Format Guide

This guide provides a comprehensive reference for creating and structuring YAML task files in the Proompting system.

## YAML Structure Overview

Every task file must follow this structure:

```yaml
# Project Name - Brief Description
# Project: project-identifier
# Created: YYYY-MM-DD

metadata:
  project: string
  description: string
  tech_stack: [array of strings]

phases:
  - name: string
    tasks: [array of task objects]

dependencies:
  external: [array of external dependency objects]

notes: [array of strings]

updates: [array of strings]
```

## Detailed Schema

### Metadata Section

Contains project-level information.

```yaml
metadata:
  project: "project-identifier"  # Unique project ID (kebab-case)
  description: "Comprehensive project description"
  tech_stack:
    - "Technology 1"
    - "Framework 2"
    - "Tool 3"
```

### Phases Section

Logical groupings of related tasks.

```yaml
phases:
  - name: "Phase Name"
    tasks:
      - id: "unique-task-id"
        name: "Task name"
        description: "Detailed task description"
        priority: critical|high|medium|low
        status: pending|in-progress|completed
        dependencies: ["task-id-1", "task-id-2"]  # Optional
        references: ["path/to/doc", "https://url"]  # Optional
```

### Task Object Properties

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| id | string | Yes | Unique identifier (kebab-case) |
| name | string | Yes | Brief, actionable task name |
| description | string | Yes | Detailed explanation of what needs to be done |
| priority | enum | Yes | One of: critical, high, medium, low |
| status | enum | Yes | One of: pending, in-progress, completed |
| dependencies | array | No | List of task IDs that must complete first |
| references | array | No | Helpful documentation or file paths |

### Dependencies Section

External project dependencies.

```yaml
dependencies:
  external:
    - project: "other-project-name"
      location: "$GIT_DIRECTORY/other-project"  # Optional
      purpose: "Why this dependency exists"      # Optional
      tasks:
        - "Specific task needed from external project"
        - "Another required external task"
```

### Notes Section

Important considerations and guidelines.

```yaml
notes:
  - "Best practice or important consideration"
  - "Technical constraint or requirement"
  - "Architecture decision or rationale"
```

### Updates Section

Track changes and progress.

```yaml
updates:
  - "YYYY-MM-DD: Description of what changed"
  - "YYYY-MM-DD: Progress update or modification"
```

## Best Practices

### Task ID Naming
- Use kebab-case: `setup-database`, `create-auth-system`
- Be descriptive but concise
- Ensure uniqueness across the entire file

### Task Descriptions
- Start with an action verb
- Include specific deliverables
- Mention key technologies or approaches
- Add acceptance criteria when helpful

### Priority Guidelines
- **critical**: Blocks other work, project can't function without it
- **high**: Core functionality, should be done early
- **medium**: Important but not blocking
- **low**: Nice-to-have, can be deferred

### Dependency Management
- Only include direct dependencies
- Avoid circular dependencies
- Consider parallel execution opportunities
- Dependencies should reference task IDs within the same file

### Phase Organization
- Group related functionality
- Order phases logically (setup → core → features → polish)
- Each phase should have a clear goal
- Aim for 3-10 tasks per phase

## Example Task Entries

### Simple Task
```yaml
- id: setup-eslint
  name: "Configure ESLint"
  description: "Set up ESLint with TypeScript support and team coding standards"
  priority: high
  status: pending
  dependencies: [setup-typescript]
```

### Complex Task with References
```yaml
- id: implement-oauth
  name: "Implement OAuth 2.0 authentication"
  description: "Set up OAuth flow with Google and GitHub providers, including refresh token handling and secure storage"
  priority: critical
  status: pending
  dependencies: [setup-database, create-user-model]
  references: 
    - "$HOME/docs/oauth-guide.md"
    - "https://oauth.net/2/"
    - "src/auth/oauth-config.example.json"
```

### Task with External Dependencies
```yaml
- id: integrate-payment-api
  name: "Integrate payment processing API"
  description: "Connect to payment service API with webhook handling for transaction updates"
  priority: high
  status: pending
  dependencies: [setup-webhook-endpoint]
  # Note: Requires external payment-service to have webhook endpoints ready
```

## Common Patterns

### Setup Phase Pattern
```yaml
- name: "Phase 1: Project Setup"
  tasks:
    - id: init-project
      name: "Initialize project"
      # ...
    - id: setup-tooling
      name: "Configure development tools"
      dependencies: [init-project]
      # ...
    - id: setup-ci
      name: "Set up CI/CD pipeline"
      dependencies: [init-project]
      # ...
```

### Feature Development Pattern
```yaml
- name: "Phase 3: User Management"
  tasks:
    - id: create-user-model
      name: "Define user data model"
      # ...
    - id: create-user-api
      name: "Build user CRUD API"
      dependencies: [create-user-model]
      # ...
    - id: create-user-ui
      name: "Build user management UI"
      dependencies: [create-user-api]
      # ...
    - id: test-user-flow
      name: "Test complete user flow"
      dependencies: [create-user-ui]
      # ...
```

## Validation Checklist

Before finalizing a task file, ensure:

- [ ] All task IDs are unique
- [ ] Every task has all required fields
- [ ] Dependencies reference valid task IDs
- [ ] No circular dependencies exist
- [ ] Phases are ordered logically
- [ ] Priority levels make sense relative to each other
- [ ] Descriptions are clear and actionable
- [ ] External dependencies are documented
- [ ] Initial status is 'pending' for new tasks

## Tips for AI Agents

1. **Read the entire file first** to understand context and dependencies
2. **Check task status** before starting work
3. **Update status to 'in-progress'** when beginning a task
4. **Verify dependencies are 'completed'** before starting
5. **Add updates** when completing significant work
6. **Set status to 'completed'** only when fully done

---

For more information, see the [Agent Guidelines](agent-guidelines.md) or review the [Example Todo App](../proompts/tasks/example-todo-app.yaml).
