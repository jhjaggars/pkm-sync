package configure

import (
	"fmt"

	jirasource "pkm-sync/internal/sources/jira"
	"pkm-sync/pkg/models"
)

// JiraProvider implements DiscoveryProvider for Jira instances.
type JiraProvider struct {
	source *jirasource.JiraSource
}

// SourceType implements DiscoveryProvider.
func (p *JiraProvider) SourceType() string { return sourceTypeJira }

// Authenticate implements DiscoveryProvider. It initializes a JiraSource and calls
// Configure to load credentials from the jira-cli config or environment variables.
func (p *JiraProvider) Authenticate(cfg *models.Config, sourceID string) error {
	srcCfg, ok := cfg.Sources[sourceID]
	if !ok {
		srcCfg = models.SourceConfig{
			Type: "jira",
			Jira: models.JiraSourceConfig{},
		}
	}

	source := jirasource.NewJiraSource(sourceID, srcCfg)

	if err := source.Configure(nil, nil); err != nil {
		return fmt.Errorf(
			"failed to authenticate with Jira: %w\n\n"+
				"Set the JIRA_API_TOKEN environment variable or run 'jira init' to configure jira-cli",
			err,
		)
	}

	p.source = source

	return nil
}

// DiscoverySections implements DiscoveryProvider. It returns three sections:
//   - "Projects" — Jira projects accessible to the authenticated user
//   - "Issue Types" — issue types from the first configured project (if any)
//   - "Statuses" — statuses from the first configured project (if any)
func (p *JiraProvider) DiscoverySections(currentConfig models.SourceConfig) ([]DiscoverySection, error) {
	if p.source == nil {
		return nil, fmt.Errorf("not authenticated — call Authenticate first")
	}

	// Build lookup set for currently-configured project keys.
	configuredProjects := make(map[string]bool, len(currentConfig.Jira.ProjectKeys))
	for _, key := range currentConfig.Jira.ProjectKeys {
		configuredProjects[key] = true
	}

	// Fetch all projects.
	projects, err := p.source.ListProjects()
	if err != nil {
		return nil, fmt.Errorf("failed to list Jira projects: %w", err)
	}

	projectOpts := make([]DiscoverableOption, 0, len(projects))
	for _, proj := range projects {
		projectOpts = append(projectOpts, DiscoverableOption{
			ID:       proj.Key,
			Name:     fmt.Sprintf("[%s] %s", proj.Key, proj.Name),
			Selected: configuredProjects[proj.Key],
		})
	}

	sections := []DiscoverySection{
		{
			Name:        "Projects",
			Description: "Select the Jira projects you want to sync",
			Options:     projectOpts,
		},
	}

	// Fetch issue types and statuses from the first configured project, if any.
	// If no project is configured yet, fall back to the first available project.
	refProject := ""

	if len(currentConfig.Jira.ProjectKeys) > 0 {
		refProject = currentConfig.Jira.ProjectKeys[0]
	} else if len(projects) > 0 {
		refProject = projects[0].Key
	}

	if refProject != "" {
		sections = append(sections, p.issueTypeSection(refProject, currentConfig.Jira.IssueTypes))
		sections = append(sections, p.statusSection(refProject, currentConfig.Jira.Statuses))
	}

	return sections, nil
}

// issueTypeSection builds the "Issue Types" DiscoverySection for the given project.
func (p *JiraProvider) issueTypeSection(projectKey string, configured []string) DiscoverySection {
	configuredTypes := make(map[string]bool, len(configured))
	for _, t := range configured {
		configuredTypes[t] = true
	}

	issueTypes, err := p.source.ListIssueTypes(projectKey)
	if err != nil {
		// Non-fatal: return an empty section rather than failing the whole flow.
		return DiscoverySection{
			Name:        "Issue Types",
			Description: fmt.Sprintf("Could not load issue types for project %s: %v", projectKey, err),
			Options:     nil,
		}
	}

	opts := make([]DiscoverableOption, 0, len(issueTypes))
	for _, it := range issueTypes {
		opts = append(opts, DiscoverableOption{
			ID:       it.Name,
			Name:     it.Name,
			Selected: configuredTypes[it.Name],
		})
	}

	return DiscoverySection{
		Name:        "Issue Types",
		Description: fmt.Sprintf("Select issue types to include (from project %s; leave empty for all)", projectKey),
		Options:     opts,
	}
}

// statusSection builds the "Statuses" DiscoverySection for the given project.
func (p *JiraProvider) statusSection(projectKey string, configured []string) DiscoverySection {
	configuredStatuses := make(map[string]bool, len(configured))
	for _, s := range configured {
		configuredStatuses[s] = true
	}

	statuses, err := p.source.ListStatuses(projectKey)
	if err != nil {
		return DiscoverySection{
			Name:        "Statuses",
			Description: fmt.Sprintf("Could not load statuses for project %s: %v", projectKey, err),
			Options:     nil,
		}
	}

	opts := make([]DiscoverableOption, 0, len(statuses))
	for _, st := range statuses {
		opts = append(opts, DiscoverableOption{
			ID:       st.Name,
			Name:     st.Name,
			Selected: configuredStatuses[st.Name],
		})
	}

	return DiscoverySection{
		Name:        "Statuses",
		Description: fmt.Sprintf("Select statuses to include (from project %s; leave empty for all)", projectKey),
		Options:     opts,
	}
}

// ApplySelections implements DiscoveryProvider. It updates the SourceConfig in-place.
func (p *JiraProvider) ApplySelections(cfg *models.SourceConfig, sectionName string, selectedIDs []string) {
	switch sectionName {
	case "Projects":
		cfg.Jira.ProjectKeys = selectedIDs
	case "Issue Types":
		cfg.Jira.IssueTypes = selectedIDs
	case "Statuses":
		cfg.Jira.Statuses = selectedIDs
	}
}

// Preview implements DiscoveryProvider. Jira issue previews are not supported
// because they require running a time-bounded JQL query which is not useful
// without knowing the user's intent. Returns nil without error.
func (p *JiraProvider) Preview(_ string, _ int) ([]string, error) {
	return nil, nil
}

// RequiredFields implements DiscoveryProvider.
func (p *JiraProvider) RequiredFields() []RequiredField {
	return []RequiredField{
		{
			Key:         "instance_url",
			Prompt:      "Jira instance URL",
			Placeholder: "https://issues.example.com",
			Validate: func(s string) error {
				if s == "" {
					return fmt.Errorf("instance URL is required")
				}

				return nil
			},
		},
	}
}
