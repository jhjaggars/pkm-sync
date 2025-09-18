# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Development Commands

```bash
# Build the application
go build -o pkm-sync ./cmd

# Run the application (requires OAuth setup first)
./pkm-sync setup             # Verify authentication configuration
./pkm-sync gmail             # Sync Gmail emails to PKM systems
./pkm-sync calendar          # List and sync Google Calendar events
./pkm-sync drive             # Export Google Drive documents to markdown

# Configuration management
./pkm-sync config init       # Create default configuration
./pkm-sync config show       # Display current configuration
./pkm-sync config validate   # Validate configuration

# Gmail-specific examples
./pkm-sync gmail --source gmail_work --output ./work-emails
./pkm-sync gmail --since 7d   # Sync last 7 days from all enabled Gmail sources
./pkm-sync gmail --dry-run    # Preview what would be synced

# Custom paths
./pkm-sync --credentials /path/to/credentials.json setup
./pkm-sync --config-dir /custom/config/dir setup

# Development setup
./scripts/install-hooks.sh   # Install Git hooks (pre-commit formatting)
make check-golangci-version  # Verify golangci-lint v2.0+ installation

# GitHub interactions (always use gh CLI)
gh issue list                 # List repository issues
gh issue view <number>        # View specific issue
gh issue create              # Create new issue
gh pr list                   # List pull requests
gh pr view <number>          # View specific pull request
gh pr create                 # Create new pull request
```

## Architecture Overview

This is a Go CLI application that provides universal Personal Knowledge Management (PKM) synchronization. It connects multiple data sources (Google Calendar, Gmail, Drive) to PKM targets (Obsidian, Logseq) using OAuth 2.0 authentication.

### CLI Framework
- Uses **Cobra** for command structure with persistent flags
- Root command (`cmd/root.go`) handles global flags: `--credentials`, `--config-dir`
- Main commands: `gmail`, `calendar`, `drive`, `config`, `setup`
- Global flags are processed in `PersistentPreRun` to configure paths

### Multi-Source Architecture
- **Universal interfaces** (`pkg/interfaces/`) for Source, Target, and Transformer abstractions
- **Universal data model** (`pkg/models/item.go`) with ItemInterface for consistent data representation
  - **ItemInterface**: Universal interface for all item types with getter/setter methods
  - **BasicItem**: Standard implementation for emails, calendar events, documents
  - **Thread**: Specialized implementation for email threads with embedded messages
- **Source implementations** in `internal/sources/` (Google Calendar, Gmail, Drive)
- **Target implementations** in `internal/targets/` (Obsidian, Logseq) with thread-aware formatting
- **Transformer pipeline** (`internal/transform/`) for configurable item processing
- **Sync engine** (`internal/sync/`) handles data pipeline with optional transformations

### Configuration System (`internal/config/config.go`)
- **Multi-source configuration** supporting enabled sources array
- **YAML-based configuration** with comprehensive options
- **Configuration search paths**:
  1. Custom directory (via `--config-dir` flag)
  2. Global config: `~/.config/pkm-sync/config.yaml`
  3. Local repository: `./config.yaml` (current directory)
- **Complete documentation** in `CONFIGURATION.md`

### Authentication Flow
- **OAuth 2.0 only** (no ADC support) with Google Calendar, Drive, and Gmail APIs
- Enhanced copy/paste flow: supports pasting full callback URL or just auth code
- Automatic extraction of auth code from URLs with `extractAuthCode()` function
- Token and credentials stored in platform-specific config directories
- **Gmail requires additional scope**: `gmail.readonly` for email access

### Data Flow
1. **Multi-source collection**: Sync engine iterates through enabled sources
2. **Universal data model**: Each source converts data to common `ItemInterface` format
3. **Transform pipeline**: Optional processing chain for item modification, filtering, and enhancement
4. **Source tagging**: Optional tags added to identify data source
5. **Target export**: Items formatted and exported according to target type
6. **Single output directory**: All targets use `sync.default_output_dir`

### Key Dependencies
- `github.com/spf13/cobra` - CLI framework
- `google.golang.org/api/calendar/v3` - Google Calendar API
- `google.golang.org/api/drive/v3` - Google Drive API
- `google.golang.org/api/gmail/v1` - Gmail API
- `golang.org/x/oauth2/google` - OAuth 2.0 client
- `gopkg.in/yaml.v3` - YAML configuration parsing

### Development Tools
- **golangci-lint v2.0+** - Required for v2 configuration format
  - The `.golangci.yml` uses v2-specific features like `formatters` section
  - Install: `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`
  - Alternative: `curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v2.4.0`
  - Verify: `make check-golangci-version`

## Current Implementation Status

### Sources
- ✅ **Gmail** - Fully implemented with **Gmail Threads API migration**, multi-instance support, advanced filtering, thread grouping, and performance optimizations
- ✅ **Google Calendar** - Fully implemented in `internal/sources/google/`
- ✅ **Google Drive** - Fully implemented for document export
- 🔧 **Slack** - Configuration ready, implementation pending
- 🔧 **Jira** - Configuration ready, implementation pending

### Targets
- ✅ **Obsidian** - Implemented with YAML frontmatter and hierarchical structure
- ✅ **Logseq** - Implemented with property blocks and flat structure

### Configuration Features
- ✅ **Multi-source support** with `enabled_sources` array
- ✅ **Per-source configuration** (intervals, priorities, filtering, output routing)
- ✅ **Multi-instance Gmail** (work, personal, newsletters) with independent configurations
- ✅ **Thread grouping** with configurable modes (individual, consolidated, summary)
- ✅ **Filename sanitization** (no spaces, command-line friendly)
- ✅ **Simplified output directory** structure with per-source subdirectories
- ✅ **Local repository configuration** support
- ✅ **Comprehensive validation** and management commands

## Command Structure

### Core Commands
- **`gmail`** - Sync Gmail emails to PKM systems
  - Supports multiple Gmail instances (work, personal, newsletters)
  - Gmail-specific configuration and filtering
  - Thread grouping: individual, consolidated, or summary modes
  - Example: `pkm-sync gmail --source gmail_work --target obsidian`

- **`calendar`** - List and sync Google Calendar events
  - Calendar-specific functionality
  - Example: `pkm-sync calendar --start 2025-01-01 --end 2025-01-31`

- **`drive`** - Export Google Drive documents to markdown
  - Drive-specific functionality for document export
  - Example: `pkm-sync drive --event-id 12345 --output ./docs`

### Utility Commands
- **`setup`** - Verify authentication configuration
  - Tests all Google services (Calendar, Drive, Gmail)
  - Provides clear error messages and instructions

- **`config`** - Manage configuration files
  - Configuration management and validation

## OAuth Setup Requirements

Users must:
1. Create Google Cloud project with Calendar/Drive/Gmail APIs enabled
2. Configure OAuth consent screen for "Desktop application"
3. Add `http://127.0.0.1:*` to authorized redirect URIs (enables automatic authorization flow)
4. Download credentials.json to config directory or use `--credentials` flag
5. Run `./pkm-sync setup` to verify configuration and complete OAuth flow

**Gmail-specific setup**:
- Enable Gmail API in Google Cloud Console
- Required scopes: `gmail.readonly` (automatically requested)
- Same OAuth credentials work for all Google services

The application uses an automatic web server-based OAuth flow that opens the user's browser and captures the authorization code automatically. If the web server fails, it falls back to the manual copy/paste flow for compatibility.

## Transformer Pipeline System

The transformer pipeline provides a configurable, chainable processing system for items between source fetch and target export. This enables content processing features like filtering, tagging, content cleanup, and future AI analysis.

### Core Architecture
- **Transformer Interface**: Simple `Transform([]ItemInterface) -> []ItemInterface` pattern
- **TransformPipeline**: Chains multiple transformers with configurable error handling
- **Configuration-driven**: Enable/disable transformers and configure processing order
- **Interface-based**: Works seamlessly with ItemInterface system supporting Thread and BasicItem types

### Configuration Example
```yaml
transformers:
  enabled: true
  pipeline_order: ["content_cleanup", "auto_tagging", "filter"]
  error_strategy: "log_and_continue"  # or "fail_fast", "skip_item"
  transformers:
    content_cleanup:
      strip_prefixes: true
    auto_tagging:
      rules:
        - pattern: "meeting"
          tags: ["work", "meeting"]
        - pattern: "urgent"
          tags: ["priority", "urgent"]
    filter:
      min_content_length: 50
      exclude_source_types: ["spam"]
      required_tags: ["important"]
```

### Built-in Transformers
- **`content_cleanup`**: Normalizes whitespace, removes email prefixes ("Re:", "Fwd:")
- **`auto_tagging`**: Adds tags based on content patterns and source metadata
- **`filter`**: Filters items by content length, source type, required tags

### Error Handling Strategies
- **`fail_fast`**: Stop processing on first transformer error
- **`log_and_continue`**: Log errors but continue with original items
- **`skip_item`**: Log errors and skip problematic items

### Integration Points
- **Sync Engine**: Automatically applies transformations between fetch and export
- **Configuration**: Transformers configured in main config.yaml
- **CLI**: Fully backward compatible - no CLI changes required

### Performance
- **Minimal overhead**: <5% performance impact when enabled
- **Memory efficient**: Processes items in-place where possible
- **Chainable**: Multiple transformers compose efficiently

## Gmail Threads API Migration

The Gmail source has been **migrated to use the Gmail Threads API** for improved performance and better thread handling. This migration provides native thread processing and more efficient data retrieval.

### Key Migration Benefits
- **Native thread support** - Uses Gmail's Threads API instead of Messages API for better thread cohesion
- **Performance improvements** - Concurrent thread fetching with rate limiting and retry logic
- **Enhanced error handling** - Thread-specific error context and recovery strategies
- **Backward compatibility** - Maintains existing message interface while adding thread capabilities

### Thread Processing Implementation
- **Concurrent fetching** - Up to 10 concurrent workers for thread retrieval with configurable delays
- **Intelligent batching** - Optimized batch processing for large mailboxes (>1000 messages)
- **Retry logic** - Exponential backoff for rate limiting and temporary errors
- **Error recovery** - Graceful handling of individual thread failures without stopping processing

### Thread API Features
- **Full thread details** - Retrieves complete thread with all messages in proper order
- **Message extraction** - Converts thread data to maintain compatibility with existing message processing
- **Performance logging** - Detailed metrics on thread retrieval and processing performance
- **Configurable delays** - User-configurable request delays to respect API limits

### Configuration Example
```yaml
sources:
  gmail_work:
    type: gmail
    gmail:
      include_threads: true           # Enable thread processing (now uses Threads API)
      thread_mode: "summary"          # Use summary mode
      thread_summary_length: 3        # Show 3 key messages per thread
      request_delay: 50ms             # Configurable delay between API requests
      batch_size: 100                 # Batch size for large mailbox processing
      query: "in:inbox to:me"
```

### Thread Modes
- **`individual`** (default) - Each email is treated as a separate item
- **`consolidated`** - All messages in a thread are combined into a single file
- **`summary`** - Creates summary files with key messages from each thread

### Thread Processing Features
- **Smart message selection** - Prioritizes different senders, longer content, attachments
- **Filename sanitization** - No spaces, command-line friendly filenames
- **Thread metadata** - Participants, duration, message count
- **Subject cleaning** - Removes "Re:", "Fwd:" prefixes
- **Chronological ordering** - Messages sorted by date within threads

### Output Examples
- Consolidated: `Thread_PR-discussion-fix-security-issue_8-messages.md`
- Summary: `Thread-Summary_meeting-notes-weekly-sync_5-messages.md`
- Individual: `Re-Project-status-update.md`

### Implementation Details
The migration is implemented in:
- `internal/sources/google/gmail/service.go` - Main service with Threads API integration
- `internal/sources/google/gmail/converter.go` - Thread-to-Item conversion logic
- `internal/sources/google/gmail/processor.go` - Content processing for threads and messages

## Development Workflow

### Initial Setup
```bash
# Clone the repository
git clone <repository-url>
cd pkm-sync

# REQUIRED: Install Git hooks for development
./scripts/install-hooks.sh
```

**⚠️ Important:** The pre-commit hook installation is **required** for all contributors. It ensures code quality by running comprehensive checks (formatting, linting, testing, building) before each commit. Commits will be blocked if quality checks fail.

### Git Hooks
The repository includes development Git hooks to maintain code quality:

- **pre-commit**: Comprehensive code quality enforcement before each commit
- Automatically runs `go fmt` on staged Go files
- Executes full CI pipeline (`make ci`) including linting, testing, and building
- Prevents commits if any quality checks fail
- Provides helpful diagnostic commands when failures occur

To install hooks after cloning:
```bash
./scripts/install-hooks.sh
```

The pre-commit hook will:
1. Format any staged Go files with `go fmt`
2. Run the complete CI pipeline (`make ci`):
   - **Lint**: Execute `golangci-lint` with comprehensive rules
   - **Test**: Run all unit tests with race detection
   - **Build**: Verify the project compiles successfully
3. Block the commit if any step fails
4. Provide clear feedback and diagnostic suggestions

**Without pre-commit hooks:** Pull requests may fail CI checks, requiring additional commits to fix formatting and quality issues. Installing hooks prevents this by catching issues locally before they're pushed.

### GitHub Workflow
Always use the `gh` CLI for GitHub interactions:

```bash
# List and manage issues
gh issue list
gh issue view <number>
gh issue create --title "Bug: Description" --body "Details"

# List and manage pull requests  
gh pr list
gh pr view <number>
gh pr create --title "feat: Description" --body "Details"
gh pr merge <number>

# Repository management
gh repo view
gh repo clone <owner/repo>
gh release list
```

**Important:** When working with GitHub programmatically or in scripts, always use `gh` CLI commands instead of direct API calls or other GitHub tools.

### Testing
```bash
# Run all tests
go test ./...

# Run specific package tests
go test ./cmd
go test ./internal/sources/google/gmail

# Run with verbose output
go test -v ./...
```

## Agent Development System

### Agent Coordination Setup
The repository includes a Claude Code agent coordination system located in `.claude/agents/` for specialized development workflows.

#### Available Agents
- **feature-architect**: System design and architecture planning
- **code-implementer**: Implementation of features and fixes
- **security-analyst**: Security analysis and threat modeling  
- **performance-optimizer**: Performance analysis and optimization
- **test-strategist**: Test strategy and quality assurance
- **bug-hunter**: Debugging and issue resolution
- **code-reviewer**: Code quality and maintainability review
- **technical-writer**: Technical documentation creation
- **documentation-writer**: User-focused documentation and guides
- **coordinator**: Multi-agent workflow orchestration

#### Agent Configuration Files
```
.claude/
├── agents/              # Agent definitions (committed)
│   ├── coordinator.md   # Main coordination agent with patterns
│   ├── feature-architect.md
│   ├── code-implementer.md
│   └── ...             # Other specialized agents
└── settings.local.json  # Local permissions (NOT committed)
```

### Serena MCP Integration (REQUIRED)
All agents working on this project must use the Serena MCP server for intelligent code analysis and modification. See `CONTRIBUTING.md` for setup instructions.

#### Key Serena Operations
```bash
# Context gathering
mcp__serena__list_memories                    # Check available context
mcp__serena__get_symbols_overview <file>      # Understand file structure
mcp__serena__find_symbol <name> <file>        # Locate specific symbols

# Surgical code modifications
mcp__serena__replace_symbol_body <symbol> <file> <new_implementation>
mcp__serena__insert_after_symbol <symbol> <file> <additional_code>
mcp__serena__insert_before_symbol <symbol> <file> <setup_code>

# Context sharing between agents
mcp__serena__write_memory "<task_name>_<agent_type>" """
Agent findings, changes made, performance results, next steps
"""
mcp__serena__read_memory "<task_name>_analysis"  # Get previous agent context
```

### Mandatory CI Compliance
**CRITICAL**: All agents must ensure `make ci` passes before completing any task involving code changes.

```bash
# Required CI verification for every task:
make ci

# This includes:
# - go fmt (code formatting)  
# - golangci-lint run (comprehensive linting)
# - go test ./... (all unit tests with race detection)
# - go build ./cmd (compilation verification)
```

### Task Completion Requirements
**A task is NEVER complete unless ALL conditions are met:**

1. **Implementation**: Complete the requested feature/fix using Serena MCP operations
2. **CI Verification**: Run `make ci` and ensure exit code is 0
3. **Context Update**: Update Serena memory with findings and changes
4. **Cleanup**: Remove temporary files (preserve Serena memories)

#### Required Context Updates
```bash
# Task completion summary (REQUIRED)
mcp__serena__write_memory "<task_name>_completion" """
## Task Completion Summary
- Files modified: [list with specific symbols changed]
- Functions/methods added/updated: [specific symbols]
- Integration points affected: [interfaces, dependencies]
- CI status: PASSED
- Issues encountered & resolved: [specific details]
- Context for next agent: [handoff information]
"""
```

### File Management
```bash
# Clean up temporary files (NOT Serena memories)
rm -f *.tmp *.temp *.log coverage.out cpu.prof mem.prof
rm -rf temp/ tmp/ .temp_*

# Preserve Serena memories for agent context transfer
# Do NOT delete: task_architecture, task_implementation, etc.
```

### Agent Workflow Patterns
Agents should follow established workflow patterns documented in `.claude/agents/coordinator.md`:

- **Progressive Implementation**: Staged development with handoff points (89% success rate)
- **Parallel Analysis**: Multiple agents analyze different aspects simultaneously  
- **Specialized Chains**: Sequential agents for focused tasks (performance, security, debugging)
- **Technology Detection**: Automatic adaptation to Go, Python, JavaScript, etc.

### Integration with Project Standards
Agent development follows the same standards as manual development:
- **Pre-commit hooks**: All code changes go through formatting, linting, and testing
- **GitHub workflow**: Use `gh` CLI for issue and PR management  
- **Configuration validation**: Ensure `./pkm-sync config validate` passes
- **Documentation updates**: Update relevant docs when changing functionality

## Enhanced Agent Coordination

The repository includes enhanced agent coordination capabilities that provide better agent selection, technology adaptation, and fallback strategies within Claude Code. These enhancements build on the existing agent system while maintaining full backward compatibility.

### Coordination Features

- **Technology Detection**: Automatic detection of Go, Python, JavaScript, Rust projects with appropriate tooling
- **Agent Fallback**: Strategies for when specialist agents aren't available
- **Proven Patterns**: Workflow patterns based on successful project executions (like Issue #28)
- **Quality Assurance**: Maintains CI compliance and code quality regardless of agent availability

### Agent Fallback Strategies

When specialist agents aren't available, the coordinator adapts using proven strategies:

- **Security tasks** → feature-architect + security checklists
- **Performance work** → code-implementer + benchmarking focus  
- **Bug hunting** → code-implementer + debugging methodology
- **Documentation** → technical-writer + user-focused templates

The system maintains quality through enhanced validation and testing when specialists are unavailable.

### Technology Adaptation

The coordination system automatically detects project type and adapts accordingly:

```bash
# Go projects
TEST_CMD="go test ./..."
LINT_CMD="golangci-lint run"  
BUILD_CMD="go build ./cmd"

# Python projects  
TEST_CMD="pytest"
LINT_CMD="flake8 . && mypy ."
BUILD_CMD="python -m build"

# JavaScript projects
TEST_CMD="npm test"
LINT_CMD="eslint . && prettier --check ."
BUILD_CMD="npm run build"
```

### Usage Examples

#### Coordination Command
```bash
# Use the coordinator for complex tasks
/coordinate "implement Gmail threads API migration"
```

The coordinator will:
1. Assess project maturity and constraints
2. Select appropriate agent workflow pattern  
3. Adapt commands for detected technology (Go)
4. Apply fallback strategies if needed
5. Ensure CI compliance throughout

#### Example Output
```markdown
## Coordination Plan: Gmail Threads API Migration

**Pattern**: Progressive Implementation (89% proven success rate)
**Technology**: Go CLI detected  
**Duration**: 8-11 hours
**Agent Sequence**: feature-architect → code-implementer → test-strategist → code-reviewer

**Commands**:
- Test: go test ./...
- Lint: golangci-lint run
- Build: go build ./cmd

**Quality Gates**: 
- CI must pass: make ci
- Performance: <5% overhead
- Backward compatibility: pre-alpha project, breaking changes OK
```
## Summary

The enhanced agent coordination system provides practical improvements to Claude Code workflows:

- **Simple, effective**: Technology detection and agent fallback without complexity
- **Proven patterns**: Based on successful project executions (Issue #28: 89% success rate) 
- **Backward compatible**: All existing agent functionality preserved
- **Quality focused**: Maintains CI compliance and code standards

The system enhances coordination effectiveness while remaining simple to understand and use.
