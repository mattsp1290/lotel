# Vibes Documentation

Welcome to the Vibes project documentation. This system helps AI agents manage and execute complex projects through structured YAML task files.

## Quick Start

1. **Initial Setup**: Run an AI agent with `proompts/initial-prompt.md` to generate your project's task file
2. **Task Execution**: Use `proompts/get-next-task.md` to execute tasks sequentially
3. **Reference Example**: Check `proompts/tasks/example-todo-app.yaml` for task structure

## System Overview

Vibes is a task management system designed specifically for AI agents to break down complex projects into manageable, sequential tasks with clear dependencies.

### Key Components

- **Task Files** (`proompts/tasks/*.yaml`): YAML files defining project structure and tasks
- **Prompts** (`proompts/*.md`): Markdown files containing instructions for AI agents
- **Documentation** (`docs/`): Comprehensive guides and references

### Directory Structure

```
vibes/
├── proompts/
│   ├── initial-prompt.md      # Generate new task files
│   ├── get-next-task.md       # Execute tasks
│   ├── cursor-cline-setup.md  # Setup Cursor/Cline environment
│   └── tasks/                 # Task YAML files
│       └── example-todo-app.yaml
├── docs/
│   ├── README.md              # This file
│   ├── task-format-guide.md   # YAML format documentation
│   ├── agent-guidelines.md    # Best practices for agents
│   └── prompt-templates.md    # Reusable prompt patterns
└── tasks.yaml                 # Active project task file
```

## Getting Started

### For AI Agents

1. Read the active `tasks.yaml` file to understand the current project
2. Use `proompts/get-next-task.md` to find and execute the next pending task
3. Update task status after completion
4. Follow guidelines in `docs/agent-guidelines.md`

### For Project Managers

1. Use `proompts/initial-prompt.md` to generate comprehensive task breakdowns
2. Review and refine the generated `tasks.yaml`
3. Monitor progress through task status updates

## Core Concepts

### Tasks
- Atomic units of work with clear deliverables
- Include dependencies, priority, and status
- Should be completable in 1-3 days

### Phases
- Logical groupings of related tasks
- Help organize project timeline
- Enable parallel work streams

### Dependencies
- Define task execution order
- Include both internal and external dependencies
- Prevent blocking and optimize flow

## Best Practices

1. **Clear Task Definitions**: Each task should have a specific, measurable outcome
2. **Proper Dependencies**: Map out task relationships to avoid blockers
3. **Regular Updates**: Keep task status current
4. **Documentation**: Document decisions and changes in the updates section

## Additional Resources

- [Task Format Guide](task-format-guide.md) - Detailed YAML schema reference
- [Agent Guidelines](agent-guidelines.md) - How to work effectively with the system
- [Prompt Templates](prompt-templates.md) - Reusable patterns for common scenarios

## Contributing

When adding new features or improvements:
1. Update relevant documentation
2. Add example tasks if introducing new patterns
3. Test with AI agents before committing

---

For questions or issues, refer to the main project README or create an issue in the repository.
