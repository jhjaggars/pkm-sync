package models

import "time"

// Config represents the application configuration.
type Config struct {
	// Default sync settings
	Sync SyncConfig `json:"sync" yaml:"sync"`

	// Source configurations
	Sources map[string]SourceConfig `json:"sources" yaml:"sources"`

	// Target configurations
	Targets map[string]TargetConfig `json:"targets" yaml:"targets"`

	// Transformer configurations
	Transformers TransformConfig `json:"transformers" yaml:"transformers"`

	// Authentication settings
	Auth AuthConfig `json:"auth" yaml:"auth"`

	// General application settings
	App AppConfig `json:"app" yaml:"app"`

	// Vector database settings
	VectorDB VectorDBConfig `json:"vectordb" yaml:"vectordb"`

	// Embeddings provider settings
	Embeddings EmbeddingsConfig `json:"embeddings" yaml:"embeddings"`

	// Email archive settings
	Archive ArchiveConfig `json:"archive" yaml:"archive"`
}

// TransformConfig defines transformer pipeline configuration.
type TransformConfig struct {
	Enabled       bool     `json:"enabled"        yaml:"enabled"`
	PipelineOrder []string `json:"pipeline_order" yaml:"pipeline_order"`
	// "fail_fast", "log_and_continue", "skip_item"
	ErrorStrategy string                            `json:"error_strategy" yaml:"error_strategy"`
	Transformers  map[string]map[string]interface{} `json:"transformers"   yaml:"transformers"`
}

type SyncConfig struct {
	// Multi-source configuration
	EnabledSources []string `json:"enabled_sources" yaml:"enabled_sources"` // ["google_calendar", "slack", "gmail"]
	DefaultTarget  string   `json:"default_target"  yaml:"default_target"`

	// Default time range for syncing
	DefaultSince string `json:"default_since" yaml:"default_since"`

	// Default output directory
	DefaultOutputDir string `json:"default_output_dir" yaml:"default_output_dir"`

	// Source-specific scheduling
	SourceSchedules map[string]string `json:"source_schedules" yaml:"source_schedules"` // e.g. "google_calendar": "1h"

	// Global sync settings
	AutoSync     bool          `json:"auto_sync"     yaml:"auto_sync"`
	SyncInterval time.Duration `json:"sync_interval" yaml:"sync_interval"` // Fallback interval

	// Data handling
	MergeSources  bool   `json:"merge_sources"  yaml:"merge_sources"`  // Combine all sources into single export
	SourceTags    bool   `json:"source_tags"    yaml:"source_tags"`    // Add source-specific tags
	OnConflict    string `json:"on_conflict"    yaml:"on_conflict"`    // "skip", "overwrite", "prompt"
	DeduplicateBy string `json:"deduplicate_by" yaml:"deduplicate_by"` // "id", "title", "content", "none"

	// File management
	CreateSubdirs   bool   `json:"create_subdirs"    yaml:"create_subdirs"`
	SubdirFormat    string `json:"subdir_format"     yaml:"subdir_format"` // "yyyy/mm", "yyyy-mm", "source", "flat"
	MaxFileAge      string `json:"max_file_age"      yaml:"max_file_age"`  // "30d", "6m", "1y"
	ArchiveOldFiles bool   `json:"archive_old_files" yaml:"archive_old_files"`
}

type SourceConfig struct {
	// Basic source settings
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Type    string `json:"type"    yaml:"type"`

	// Per-source overrides (NEW)
	Name         string        `json:"name,omitempty"          yaml:"name,omitempty"` // Human-readable instance name
	OutputSubdir string        `json:"output_subdir,omitempty" yaml:"output_subdir,omitempty"`
	OutputTarget string        `json:"output_target,omitempty" yaml:"output_target,omitempty"`
	SyncInterval time.Duration `json:"sync_interval,omitempty" yaml:"sync_interval,omitempty"`
	Since        string        `json:"since,omitempty"         yaml:"since,omitempty"`
	Priority     int           `json:"priority,omitempty"      yaml:"priority,omitempty"`

	// Source-specific configurations
	Google GoogleSourceConfig `json:"google,omitempty" yaml:"google,omitempty"`
	Slack  SlackSourceConfig  `json:"slack,omitempty"  yaml:"slack,omitempty"`
	Gmail  GmailSourceConfig  `json:"gmail,omitempty"  yaml:"gmail,omitempty"`
	Jira   JiraSourceConfig   `json:"jira,omitempty"   yaml:"jira,omitempty"`
	Drive  DriveSourceConfig  `json:"drive,omitempty"  yaml:"drive,omitempty"`
}

// DriveSourceConfig defines configuration for a Google Drive source.
type DriveSourceConfig struct {
	Name        string `json:"name"        yaml:"name"`
	Description string `json:"description" yaml:"description"`

	// Folders to sync; empty = root only
	FolderIDs []string `json:"folder_ids" yaml:"folder_ids"`
	// Recurse into subfolders (default: true)
	Recursive bool `json:"recursive" yaml:"recursive"`

	IncludeSharedWithMe bool `json:"include_shared_with_me" yaml:"include_shared_with_me"`
	IncludeSharedDrives bool `json:"include_shared_drives"  yaml:"include_shared_drives"`

	// Which workspace types to export (empty = all): "document", "spreadsheet", "presentation"
	WorkspaceTypes []string `json:"workspace_types" yaml:"workspace_types"`

	// Export format preferences
	DocExportFormat   string `json:"doc_export_format"   yaml:"doc_export_format"`   // "md" (default), "txt", "html"
	SheetExportFormat string `json:"sheet_export_format" yaml:"sheet_export_format"` // "csv" (default), "html"
	SlideExportFormat string `json:"slide_export_format" yaml:"slide_export_format"` // "txt" (default), "html"

	// Custom Drive API query (appended with AND to the generated query)
	Query string `json:"query" yaml:"query"`

	// Rate limiting
	RequestDelay time.Duration `json:"request_delay" yaml:"request_delay"`
	MaxRequests  int           `json:"max_requests"  yaml:"max_requests"`
}

type GoogleSourceConfig struct {
	// Calendar settings
	CalendarID      string   `json:"calendar_id"      yaml:"calendar_id"` // "primary" or specific calendar
	IncludeDeclined bool     `json:"include_declined" yaml:"include_declined"`
	IncludePrivate  bool     `json:"include_private"  yaml:"include_private"`
	EventTypes      []string `json:"event_types"      yaml:"event_types"` // filter by event types
	// maximum number of events to fetch (default: 1000)
	MaxResults int `json:"max_results" yaml:"max_results"`

	// Attendee filtering
	// only include events with these attendees
	AttendeeAllowList []string `json:"attendee_allow_list" yaml:"attendee_allow_list"`
	// exclude events with 0-1 attendees (default: true)
	RequireMultipleAttendees bool `json:"require_multiple_attendees" yaml:"require_multiple_attendees"`
	// include events where you're the only attendee (default: false)
	IncludeSelfOnlyEvents bool `json:"include_self_only_events" yaml:"include_self_only_events"`

	// Drive settings
	DownloadDocs  bool     `json:"download_docs"  yaml:"download_docs"`
	DocFormats    []string `json:"doc_formats"    yaml:"doc_formats"`  // "markdown", "pdf", "docx"
	MaxDocSize    string   `json:"max_doc_size"   yaml:"max_doc_size"` // "10MB"
	IncludeShared bool     `json:"include_shared" yaml:"include_shared"`

	// Rate limiting
	RequestDelay time.Duration `json:"request_delay" yaml:"request_delay"`
	MaxRequests  int           `json:"max_requests"  yaml:"max_requests"`
}

type TargetConfig struct {
	// Target type (output directory comes from SyncConfig.DefaultOutputDir)
	Type string `json:"type" yaml:"type"`

	// Obsidian-specific settings
	Obsidian ObsidianTargetConfig `json:"obsidian,omitempty" yaml:"obsidian,omitempty"`

	// Logseq-specific settings
	Logseq LogseqTargetConfig `json:"logseq,omitempty" yaml:"logseq,omitempty"`
}

type ObsidianTargetConfig struct {
	// Vault organization (vault path is the output directory)
	DefaultFolder string `json:"default_folder" yaml:"default_folder"` // "Calendar", "Inbox"

	// File naming and organization
	FilenameTemplate string `json:"filename_template" yaml:"filename_template"` // "{{date}} - {{title}}"
	DateFormat       string `json:"date_format"       yaml:"date_format"`       // "2006-01-02"
	TagPrefix        string `json:"tag_prefix"        yaml:"tag_prefix"`        // "calendar/"

	// Content formatting
	IncludeFrontmatter bool     `json:"include_frontmatter" yaml:"include_frontmatter"`
	CustomFields       []string `json:"custom_fields"       yaml:"custom_fields"`
	TemplateFile       string   `json:"template_file"       yaml:"template_file"`

	// Linking and references
	CreateDailyNotes bool   `json:"create_daily_notes" yaml:"create_daily_notes"`
	DailyNotesFolder string `json:"daily_notes_folder" yaml:"daily_notes_folder"`
	LinkFormat       string `json:"link_format"        yaml:"link_format"` // "wikilink", "markdown"

	// Attachments
	AttachmentFolder    string `json:"attachment_folder"    yaml:"attachment_folder"`
	DownloadAttachments bool   `json:"download_attachments" yaml:"download_attachments"`
}

type LogseqTargetConfig struct {
	// Graph settings (graph path is the output directory)
	DefaultPage string `json:"default_page" yaml:"default_page"`

	// Content formatting
	UseProperties    bool   `json:"use_properties"    yaml:"use_properties"`
	PropertyPrefix   string `json:"property_prefix"   yaml:"property_prefix"`
	BlockIndentation int    `json:"block_indentation" yaml:"block_indentation"`

	// Journal integration
	CreateJournalRefs bool   `json:"create_journal_refs" yaml:"create_journal_refs"`
	JournalDateFormat string `json:"journal_date_format" yaml:"journal_date_format"`
}

type AuthConfig struct {
	// OAuth settings
	CredentialsPath string `json:"credentials_path" yaml:"credentials_path"`
	TokenPath       string `json:"token_path"       yaml:"token_path"`

	// Security settings
	EncryptTokens   bool   `json:"encrypt_tokens"   yaml:"encrypt_tokens"`
	TokenExpiration string `json:"token_expiration" yaml:"token_expiration"` // "30d"
}

type AppConfig struct {
	// Logging and output
	LogLevel    string `json:"log_level"    yaml:"log_level"` // "debug", "info", "warn", "error"
	LogFile     string `json:"log_file"     yaml:"log_file"`
	QuietMode   bool   `json:"quiet_mode"   yaml:"quiet_mode"`
	VerboseMode bool   `json:"verbose_mode" yaml:"verbose_mode"`

	// Backup and recovery
	CreateBackups bool   `json:"create_backups" yaml:"create_backups"`
	BackupDir     string `json:"backup_dir"     yaml:"backup_dir"`
	MaxBackups    int    `json:"max_backups"    yaml:"max_backups"`

	// Performance
	CacheEnabled bool          `json:"cache_enabled" yaml:"cache_enabled"`
	CacheDir     string        `json:"cache_dir"     yaml:"cache_dir"`
	CacheTTL     time.Duration `json:"cache_ttl"     yaml:"cache_ttl"`

	// Notifications
	NotifyOnSuccess bool `json:"notify_on_success" yaml:"notify_on_success"`
	NotifyOnError   bool `json:"notify_on_error"   yaml:"notify_on_error"`
}

// Future source configurations (placeholders for planned integrations)

type SlackSourceConfig struct {
	// Workspace settings
	WorkspaceURL string `json:"workspace_url" yaml:"workspace_url"` // e.g. "https://myworkspace.slack.com"
	APIUrl       string `json:"api_url"       yaml:"api_url"`       // optional Enterprise Grid override

	// Channel settings
	WorkspaceID    string   `json:"workspace_id"    yaml:"workspace_id"` // legacy field
	Channels       []string `json:"channels"        yaml:"channels"`     // ["general", "engineering"]
	IncludeThreads bool     `json:"include_threads" yaml:"include_threads"`
	IncludeDMs     bool     `json:"include_dms"     yaml:"include_dms"`
	MinImportance  string   `json:"min_importance"  yaml:"min_importance"` // "starred", "mentions", "all"

	// Thread settings: "individual", "consolidated", "summary"
	ThreadMode          string `json:"thread_mode"           yaml:"thread_mode"`
	ThreadSummaryLength int    `json:"thread_summary_length" yaml:"thread_summary_length"`

	// Content filtering
	ExcludeBots  bool     `json:"exclude_bots"  yaml:"exclude_bots"`
	MinLength    int      `json:"min_length"    yaml:"min_length"` // Minimum message length
	IncludeFiles bool     `json:"include_files" yaml:"include_files"`
	FileTypes    []string `json:"file_types"    yaml:"file_types"` // ["pdf", "doc", "img"]

	// Rate limiting and performance
	RateLimitMs           int `json:"rate_limit_ms"            yaml:"rate_limit_ms"`
	MaxMessagesPerChannel int `json:"max_messages_per_channel" yaml:"max_messages_per_channel"`
}

type GmailSourceConfig struct {
	// Instance identification
	Name        string `json:"name"        yaml:"name"`        // "Work Emails", "Personal Important"
	Description string `json:"description" yaml:"description"` // Optional description

	// Query and filtering
	// e.g., ["IMPORTANT", "STARRED"]
	Labels []string `json:"labels" yaml:"labels"`
	// Custom Gmail search query
	Query          string `json:"query"           yaml:"query"`
	IncludeUnread  bool   `json:"include_unread"  yaml:"include_unread"`
	IncludeRead    bool   `json:"include_read"    yaml:"include_read"`
	IncludeThreads bool   `json:"include_threads" yaml:"include_threads"`
	// "individual", "consolidated", "summary"
	ThreadMode string `json:"thread_mode,omitempty" yaml:"thread_mode,omitempty"`
	// Max messages in summary (default: 5)
	ThreadSummaryLength int `json:"thread_summary_length,omitempty" yaml:"thread_summary_length,omitempty"`
	// e.g., "30d", "1y"
	MaxEmailAge string `json:"max_email_age" yaml:"max_email_age"`
	// e.g., "1d" (exclude very recent)
	MinEmailAge string `json:"min_email_age,omitempty" yaml:"min_email_age,omitempty"`

	// Sender/recipient filtering (NEW)
	// e.g., ["company.com"]
	FromDomains []string `json:"from_domains,omitempty" yaml:"from_domains,omitempty"`
	// e.g., ["company.com"]
	ToDomains []string `json:"to_domains,omitempty" yaml:"to_domains,omitempty"`
	// e.g., ["noreply.com"]
	ExcludeFromDomains []string `json:"exclude_from_domains,omitempty" yaml:"exclude_from_domains,omitempty"`
	// Only include emails with attachments
	RequireAttachments bool `json:"require_attachments,omitempty" yaml:"require_attachments,omitempty"`

	// Content processing
	ExtractLinks        bool `json:"extract_links"                   yaml:"extract_links"`
	ExtractRecipients   bool `json:"extract_recipients"              yaml:"extract_recipients"`
	IncludeFullHeaders  bool `json:"include_full_headers"            yaml:"include_full_headers"`
	ProcessHTMLContent  bool `json:"process_html_content"            yaml:"process_html_content"`
	IncludeOriginalHTML bool `json:"include_original_html,omitempty" yaml:"include_original_html,omitempty"`
	StripQuotedText     bool `json:"strip_quoted_text,omitempty"     yaml:"strip_quoted_text,omitempty"`
	ExtractSignatures   bool `json:"extract_signatures,omitempty"    yaml:"extract_signatures,omitempty"`

	// Attachment handling
	DownloadAttachments bool `json:"download_attachments" yaml:"download_attachments"`
	// e.g., ["pdf", "doc", "jpg"]
	AttachmentTypes   []string `json:"attachment_types"    yaml:"attachment_types"`
	MaxAttachmentSize string   `json:"max_attachment_size" yaml:"max_attachment_size"`
	// Custom attachment folder
	AttachmentSubdir string `json:"attachment_subdir,omitempty" yaml:"attachment_subdir,omitempty"`

	// Rate limiting and performance
	RequestDelay time.Duration `json:"request_delay,omitempty" yaml:"request_delay,omitempty"` // Delay between requests
	MaxRequests  int           `json:"max_requests,omitempty"  yaml:"max_requests,omitempty"`  // Max requests per sync
	BatchSize    int           `json:"batch_size,omitempty"    yaml:"batch_size,omitempty"`    // Messages per API call

	// Output customization
	// e.g., "{{date}}-{{from}}-{{subject}}"
	FilenameTemplate     string        `json:"filename_template,omitempty"      yaml:"filename_template,omitempty"`
	IncludeThreadContext bool          `json:"include_thread_context,omitempty" yaml:"include_thread_context,omitempty"`
	GroupByThread        bool          `json:"group_by_thread,omitempty"        yaml:"group_by_thread,omitempty"`
	TaggingRules         []TaggingRule `json:"tagging_rules,omitempty"          yaml:"tagging_rules,omitempty"`
}

type TaggingRule struct {
	Condition string   `json:"condition" yaml:"condition"` // "from:boss@company.com"
	Tags      []string `json:"tags"      yaml:"tags"`      // ["urgent", "work"]
}

type JiraSourceConfig struct {
	// Instance and authentication
	InstanceURL string   `json:"instance_url" yaml:"instance_url"` // "https://company.atlassian.net"
	ProjectKeys []string `json:"project_keys" yaml:"project_keys"` // ["PROJ", "TEAM"]

	// Issue filtering
	JQL            string   `json:"jql"             yaml:"jql"`             // Custom JQL query
	IssueTypes     []string `json:"issue_types"     yaml:"issue_types"`     // ["Bug", "Story", "Task"]
	Statuses       []string `json:"statuses"        yaml:"statuses"`        // ["In Progress", "Done"]
	AssigneeFilter string   `json:"assignee_filter" yaml:"assignee_filter"` // "me", "team", "all"

	// Content inclusion
	IncludeComments    bool `json:"include_comments"    yaml:"include_comments"`
	IncludeHistory     bool `json:"include_history"     yaml:"include_history"`
	IncludeAttachments bool `json:"include_attachments" yaml:"include_attachments"`
}

// VectorDBConfig defines vector database configuration.
type VectorDBConfig struct {
	DBPath    string `json:"db_path"    yaml:"db_path"`    // Path to SQLite database file
	AutoIndex bool   `json:"auto_index" yaml:"auto_index"` // Auto-index on sync
}

// EmbeddingsConfig defines embeddings provider configuration.
type EmbeddingsConfig struct {
	Provider   string `json:"provider"   yaml:"provider"`   // "ollama" or "openai"
	Model      string `json:"model"      yaml:"model"`      // Model name
	APIURL     string `json:"api_url"    yaml:"api_url"`    // API base URL
	APIKey     string `json:"api_key"    yaml:"api_key"`    // API key (for OpenAI)
	Dimensions int    `json:"dimensions" yaml:"dimensions"` // Embedding dimensions
}

// ArchiveConfig defines configuration for the EML + SQLite email archive.
type ArchiveConfig struct {
	Enabled      bool   `json:"enabled"       yaml:"enabled"`
	EMLDir       string `json:"eml_dir"       yaml:"eml_dir"`       // Directory for raw .eml files
	DBPath       string `json:"db_path"       yaml:"db_path"`       // Path to SQLite archive database
	RequestDelay int    `json:"request_delay" yaml:"request_delay"` // ms between raw fetches
	MaxPerSync   int    `json:"max_per_sync"  yaml:"max_per_sync"`  // 0 = unlimited
}
