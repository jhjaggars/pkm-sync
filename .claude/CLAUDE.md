# .claude/ — Agent & Tooling Configuration

## Available Agents (`agents/`)

Specialized subagents for development workflows — invoke via the Agent tool with `subagent_type`:

| Agent | Purpose |
|-------|---------|
| `feature-architect` | System design and architecture planning |
| `code-implementer` | Feature implementation and fixes |
| `security-analyst` | Security analysis and threat modeling |
| `performance-optimizer` | Performance analysis and optimization |
| `test-strategist` | Test strategy and QA |
| `bug-hunter` | Debugging and issue resolution |
| `code-reviewer` | Code quality and maintainability review |
| `technical-writer` | Technical documentation |
| `documentation-writer` | User-focused documentation and guides |
| `coordinator` | Multi-agent workflow orchestration |

## Claude Code Skills

The `pkm-search` skill in the ObsidianVault exposes unified search across all pkm-sync sinks:

| Skill | Location | Purpose |
|-------|----------|---------|
| `pkm-search` | `ObsidianVault/.claude/skills/pkm-search/SKILL.md` | Semantic search via `pkm-sync search`, Gmail FTS4 via `archive.db`, Slack via `slack.db` |

## Agent Standards

All agents must:
- Run `make ci` before completing tasks
- Use `gh` CLI for GitHub interactions
- Update CLAUDE.md files when changing functionality they document
