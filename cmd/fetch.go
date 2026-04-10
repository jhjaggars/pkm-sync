package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"pkm-sync/internal/config"
	"pkm-sync/internal/resolve"
	"pkm-sync/internal/sinks"
	"pkm-sync/internal/sources/google/auth"
	"pkm-sync/internal/sources/google/drive"
	jirasource "pkm-sync/internal/sources/jira"
	"pkm-sync/internal/utils"
	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
	"pkm-sync/pkg/routing"

	"github.com/spf13/cobra"
)

var (
	fetchCmdSource   string
	fetchCmdFormat   string
	fetchCmdOutput   string
	fetchCmdComments bool
)

var fetchCmd = &cobra.Command{
	Use:   "fetch <url-or-identifier>",
	Short: "Fetch a single item by URL or source-qualified identifier",
	Long: `Fetch a single item by URL or identifier and output its content.

Identifier formats:

  URL (auto-routed):
    pkm-sync fetch https://docs.google.com/document/d/ID/edit
    pkm-sync fetch https://company.atlassian.net/browse/PROJ-123

  Source-type prefix:
    pkm-sync fetch jira/PROJ-123
    pkm-sync fetch drive/FILE_ID
    pkm-sync fetch drive/https://docs.google.com/document/d/ID/edit

  When multiple sources of the same type exist, use --source to disambiguate:
    pkm-sync fetch jira/PROJ-123 --source jira_work

By default content is written to stdout. Use --output to write a markdown file
with YAML frontmatter (enables re-fetch later).

Output formats:
  txt  : Plain text (default for stdout)
  md   : Markdown (default for --output)
  json : Raw JSON item representation

Examples:
  pkm-sync fetch "https://docs.google.com/document/d/abc123/edit"
  pkm-sync fetch "https://docs.google.com/document/d/abc123/edit" --format md --comments
  pkm-sync fetch "https://docs.google.com/document/d/abc123/edit" --output ./docs/
  pkm-sync fetch jira/PROJ-123
  pkm-sync fetch jira/PROJ-123 --source jira_work --output ./jira/`,
	Args: cobra.ExactArgs(1),
	RunE: runFetchCommand,
}

func init() {
	rootCmd.AddCommand(fetchCmd)
	fetchCmd.Flags().StringVar(&fetchCmdSource, "source", "", "Source name when multiple sources of the same type are configured")
	fetchCmd.Flags().StringVar(&fetchCmdFormat, "format", "", "Output format (txt, md, json). Defaults to md with --output, txt otherwise")
	fetchCmd.Flags().StringVarP(&fetchCmdOutput, "output", "o", "", "Write to file/directory with frontmatter")
	fetchCmd.Flags().BoolVar(&fetchCmdComments, "comments", false, "Append document comments as markdown footnotes (Drive documents)")
}

func runFetchCommand(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	id := routing.Parse(args[0])

	// Case 1 & 2: the identifier is (or contains) a URL — route through resolvers.
	if id.IsURL {
		return runFetchByURL(ctx, id)
	}

	// Case 3: source-type/key — use the Fetcher interface on a configured source.
	if id.SourceType != "" {
		return runFetchByKey(ctx, id)
	}

	// Case 4: bare key — need --source to identify the source.
	if fetchCmdSource == "" {
		return fmt.Errorf("identifier %q is ambiguous: use source-type prefix (e.g. jira/%s) or --source flag", args[0], args[0])
	}

	return runFetchBySourceName(ctx, id.Key, fetchCmdSource)
}

// runFetchByURL uses the resolve.Engine to fetch an item by URL.
// When fetchCmdSource is set, only resolvers matching that source name are used.
func runFetchByURL(ctx context.Context, id routing.ParsedIdentifier) error {
	resolvers, err := buildResolvers(fetchCmdSource)
	if err != nil {
		return err
	}

	if len(resolvers) == 0 {
		return fmt.Errorf("no resolvers available for URL %q (check authentication)", id.URL)
	}

	engine := resolve.NewEngine(resolvers)

	item, err := engine.ResolveURL(ctx, id.URL)
	if err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}

	if item == nil {
		return fmt.Errorf("no resolver matched URL %q", id.URL)
	}

	// Append Drive comments when requested.
	if fetchCmdComments {
		item, err = appendDriveComments(item, id.URL)
		if err != nil {
			return err
		}
	}

	return outputFetchedItem(item, id.URL)
}

// runFetchByKey looks up a configured source by type prefix and calls FetchOne.
func runFetchByKey(ctx context.Context, id routing.ParsedIdentifier) error {
	canonicalType := routing.CanonicalSourceType(id.SourceType)

	// For Drive with a file ID, construct a URL and route through the resolver.
	// This avoids duplicating the export logic that already lives in DriveResolver.
	if canonicalType == "google_drive" && !strings.HasPrefix(id.Key, "http") {
		driveURL := "https://drive.google.com/file/d/" + id.Key + "/view"

		return runFetchByURL(ctx, routing.ParsedIdentifier{
			Raw:        id.Raw,
			IsURL:      true,
			URL:        driveURL,
			SourceType: id.SourceType,
			Key:        id.Key,
		})
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	srcName, sc, err := findSourceByType(cfg, canonicalType, fetchCmdSource)
	if err != nil {
		return fmt.Errorf("cannot fetch %s/%s: %w", id.SourceType, id.Key, err)
	}

	src, err := createSourceWithConfig(srcName, sc, nil)
	if err != nil {
		return fmt.Errorf("failed to create source %q: %w", srcName, err)
	}

	fetcher, ok := src.(interfaces.Fetcher)
	if !ok {
		return fmt.Errorf("source type %q does not support single-item fetch", id.SourceType)
	}

	item, err := fetcher.FetchOne(ctx, id.Key)
	if err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}

	if item == nil {
		return fmt.Errorf("item %q not found in source %q", id.Key, srcName)
	}

	return outputFetchedItem(item, "")
}

// runFetchBySourceName looks up a source by explicit name and calls FetchOne.
func runFetchBySourceName(ctx context.Context, key, sourceName string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	sc, ok := cfg.Sources[sourceName]
	if !ok {
		return fmt.Errorf("source %q not found in config", sourceName)
	}

	src, err := createSourceWithConfig(sourceName, sc, nil)
	if err != nil {
		return fmt.Errorf("failed to create source %q: %w", sourceName, err)
	}

	fetcher, fok := src.(interfaces.Fetcher)
	if !fok {
		return fmt.Errorf("source %q (type %q) does not support single-item fetch", sourceName, sc.Type)
	}

	item, err := fetcher.FetchOne(ctx, key)
	if err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}

	if item == nil {
		return fmt.Errorf("item %q not found in source %q", key, sourceName)
	}

	return outputFetchedItem(item, "")
}

// buildResolvers constructs a resolver slice from available auth and config.
// sourceName optionally restricts to a specific configured source (by name).
// Drive resolution only requires Google OAuth. Jira resolution additionally
// requires a configured jira source (for the instance URL).
func buildResolvers(sourceName string) ([]interfaces.Resolver, error) {
	var resolvers []interfaces.Resolver

	// Drive resolver — requires only Google OAuth. Skip when --source names a
	// non-drive source explicitly.
	addDrive := sourceName == "" || isSourceNameOfType(sourceName, "google_drive")
	if addDrive {
		client, authErr := auth.GetClient()
		if authErr == nil {
			svc, svcErr := drive.NewService(client)
			if svcErr == nil {
				resolvers = append(resolvers, resolve.NewDriveResolver(svc, models.DriveSourceConfig{}))
			}
		} else if sourceName != "" {
			// Explicit drive source requested but auth failed — surface the error.
			return nil, fmt.Errorf("authentication required: %w", authErr)
		}
	}

	// Jira resolvers — one per configured jira source. When sourceName is set,
	// only build the resolver for that specific source.
	if cfg, err := config.LoadConfig(); err == nil {
		resolvers = buildJiraResolvers(resolvers, cfg.Sources, sourceName)
	}

	if len(resolvers) == 0 {
		return nil, fmt.Errorf("no resolvers available (check authentication and source configuration)")
	}

	return resolvers, nil
}

// isSourceNameOfType returns true when the named source has the given canonical
// type, or when sourceName is empty (no restriction).
func isSourceNameOfType(sourceName, canonicalType string) bool {
	cfg, err := config.LoadConfig()
	if err != nil {
		return false
	}

	sc, ok := cfg.Sources[sourceName]

	return ok && sc.Type == canonicalType
}

// appendDriveComments fetches Drive document comments and appends them as
// markdown footnotes when --comments is set. Only applies to Drive items;
// other source types are returned unchanged.
func appendDriveComments(item models.FullItem, sourceURL string) (models.FullItem, error) {
	if item.GetSourceType() != "google_drive" {
		return item, nil
	}

	fileID, err := drive.ExtractFileID(sourceURL)
	if err != nil {
		return item, nil // not a Drive URL we can extract from — skip silently
	}

	client, err := auth.GetClient()
	if err != nil {
		return nil, fmt.Errorf("authentication required for --comments: %w", err)
	}

	svc, err := drive.NewService(client)
	if err != nil {
		return nil, fmt.Errorf("failed to create drive service: %w", err)
	}

	comments, err := svc.GetComments(fileID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch comments: %w", err)
	}

	if len(comments) == 0 {
		return item, nil
	}

	content := drive.InsertCommentMarkers(item.GetContent(), comments)
	content += "\n\n" + drive.FormatCommentsAsFootnotes(comments)
	item.SetContent(content)

	return item, nil
}

// outputFetchedItem writes item content to stdout or to a file per CLI flags.
// sourceURL is the original URL (used for frontmatter when writing Drive docs).
func outputFetchedItem(item models.FullItem, sourceURL string) error {
	format := fetchCmdFormat
	if format == "" {
		if fetchCmdOutput != "" {
			format = "md"
		} else {
			format = "txt"
		}
	}

	if format == "json" {
		data, err := item.MarshalJSON()
		if err != nil {
			return fmt.Errorf("failed to serialize item: %w", err)
		}

		if fetchCmdOutput != "" {
			outPath := resolveItemOutputPath(fetchCmdOutput, item.GetTitle(), "json")

			if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

			return os.WriteFile(outPath, data, 0644)
		}

		_, err = os.Stdout.Write(data)

		return err
	}

	content := item.GetContent()

	if fetchCmdOutput != "" {
		return writeFetchedItemToFile(item, sourceURL, content, fetchCmdOutput)
	}

	_, err := fmt.Fprint(os.Stdout, content)

	return err
}

// writeFetchedItemToFile writes a fetched item to a markdown file with YAML
// frontmatter. For Drive documents the source URL is embedded so that
// 'pkm-sync drive refresh' can re-fetch later.
func writeFetchedItemToFile(item models.FullItem, sourceURL, content, outputFlag string) error {
	outPath := resolveItemOutputPath(outputFlag, item.GetTitle(), "md")

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Build a BasicItem so we get consistent frontmatter via the Obsidian formatter.
	meta := item.GetMetadata()
	if meta == nil {
		meta = make(map[string]any)
	}

	if sourceURL != "" {
		meta["source_url"] = sourceURL
	}

	bi := &models.BasicItem{
		ID:         item.GetID(),
		Title:      item.GetTitle(),
		SourceType: item.GetSourceType(),
		ItemType:   item.GetItemType(),
		Content:    content,
		CreatedAt:  item.GetCreatedAt(),
		UpdatedAt:  item.GetUpdatedAt(),
		Tags:       item.GetTags(),
		Metadata:   meta,
	}

	formatter := sinks.NewObsidianFormatterPublic()
	formatted := formatter.FormatItemContent(bi)

	if err := os.WriteFile(outPath, []byte(formatted), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", outPath, err)
	}

	fmt.Fprintf(os.Stderr, "Wrote %s\n", outPath)

	return nil
}

// buildJiraResolvers appends a JiraResolver for each enabled jira source in
// sources. When sourceName is non-empty only that source is considered.
func buildJiraResolvers(resolvers []interfaces.Resolver, sources map[string]models.SourceConfig, sourceName string) []interfaces.Resolver {
	for name, sc := range sources {
		if !sc.Enabled || sc.Type != "jira" {
			continue
		}

		if sourceName != "" && name != sourceName {
			continue
		}

		if sc.Jira.InstanceURL == "" {
			continue
		}

		jiraSrc := jirasource.NewJiraSource(name, sc)
		if err := jiraSrc.Configure(nil, nil); err != nil {
			continue
		}

		jr, err := resolve.NewJiraResolver(jiraSrc, sc.Jira.InstanceURL)
		if err != nil {
			continue
		}

		resolvers = append(resolvers, jr)
	}

	return resolvers
}

// resolveItemOutputPath determines the output path for a fetched item.
// If outputFlag is an existing directory or ends with /, the title is used as filename.
func resolveItemOutputPath(outputFlag, title, format string) string {
	ext := "." + format

	info, err := os.Stat(outputFlag)
	if (err == nil && info.IsDir()) || strings.HasSuffix(outputFlag, "/") {
		safe := utils.SanitizeFilename(title)

		return filepath.Join(outputFlag, safe+ext)
	}

	return outputFlag
}
