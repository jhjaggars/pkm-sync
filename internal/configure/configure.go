// Package configure provides an interactive TUI for discovering and selecting
// what to sync from each source. It uses the huh library for terminal forms.
package configure

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"

	"pkm-sync/internal/config"
	"pkm-sync/pkg/models"

	"github.com/charmbracelet/huh"
	"golang.org/x/term"
)

// DiscoverableOption represents a single selectable item (channel, label, folder, project).
type DiscoverableOption struct {
	ID          string // channel ID, folder ID, label ID/name, project key
	Name        string // human-readable display name
	Description string // extra context shown in TUI (e.g., recent item previews)
	Selected    bool   // currently in config
}

// DiscoverySection groups related options for display in the TUI.
type DiscoverySection struct {
	Name        string // e.g., "Channels", "Labels", "Folders"
	Description string // prompt text shown above the multi-select
	Options     []DiscoverableOption
}

// RequiredField describes a field needed when creating a new source instance.
type RequiredField struct {
	Key         string // config field name (e.g., "workspace_url")
	Prompt      string // user-facing prompt
	Placeholder string // example value shown as placeholder
	Validate    func(string) error
}

// DiscoveryProvider is the interface each source type implements to support
// the interactive configure flow.
type DiscoveryProvider interface {
	// SourceType returns the source type string (e.g., "slack", "gmail").
	SourceType() string
	// Authenticate connects to the source API using existing credentials.
	Authenticate(cfg *models.Config, sourceID string) error
	// DiscoverySections returns selectable option groups populated from the live API.
	DiscoverySections(currentConfig models.SourceConfig) ([]DiscoverySection, error)
	// ApplySelections updates the SourceConfig in-place with the user's selections.
	ApplySelections(cfg *models.SourceConfig, sectionName string, selectedIDs []string)
	// Preview returns recent item titles/subjects for the given option ID.
	Preview(optionID string, limit int) ([]string, error)
	// RequiredFields returns fields to prompt for when creating a new source.
	RequiredFields() []RequiredField
}

// Source type constants.
const (
	sourceTypeSlack    = "slack"
	sourceTypeGmail    = "gmail"
	sourceTypeDrive    = "google_drive"
	sourceTypeJira     = "jira"
	sourceTypeCalendar = "google_calendar"

	// newSourceSentinel is a special value returned by promptSourceChoice to signal
	// that the user wants to create a new source rather than edit an existing one.
	newSourceSentinel = "::new::"
)

// getProvider returns the DiscoveryProvider for the given source type.
func getProvider(sourceType string) (DiscoveryProvider, error) {
	switch sourceType {
	case sourceTypeSlack:
		return &SlackProvider{}, nil
	case sourceTypeGmail:
		return &GmailProvider{}, nil
	case sourceTypeDrive:
		return &DriveProvider{}, nil
	case sourceTypeJira:
		return &JiraProvider{}, nil
	case sourceTypeCalendar:
		return &CalendarProvider{}, nil
	default:
		return nil, fmt.Errorf("unsupported source type: %s", sourceType)
	}
}

// supportedSourceTypes lists known source types for new-source creation.
var supportedSourceTypes = []string{
	sourceTypeSlack,
	sourceTypeGmail,
	sourceTypeDrive,
	sourceTypeJira,
	sourceTypeCalendar,
}

// RunConfigure is the main entry point for the interactive configuration TUI.
// sourceID identifies an existing source to configure; sourceType is used when
// creating a new source. Either or both may be empty, in which case the user is
// prompted to choose.
func RunConfigure(cfg *models.Config, sourceID string, sourceType string) error {
	// Require an interactive terminal.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("pkm-sync configure requires an interactive terminal (TTY). " +
			"Run this command directly in your shell, not piped or in a script")
	}

	if cfg.Sources == nil {
		cfg.Sources = make(map[string]models.SourceConfig)
	}

	// Phase 1: Determine which source to configure.
	if sourceID == "" && sourceType == "" {
		var err error

		sourceID, sourceType, err = promptSourceChoice(cfg)
		if err != nil {
			return err
		}

		// User aborted the source picker — exit cleanly.
		if sourceID == "" && sourceType == "" {
			return nil
		}
	}

	// If the user chose to create a new source and no type was given yet, ask for type.
	if sourceID == newSourceSentinel {
		sourceID = ""

		resolved, err := resolveNewSourceType(sourceType)
		if err != nil {
			return err
		}

		// resolveNewSourceType returns "" when the user aborts.
		if resolved == "" {
			return nil
		}

		sourceType = resolved
	}

	// Phase 2: If creating a new source, collect required fields and build SourceConfig.
	isNew := sourceID == ""
	if isNew {
		var err error

		sourceID, err = promptNewSource(cfg, sourceType)
		if err != nil {
			return err
		}
	}

	// Phase 3: Get provider and authenticate.
	if sourceType == "" {
		src, ok := cfg.Sources[sourceID]
		if !ok {
			return fmt.Errorf("source %q not found in configuration", sourceID)
		}

		sourceType = src.Type
	}

	provider, err := getProvider(sourceType)
	if err != nil {
		return err
	}

	fmt.Printf("Connecting to %s source %q...\n", sourceType, sourceID)

	if err := provider.Authenticate(cfg, sourceID); err != nil {
		return fmt.Errorf("authentication failed: %w\n\nTo fix this, run 'pkm-sync setup' to verify your credentials", err)
	}

	// Phase 4: Discover available options.
	fmt.Println("Discovering available options...")

	currentSrc := cfg.Sources[sourceID]

	sections, err := provider.DiscoverySections(currentSrc)
	if err != nil {
		return fmt.Errorf("failed to discover options: %w", err)
	}

	if len(sections) == 0 {
		fmt.Println("No configurable options found for this source.")

		return nil
	}

	// Phase 5: Build and run huh multi-select forms for each section.
	// selectedPtrs holds a pointer per section so huh can write results directly.
	selectedPtrs := make([]*[]string, len(sections))
	formGroups := make([]*huh.Group, 0, len(sections))

	for i := range sections {
		section := &sections[i]

		// Build huh options from DiscoverableOption slice.
		opts := make([]huh.Option[string], 0, len(section.Options))
		for _, opt := range section.Options {
			label := opt.Name
			if opt.Description != "" {
				label = fmt.Sprintf("%s — %s", opt.Name, TruncateString(opt.Description, 60))
			}

			o := huh.NewOption(label, opt.ID)
			if opt.Selected {
				o = o.Selected(true)
			}

			opts = append(opts, o)
		}

		// Allocate an addressable slice for huh to write into.
		selected := make([]string, 0)
		selectedPtrs[i] = &selected

		multi := huh.NewMultiSelect[string]().
			Title(section.Name).
			Description(section.Description).
			Options(opts...).
			Value(selectedPtrs[i])

		formGroups = append(formGroups, huh.NewGroup(multi))
	}

	form := huh.NewForm(formGroups...)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Println("Canceled.")

			return nil
		}

		return fmt.Errorf("form error: %w", err)
	}

	// Phase 6: Apply selections and show diff.
	updatedSrc := cfg.Sources[sourceID]

	for i, section := range sections {
		before := getSectionIDs(section)
		selected := *selectedPtrs[i]
		provider.ApplySelections(&updatedSrc, section.Name, selected)
		diff := FormatDiff(section.Name, before, selected)
		fmt.Println(diff)
	}

	// Phase 7: Confirm save.
	var confirmed bool

	confirmForm := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Save changes?").
				Description("This will update your config.yaml").
				Value(&confirmed),
		),
	)

	if err := confirmForm.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Println("Canceled.")

			return nil
		}

		return fmt.Errorf("confirm form error: %w", err)
	}

	if !confirmed {
		fmt.Println("Changes discarded.")

		return nil
	}

	// Phase 8: Save configuration.
	cfg.Sources[sourceID] = updatedSrc

	// Add to EnabledSources if not already present and source is enabled.
	if updatedSrc.Enabled && !slices.Contains(cfg.Sync.EnabledSources, sourceID) {
		cfg.Sync.EnabledSources = append(cfg.Sync.EnabledSources, sourceID)
	}

	if err := config.SaveConfig(cfg); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	fmt.Printf("Configuration saved for source %q.\n", sourceID)

	return nil
}

// promptSourceChoice asks the user to pick an existing source or create a new one.
// Returns (sourceID, sourceType, error). If the user chooses "Create new source",
// sourceID will be "::new::" and sourceType will be empty.
func promptSourceChoice(cfg *models.Config) (string, string, error) {
	// Build list of existing sources.
	sourceNames := make([]string, 0, len(cfg.Sources))
	for name := range cfg.Sources {
		sourceNames = append(sourceNames, name)
	}

	sort.Strings(sourceNames)

	opts := make([]huh.Option[string], 0, len(sourceNames)+1)
	for _, name := range sourceNames {
		src := cfg.Sources[name]
		label := fmt.Sprintf("%s (%s)", name, src.Type)
		opts = append(opts, huh.NewOption(label, name))
	}

	opts = append(opts, huh.NewOption("Create new source", newSourceSentinel))

	var choice string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Which source would you like to configure?").
				Options(opts...).
				Value(&choice),
		),
	)

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Println("Canceled.")

			return "", "", nil
		}

		return "", "", fmt.Errorf("source selection error: %w", err)
	}

	if choice == newSourceSentinel {
		return newSourceSentinel, "", nil
	}

	return choice, cfg.Sources[choice].Type, nil
}

// promptSourceType asks the user to pick the type for a new source.
func promptSourceType() (string, error) {
	opts := make([]huh.Option[string], 0, len(supportedSourceTypes))
	for _, t := range supportedSourceTypes {
		opts = append(opts, huh.NewOption(t, t))
	}

	var chosen string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("What type of source would you like to create?").
				Options(opts...).
				Value(&chosen),
		),
	)

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Println("Canceled.")

			return "", nil
		}

		return "", fmt.Errorf("source type selection error: %w", err)
	}

	return chosen, nil
}

// resolveNewSourceType returns the source type to use when creating a new source.
// If sourceType is already known (from --type flag), it is returned as-is.
// Otherwise the user is prompted. Returns "" if the user aborts.
func resolveNewSourceType(sourceType string) (string, error) {
	if sourceType != "" {
		return sourceType, nil
	}

	return promptSourceType()
}

// promptNewSource collects required field values for a new source, builds the SourceConfig,
// and inserts it into cfg.Sources. Returns the new sourceID (name).
func promptNewSource(cfg *models.Config, sourceType string) (string, error) {
	provider, err := getProvider(sourceType)
	if err != nil {
		return "", err
	}

	fields := provider.RequiredFields()

	// Always collect a source name/ID.
	var sourceID string

	idField := huh.NewInput().
		Title("Source name (config key)").
		Description("A unique identifier for this source, e.g. slack_work").
		Placeholder("my_" + sourceType).
		Validate(func(s string) error {
			if s == "" {
				return fmt.Errorf("source name is required")
			}

			if _, exists := cfg.Sources[s]; exists {
				return fmt.Errorf("source %q already exists", s)
			}

			return nil
		}).
		Value(&sourceID)

	// Build input fields for each RequiredField.
	// fieldPtrs maps key → addressable string so huh can write into them.
	fieldPtrs := make(map[string]*string, len(fields))
	inputFields := make([]huh.Field, 0, len(fields)+1)
	inputFields = append(inputFields, idField)

	for i := range fields {
		f := &fields[i]
		val := new(string)
		fieldPtrs[f.Key] = val

		inp := huh.NewInput().
			Title(f.Prompt).
			Placeholder(f.Placeholder).
			Value(val)

		if f.Validate != nil {
			validate := f.Validate
			inp = inp.Validate(validate)
		}

		inputFields = append(inputFields, inp)
	}

	form := huh.NewForm(huh.NewGroup(inputFields...))
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Println("Canceled.")

			return "", fmt.Errorf("creation canceled")
		}

		return "", fmt.Errorf("new source form error: %w", err)
	}

	// Collect field values from the pointers.
	fieldValues := make(map[string]string, len(fields))
	for key, ptr := range fieldPtrs {
		fieldValues[key] = *ptr
	}

	// Build the SourceConfig from collected values.
	newSrc := models.SourceConfig{
		Type:    sourceType,
		Enabled: true,
	}

	applyFieldValues(&newSrc, sourceType, fieldValues)

	cfg.Sources[sourceID] = newSrc

	return sourceID, nil
}

// applyFieldValues applies the RequiredField values into a SourceConfig based on source type.
func applyFieldValues(src *models.SourceConfig, sourceType string, values map[string]string) {
	switch sourceType {
	case "slack":
		src.Slack.WorkspaceURL = values["workspace_url"]
	case "gmail":
		src.Gmail.Name = values["name"]
	case "google_drive":
		src.Drive.Name = values["name"]
	case "google_calendar":
		calID := values["calendar_id"]
		if calID == "" {
			calID = "primary"
		}

		src.Google.CalendarID = calID
	case "jira":
		src.Jira.InstanceURL = values["instance_url"]
	}
}

// getSectionIDs returns the IDs of all currently-selected options in a section
// (i.e., those marked Selected=true). Used to compute diffs before/after user edits.
func getSectionIDs(section DiscoverySection) []string {
	var ids []string

	for _, opt := range section.Options {
		if opt.Selected {
			ids = append(ids, opt.ID)
		}
	}

	return ids
}
