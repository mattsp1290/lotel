# Prompt Templates

A collection of reusable prompt patterns for common scenarios in the Proompting system.

## Task Generation Prompts

### Basic Project Task Generation
```markdown
Generate a comprehensive task YAML file for a [PROJECT_TYPE] project using [TECH_STACK].

Project Requirements:
- [REQUIREMENT_1]
- [REQUIREMENT_2]
- [REQUIREMENT_3]

Please follow the format in proompts/tasks/example-todo-app.yaml and create a detailed task breakdown with phases, dependencies, and clear task descriptions.
```

### Feature Addition
```markdown
I need to add [FEATURE_NAME] to the existing project. 

Current project uses: [TECH_STACK]
Feature requirements:
- [REQUIREMENT_1]
- [REQUIREMENT_2]

Create tasks for implementing this feature, considering the existing project structure and dependencies.
```

### Refactoring Tasks
```markdown
Create refactoring tasks to [REFACTORING_GOAL] in the current project.

Areas to refactor:
- [AREA_1]
- [AREA_2]

Ensure tasks maintain backward compatibility and include appropriate testing.
```

## Task Execution Prompts

### Starting a New Task
```markdown
I'm starting work on task ID: [TASK_ID]

Task: [TASK_NAME]
Description: [TASK_DESCRIPTION]

Please help me implement this task following the requirements and project conventions.
```

### Debugging Assistance
```markdown
I'm working on task [TASK_ID] and encountering this issue:

Error: [ERROR_MESSAGE]
Context: [WHAT_YOU_WERE_TRYING]

The task requires: [TASK_REQUIREMENTS]

Please help debug and provide a solution.
```

### Task Completion Review
```markdown
I've completed task [TASK_ID]. Here's what was implemented:

- [CHANGE_1]
- [CHANGE_2]
- [CHANGE_3]

Please review if this meets all task requirements and suggest any improvements.
```

## Project Analysis Prompts

### Dependency Analysis
```markdown
Analyze the task dependencies in the current project and identify:
1. Any circular dependencies
2. Potential bottlenecks
3. Tasks that can be parallelized

Provide recommendations for optimizing the task flow.
```

### Priority Assessment
```markdown
Review all pending tasks and suggest priority adjustments based on:
- Technical dependencies
- Business value
- Risk factors
- Resource requirements

Explain the reasoning for any suggested changes.
```

### Progress Report
```markdown
Generate a progress report for the current project including:
- Completed tasks (count and percentage)
- In-progress tasks
- Blocked tasks
- Estimated timeline for remaining work
- Key achievements so far
```

## Architecture and Design Prompts

### Component Design
```markdown
Design a [COMPONENT_TYPE] component for [PURPOSE].

Requirements:
- [REQUIREMENT_1]
- [REQUIREMENT_2]

Consider:
- Reusability
- Testing approach
- Integration with existing components
- Performance implications
```

### API Design
```markdown
Design a REST API for [RESOURCE_NAME] with the following operations:
- [OPERATION_1]
- [OPERATION_2]

Include:
- Endpoint paths
- HTTP methods
- Request/response schemas
- Error handling
- Authentication requirements
```

### Database Schema
```markdown
Design a database schema for [FEATURE_NAME].

Requirements:
- [DATA_REQUIREMENT_1]
- [DATA_REQUIREMENT_2]

Consider:
- Relationships between entities
- Indexing strategy
- Data integrity constraints
- Migration approach
```

## Testing Prompts

### Test Case Generation
```markdown
Generate test cases for task [TASK_ID].

Implementation includes:
- [FEATURE_1]
- [FEATURE_2]

Create:
- Unit test scenarios
- Integration test scenarios
- Edge cases to consider
- Expected outcomes
```

### Test Coverage Analysis
```markdown
Analyze the current test coverage for [COMPONENT/FEATURE].

Identify:
- Untested code paths
- Missing edge cases
- Integration gaps
- Suggested additional tests
```

## Documentation Prompts

### API Documentation
```markdown
Document the API endpoints created in task [TASK_ID].

For each endpoint include:
- Purpose
- Request format
- Response format
- Error codes
- Example usage
- Authentication requirements
```

### Setup Guide
```markdown
Create a setup guide for developers joining the project.

Include:
- Prerequisites
- Installation steps
- Configuration requirements
- Common issues and solutions
- Verification steps
```

### Feature Documentation
```markdown
Document the [FEATURE_NAME] feature implemented in tasks [TASK_IDS].

Include:
- Feature overview
- User guide
- Technical implementation details
- Configuration options
- Troubleshooting guide
```

## Code Review Prompts

### Security Review
```markdown
Review the code implemented in task [TASK_ID] for security issues.

Check for:
- Input validation
- Authentication/authorization
- Data exposure
- Injection vulnerabilities
- Security best practices
```

### Performance Review
```markdown
Review the implementation in task [TASK_ID] for performance.

Analyze:
- Algorithm efficiency
- Database query optimization
- Caching opportunities
- Memory usage
- Potential bottlenecks
```

### Code Quality Review
```markdown
Review code quality for task [TASK_ID].

Evaluate:
- Code organization
- Naming conventions
- Documentation
- Error handling
- Test coverage
- Adherence to project standards
```

## Integration Prompts

### Service Integration
```markdown
Integrate [EXTERNAL_SERVICE] into the project.

Requirements:
- [INTEGRATION_REQUIREMENT_1]
- [INTEGRATION_REQUIREMENT_2]

Consider:
- Authentication method
- Error handling
- Rate limiting
- Fallback strategies
```

### Migration Planning
```markdown
Plan migration from [OLD_SYSTEM] to [NEW_SYSTEM].

Current state: [DESCRIPTION]
Target state: [DESCRIPTION]

Create tasks for:
- Data migration
- Feature parity
- Testing approach
- Rollback plan
```

## Troubleshooting Prompts

### Bug Investigation
```markdown
Investigate bug: [BUG_DESCRIPTION]

Symptoms:
- [SYMPTOM_1]
- [SYMPTOM_2]

Environment: [ENVIRONMENT_DETAILS]
Steps to reproduce: [STEPS]

Provide:
- Root cause analysis
- Proposed fix
- Impact assessment
```

### Performance Investigation
```markdown
Investigate performance issue in [COMPONENT/FEATURE].

Symptoms:
- [PERFORMANCE_METRIC_1]
- [PERFORMANCE_METRIC_2]

Analyze:
- Bottleneck identification
- Optimization opportunities
- Trade-offs of proposed solutions
```

## Best Practices for Prompt Usage

1. **Be Specific**: Include concrete requirements and constraints
2. **Provide Context**: Share relevant project information
3. **Set Clear Goals**: Define what success looks like
4. **Include Examples**: Reference existing code or patterns when helpful
5. **Iterate**: Refine prompts based on results

## Customization Tips

- Replace placeholders in [BRACKETS] with specific values
- Add project-specific requirements
- Combine templates for complex scenarios
- Adjust detail level based on task complexity
- Include relevant file paths and code snippets

---

These templates are starting points. Adapt them to fit your specific project needs and team preferences.
