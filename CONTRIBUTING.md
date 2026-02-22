# Contributing to pkm-sync

This document outlines the development workflow and requirements for contributing to pkm-sync.

## Development Requirements

### Required Tools
- **Go 1.21+** - Core development language
- **golangci-lint v2.0+** - Linting and formatting (required for v2 config format)
- **make** - Build automation and CI pipeline
- **Git hooks** - Automated quality checks (run `./scripts/install-hooks.sh`)
- **Claude Code with Serena MCP** - AI-assisted development (required for agent workflows)

### Serena MCP Server Dependency
This project uses AI agents for development coordination that depend on the **Serena MCP server** for intelligent code analysis and modification.

#### Setup Serena MCP
Ensure Claude Code is configured with the Serena MCP server for:
- Symbolic code analysis and modification
- Memory-based context sharing between AI agents
- Precise, surgical code changes

#### Agent Development Requirements
**ALL agent types** working on this project must:

1. **Use surgical code modifications only**:
   - `mcp__serena__replace_symbol_body` instead of broad file edits
   - `mcp__serena__insert_after_symbol` for clean additions
   - Never edit files without first using `mcp__serena__find_symbol`

2. **Maintain shared context throughout task**:
   - `mcp__serena__write_memory` to document findings and changes
   - Update task-specific memories for subsequent agents
   - Create completion summaries before marking tasks done

3. **Complete with CI compliance and context maintenance**:
   - Tasks are NOT complete without proper Serena memory updates
   - Must pass `make ci` completely before task completion
   - Must clean up temporary files (preserve Serena memories for context transfer)

## Development Workflow

### Initial Setup
```bash
# Clone and setup
git clone <repository-url>
cd pkm-sync

# REQUIRED: Install golangci-lint v2.0+ (if not already installed)
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# REQUIRED: Install Git hooks for quality checks
./scripts/install-hooks.sh

# Verify tools and configuration
make check-golangci-version  # Verify golangci-lint v2.0+
make ci
./pkm-sync setup --dry-run
```

### Agent-Assisted Development
This project uses coordinated AI agents for development tasks:

- **`/coordinate "<task description>"`** - Get intelligent workflow recommendations  
- **`.claude/agents/coordinator.md`** - Coordination agent with workflow patterns and strategies

#### Agent Coordination
```bash
# 1. Check available context
mcp__serena__list_memories

# 2. Load relevant memories for task
mcp__serena__read_memory "project_overview"
mcp__serena__read_memory "architecture_overview" 

# 3. Architecture analysis 
mcp__serena__get_symbols_overview <target_files>
mcp__serena__find_symbol <key_components>
```

#### Agent Memory Coordination
```bash
# Agents create shared context for handoffs
mcp__serena__write_memory "task_analysis" "architectural decisions..."

# Cross-agent coordination
mcp__serena__read_memory "task_analysis"  # Load context from previous agent
```

### Quality Requirements

#### Mandatory CI Compliance
**All code changes must pass `make ci` before completion:**
```bash
make ci
# This includes:
# - go fmt (code formatting)
# - golangci-lint run (comprehensive linting) 
# - go test ./... (all unit tests with race detection)
# - go build ./cmd (compilation verification)
```

#### Git Hooks
The pre-commit hook automatically ensures code quality by running the full CI pipeline before each commit. Install with:
```bash
./scripts/install-hooks.sh
```

**Without pre-commit hooks**, pull requests may fail CI checks, requiring additional commits to fix formatting and quality issues.

## Architecture Guidelines

### Universal Interface Pattern
- Use `FullItem` for all data model interactions
- Implement `BasicItem` for standard items, `Thread` for email threads
- Follow existing patterns in `pkg/interfaces/` and `pkg/models/`

### OAuth Security Requirements
- OAuth 2.0 only (no ADC support)
- Secure token storage in platform-specific directories
- Proper token refresh handling for long-running operations

### Configuration System
- YAML-based configuration with validation
- Multi-source support with `enabled_sources` array
- Search paths: custom dir → global config → local repository

## Task Types and Agent Coordination

### Complex Features (use Parallel Analysis Pattern)
- Multiple AI agents analyze different aspects simultaneously
- Synthesis phase combines recommendations
- Progressive implementation with continuous integration

### Large Refactoring (use Progressive Implementation Pattern)
- Stage-gate approach with early issue detection
- Memory-based context sharing between stages
- Continuous CI validation at each stage

### Bug Fixes (use Specialized Chain Pattern)
- bug-hunter → code-implementer → code-reviewer
- Focus on root cause analysis and regression prevention

## Testing Strategy

### Test Coverage Requirements
- Unit tests for all new functionality
- Integration tests for cross-component changes
- Performance benchmarks for optimization work
- Security validation for authentication changes

### CI Pipeline Integration
All tests must integrate with `make ci` and pass consistently.

## Documentation Requirements

### Code Documentation
- Clear function and type documentation
- Architecture decisions documented in CLAUDE.md
- API changes documented with examples

### Agent Memory Documentation
Use Serena memory system to document:
- Architectural decisions and rationale
- Implementation approach and constraints
- Testing strategy and results
- Performance considerations

## Tools and Environment

### Required Tools
- **Go 1.21+**: `go version`
- **golangci-lint v2.0+**: `golangci-lint version`
- **make**: `make --version`

### Issue and PR Management
Always use the `gh` CLI for GitHub interactions:
```bash
# Create and manage issues
gh issue list
gh issue create --title "Bug: Description" --body "Details"
gh issue view <number>

# Create and manage pull requests
gh pr list
gh pr create --title "feat: Description" --body "Details"
gh pr view <number>
```

## Getting Help

### Documentation
- **CLAUDE.md** - Project architecture and agent coordination
- **CONFIGURATION.md** - Complete configuration documentation  
- **README.md** - Quick start and overview

### Community
- Open an issue for bugs or feature requests
- Use discussions for questions and design feedback
- Follow the code of conduct for all interactions

This simplified approach ensures all contributors can work effectively with the agent coordination system while maintaining high code quality standards.