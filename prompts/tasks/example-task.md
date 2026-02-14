---
task_id: setup-github-actions
task_reference: "tasks.yaml#phases[0].tasks[3]"
---

# 🚀 Task Execution: Configure GitHub Actions CI/CD Pipeline

## Mission Critical: Continuous Integration Foundation

### Task Overview
You are tasked with establishing a world-class CI/CD pipeline that ensures code quality, catches regressions early, and maintains the exceptional standards required for a cross-platform WebAssembly runtime. This pipeline will be the guardian of code quality, the accelerator of development velocity, and the foundation of trust for all contributors.

## Context

### Background
- **Project**: Flight Core - A runtime that must work flawlessly across wildly different platforms
- **Challenge**: Build a CI system that validates code on macOS, Linux, and prepares for embedded platforms
- **Stakes**: Every commit could break compatibility; CI is the first line of defense
- **Impact**: Developer productivity and code quality depend on fast, reliable CI feedback

### Dependencies
- ✅ **setup-project-structure**: Project layout is complete
- ✅ **setup-cmake-build**: CMake configuration exists (task file available)
- 🔄 **Infra Team Support**: May need assistance from birb-home team for infrastructure

### Constraints
- **Build Time**: Must provide feedback within 10 minutes
- **Platform Coverage**: Initially macOS and Linux, extensible to embedded
- **Resource Usage**: Be mindful of GitHub Actions minutes
- **Caching**: Maximize cache usage for dependencies

## Execution Strategy

### Approach
Create a CI pipeline that:
1. **Validates Quickly**: Fast feedback on every push
2. **Tests Thoroughly**: Comprehensive test coverage
3. **Builds Everywhere**: Matrix builds across platforms/compilers
4. **Reports Clearly**: Actionable feedback for developers
5. **Scales Gracefully**: Easy to add new platforms

### Milestones
1. ✅ Create basic workflow structure
2. ✅ Configure build matrix (OS × Compiler)
3. ✅ Set up dependency caching with CPM
4. ✅ Add test execution and reporting
5. ✅ Configure code coverage collection
6. ✅ Set up benchmark runs
7. ✅ Implement artifact publishing
8. ✅ Add badge generation

### Success Criteria
- [x] CI runs on every push and PR
- [x] Build matrix covers macOS/Linux with Clang/GCC
- [x] Tests execute with clear pass/fail status
- [x] Coverage reports are generated and accessible
- [x] Build artifacts are preserved
- [x] CI completes in under 10 minutes

## Resources

### Required Tools
- GitHub Actions workflow syntax knowledge
- CMake build system understanding
- CPM package manager familiarity
- Coverage tools (lcov, gcov)
- Benchmark result processing

### Reference Materials
- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [Flight Project Workflows](file://$HOME/git/flight/.github/workflows) - Existing examples
- [CMake GitHub Actions](https://github.com/marketplace/actions/cmake-build)
- [CPM Cache Action](https://github.com/marketplace/actions/cache)

### Infrastructure Needs (birb-home team)
- Repository secrets for deployment
- Self-hosted runners for embedded platforms (future)
- Artifact storage configuration
- Integration with monitoring systems

## Confidence Boosters

### Similar Successes
- The Flight project already has working GitHub Actions
- CMake projects commonly use GitHub Actions
- CPM has built-in cache support
- Matrix builds are well-documented

### Available Support
- GitHub Actions has excellent documentation
- Many C++ projects to reference
- Infra team at birb-home for complex needs
- GitHub support for Actions issues

### Fallback Options
- Start with simple builds, add features incrementally
- Use GitHub-hosted runners initially
- Can disable slow jobs temporarily
- Manual builds always possible locally

## Execution Commands

```bash
# Create workflow directory
mkdir -p .github/workflows

# Key files to create:
# 1. .github/workflows/ci.yml - Main CI workflow
# 2. .github/workflows/benchmark.yml - Performance tracking
# 3. .github/workflows/coverage.yml - Coverage reporting
# 4. .github/dependabot.yml - Dependency updates
# 5. scripts/ci/ - CI helper scripts
```

## Ultra-Thinking Considerations

### Main CI Workflow Structure
```yaml
name: CI

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main]

env:
  CPM_SOURCE_CACHE: ${{ github.workspace }}/cpm_modules

jobs:
  build:
    name: ${{ matrix.os }} - ${{ matrix.compiler }}
    runs-on: ${{ matrix.os }}
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, macos-latest]
        compiler: [gcc, clang]
        build_type: [Debug, Release]
        exclude:
          - os: macos-latest
            compiler: gcc  # Use Apple Clang on macOS

    steps:
    - uses: actions/checkout@v4
      with:
        submodules: recursive

    - name: Cache CPM dependencies
      uses: actions/cache@v3
      with:
        path: ${{ env.CPM_SOURCE_CACHE }}
        key: ${{ runner.os }}-cpm-${{ hashFiles('**/CMakeLists.txt') }}
        restore-keys: |
          ${{ runner.os }}-cpm-

    - name: Configure CMake
      run: |
        cmake -B build \
          -DCMAKE_BUILD_TYPE=${{ matrix.build_type }} \
          -DCMAKE_C_COMPILER=${{ matrix.compiler }} \
          -DCMAKE_CXX_COMPILER=${{ matrix.compiler == 'gcc' && 'g++' || 'clang++' }} \
          -DFLIGHT_CORE_BUILD_TESTS=ON \
          -DFLIGHT_CORE_BUILD_BENCHMARKS=ON

    - name: Build
      run: cmake --build build --config ${{ matrix.build_type }} --parallel 4

    - name: Test
      run: ctest --test-dir build --config ${{ matrix.build_type }} --output-on-failure

    - name: Upload artifacts
      uses: actions/upload-artifact@v3
      with:
        name: flight-core-${{ matrix.os }}-${{ matrix.compiler }}-${{ matrix.build_type }}
        path: build/bin/
```

### Coverage Workflow Considerations
```yaml
- name: Generate coverage
  if: matrix.build_type == 'Debug'
  run: |
    cmake --build build --target coverage
    
- name: Upload coverage to Codecov
  uses: codecov/codecov-action@v3
  with:
    file: build/coverage.info
    flags: unittests
    name: flight-core-coverage
```

### Benchmark Workflow Design
- Run benchmarks on consistent hardware (self-hosted runner?)
- Store results for trend analysis
- Fail PR if performance regresses > 10%
- Generate performance graphs

### Platform-Specific Considerations
- **Linux**: Need to install build-essential, cmake
- **macOS**: Use Homebrew for dependencies
- **Windows**: Future addition, use MSVC
- **Embedded**: Will need custom runners (birb-home team)

### Optimization Strategies
1. **Parallel Jobs**: Split tests/benchmarks into separate jobs
2. **Ccache Integration**: Speed up rebuilds
3. **Incremental Testing**: Only test changed modules
4. **Smart Caching**: Cache build artifacts between jobs

### Security Considerations
- No secrets in workflow files
- Use environment protection rules
- Limit permissions with `permissions:`
- Scan for vulnerabilities in dependencies

### Future Enhancements
1. **Emscripten Builds**: Add web platform testing
2. **Cross-Compilation**: Test embedded builds
3. **Deployment Pipeline**: Publish releases automatically
4. **Integration Tests**: Test with downstream projects

## Critical Implementation Details

### Error Handling
- Always use `continue-on-error: false` for critical steps
- Provide clear error messages in scripts
- Upload logs as artifacts for debugging
- Set up notifications for failures

### Performance Optimization
- Use GitHub's larger runners for intensive jobs
- Parallelize independent tasks
- Cache everything possible
- Minimize checkout depth when possible

### Developer Experience
- Fast feedback on PRs (< 5 minutes for basic checks)
- Clear status badges in README
- Detailed logs for debugging failures
- Automatic PR comments with results

### Monitoring and Alerts
- Track CI runtime trends
- Alert on repeated failures
- Monitor GitHub Actions usage
- Report to team dashboard

## Infrastructure Team Request (If Needed)

If we need infrastructure support, create request for birb-home team:

```yaml
# birb-home-request.yaml
request:
  team: infra
  project: flight-core
  needs:
    - github_secrets:
        - CODECOV_TOKEN
        - DEPLOYMENT_KEY
    - self_hosted_runners:
        - platform: dreamcast
        - platform: psp
    - artifact_storage:
        - retention: 30 days
        - size_limit: 1GB
    - monitoring:
        - ci_performance_dashboard
        - failure_alerts
```

## Final Charge

This CI/CD pipeline is not just automation—it's the heartbeat of the project. Every successful build increases confidence. Every caught regression saves hours of debugging. Every performance benchmark guards against degradation.

Your GitHub Actions configuration must be:
- **Reliable**: No flaky tests or random failures
- **Fast**: Developers shouldn't wait for results
- **Comprehensive**: Cover all critical paths
- **Maintainable**: Easy to update and extend

Execute with the knowledge that this pipeline will run thousands of times, catch countless bugs, and enable fearless development!

---

**"CI/CD is not about finding problems, it's about preventing them from reaching users."**

**NOW BUILD THE PIPELINE THAT GUARDS THE FUTURE OF WEBASSEMBLY!** 🛡️
