# Agent Guidelines

This guide provides best practices and guidelines for AI agents working with the Proompting system.

## Quick Reference

- **Task Files Location**: `/proompts/tasks/`
- **Active Project**: Always use `tasks.yaml` in the root directory
- **Status Updates**: Required when starting and completing tasks
- **File Organization**: Keep all generated files within appropriate project directories

## Core Principles

### 1. Task-Driven Development
- Always reference the current task when working
- Complete one task at a time
- Don't skip ahead or work on unassigned tasks
- Update task status as you progress

### 2. File Organization
- Create task files in `/proompts/tasks/` directory
- Use descriptive filenames: `project-name.yaml`
- Don't create files in random locations
- Follow project structure conventions

### 3. Status Management
When working on a task:
1. Set status to `in-progress` when starting
2. Complete all requirements in the description
3. Set status to `completed` when done
4. Add an entry to the updates section

## Working with Tasks

### Finding the Next Task

1. Read the active `tasks.yaml` file
2. Look for tasks with:
   - Status: `pending`
   - All dependencies: `completed`
   - Highest priority among available tasks

### Task Execution Process

```
1. Identify next task
   ↓
2. Check dependencies are completed
   ↓
3. Update status to 'in-progress'
   ↓
4. Execute task requirements
   ↓
5. Verify completion criteria
   ↓
6. Update status to 'completed'
   ↓
7. Add update entry with timestamp
```

### Handling Dependencies

**Before starting a task:**
- Verify all dependencies have `status: completed`
- If dependencies aren't complete, choose a different task
- Never work on tasks with incomplete dependencies

**Example dependency check:**
```yaml
# If task has:
dependencies: [setup-database, create-user-model]

# Verify both are completed:
- id: setup-database
  status: completed  ✓
- id: create-user-model
  status: completed  ✓
```

## Best Practices

### 1. Communication
- Be clear about what you're working on
- Explain your approach before implementing
- Ask for clarification if requirements are unclear
- Report blockers immediately

### 2. Code Quality
- Follow the project's established patterns
- Use the tech stack specified in metadata
- Write clean, documented code
- Consider error handling and edge cases

### 3. Testing
- Test your implementation thoroughly
- Verify the task meets all requirements
- Check that nothing breaks existing functionality
- Document any testing performed

### 4. Documentation
- Update relevant documentation as you work
- Add code comments for complex logic
- Create examples when helpful
- Keep the updates section current

## File Management

### Creating New Files
- Place files in logical directories
- Follow project naming conventions
- Don't overwrite existing files without reason
- Create directories as needed

### Modifying Existing Files
- Make targeted changes
- Preserve existing functionality
- Follow the file's established style
- Test after modifications

### Example Structure
```
project-root/
├── src/
│   ├── components/
│   ├── utils/
│   └── types/
├── tests/
├── docs/
└── proompts/
    └── tasks/
        └── project-task.yaml
```

## Common Pitfalls to Avoid

### 1. Working Out of Order
❌ Starting tasks with incomplete dependencies
✅ Only work on tasks with all dependencies completed

### 2. Incomplete Status Updates
❌ Forgetting to update task status
✅ Always update status when starting and completing

### 3. Scope Creep
❌ Adding features not in the task description
✅ Stick to the defined requirements

### 4. Poor File Organization
❌ Creating files in random locations
✅ Follow project structure conventions

### 5. Skipping Documentation
❌ Implementing without documenting
✅ Update docs and add comments as you go

## Task Status Workflow

### Status Transitions
```
pending → in-progress → completed
        ↘              ↗
          → blocked →
```

### When to Use Each Status
- **pending**: Initial state, task not started
- **in-progress**: Actively working on the task
- **completed**: All requirements met and tested
- **blocked**: (rare) External dependency preventing progress

## Working with Different Task Types

### Setup Tasks
- Usually have few or no dependencies
- Set foundation for other work
- Test configuration thoroughly
- Document setup steps

### Feature Tasks
- Build on setup tasks
- May have multiple dependencies
- Focus on user-facing functionality
- Include appropriate tests

### Testing Tasks
- Verify feature functionality
- Should cover edge cases
- Document test scenarios
- Report any issues found

### Documentation Tasks
- Keep docs in sync with code
- Use clear, concise language
- Include examples
- Update based on implementation

## Integration with Tools

### When Using Cursor/Cline
- Reference task IDs in commits
- Keep changes focused on current task
- Use the task description as context
- Update task file after completion

### Version Control
- Commit completed tasks separately
- Reference task ID in commit messages
- Example: `[task-id] Implement user authentication`
- Push changes after task completion

## Debugging and Problem Solving

### When Stuck
1. Re-read the task description
2. Check examples in documentation
3. Review completed similar tasks
4. Ask for clarification if needed

### Common Issues
- **Missing dependencies**: Check if required packages are installed
- **Failing tests**: Ensure all changes are compatible
- **Unclear requirements**: Refer to project documentation or ask

## Summary Checklist

Before considering a task complete:

- [ ] All requirements in description are met
- [ ] Code is tested and working
- [ ] Status is updated to 'completed'
- [ ] Update entry is added with date
- [ ] Any new files are in correct locations
- [ ] Documentation is updated if needed
- [ ] No existing functionality is broken

---

Remember: The goal is systematic, reliable task completion. When in doubt, refer to the task description and these guidelines.
