# internal/sources/google/gmail/ — Gmail Source

## Thread Grouping

The Gmail source supports three thread modes (set via `gmail.thread_mode`):

| Mode | Behavior |
|------|----------|
| `individual` (default) | Each email is a separate item |
| `consolidated` | All messages in a thread → single file |
| `summary` | Summary file with key messages per thread |

Smart message selection: prioritizes different senders, longer content, attachments.

```yaml
sources:
  gmail_work:
    type: gmail
    gmail:
      include_threads: true
      thread_mode: "summary"
      thread_summary_length: 3   # key messages to include
      query: "in:inbox to:me"
```

## Output Filename Patterns

- Consolidated: `Thread_PR-discussion-fix-security-issue_8-messages.md`
- Summary: `Thread-Summary_meeting-notes-weekly-sync_5-messages.md`
- Individual: `Re-Project-status-update.md`

Filenames are sanitized: no spaces, command-line friendly.
