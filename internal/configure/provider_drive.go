package configure

import (
	"fmt"

	"pkm-sync/internal/sources/google/auth"
	"pkm-sync/internal/sources/google/drive"
	"pkm-sync/pkg/models"
)

// DriveProvider implements DiscoveryProvider for Google Drive.
type DriveProvider struct {
	svc *drive.Service
}

// SourceType implements DiscoveryProvider.
func (p *DriveProvider) SourceType() string { return sourceTypeDrive }

// Authenticate implements DiscoveryProvider. It obtains a Google OAuth client and
// initializes the Drive service client.
func (p *DriveProvider) Authenticate(_ *models.Config, _ string) error {
	httpClient, err := auth.GetClient()
	if err != nil {
		return fmt.Errorf(
			"failed to get Google OAuth client: %w\n\n"+
				"Run 'pkm-sync setup' to complete the OAuth flow",
			err,
		)
	}

	svc, err := drive.NewService(httpClient)
	if err != nil {
		return fmt.Errorf("failed to create Drive service: %w", err)
	}

	p.svc = svc

	return nil
}

// DiscoverySections implements DiscoveryProvider. It returns up to three sections:
//   - "Folders" — root-level Drive folders to sync
//   - "Workspace Types" — Google Workspace document types to include
//   - "Shared Drives" — shared drives (only shown when at least one exists)
func (p *DriveProvider) DiscoverySections(currentConfig models.SourceConfig) ([]DiscoverySection, error) {
	if p.svc == nil {
		return nil, fmt.Errorf("not authenticated — call Authenticate first")
	}

	// Build lookup set for currently-configured folder IDs.
	configuredFolders := make(map[string]bool, len(currentConfig.Drive.FolderIDs))
	for _, id := range currentConfig.Drive.FolderIDs {
		configuredFolders[id] = true
	}

	// Fetch root-level folders.
	folders, err := p.svc.ListFolders("")
	if err != nil {
		return nil, fmt.Errorf("failed to list Drive folders: %w", err)
	}

	folderOpts := make([]DiscoverableOption, 0, len(folders))
	for _, f := range folders {
		folderOpts = append(folderOpts, DiscoverableOption{
			ID:       f.ID,
			Name:     f.Name,
			Selected: configuredFolders[f.ID],
		})
	}

	// Build lookup set for currently-configured workspace types.
	configuredTypes := make(map[string]bool, len(currentConfig.Drive.WorkspaceTypes))
	for _, t := range currentConfig.Drive.WorkspaceTypes {
		configuredTypes[t] = true
	}

	workspaceTypeOpts := []DiscoverableOption{
		{ID: "document", Name: "Google Docs", Selected: configuredTypes["document"]},
		{ID: "spreadsheet", Name: "Google Sheets", Selected: configuredTypes["spreadsheet"]},
		{ID: "presentation", Name: "Google Slides", Selected: configuredTypes["presentation"]},
	}

	sections := []DiscoverySection{
		{
			Name:        "Folders",
			Description: "Select the Drive folders you want to sync (leave empty to use root)",
			Options:     folderOpts,
		},
		{
			Name:        "Workspace Types",
			Description: "Select the Google Workspace document types to include",
			Options:     workspaceTypeOpts,
		},
	}

	// Fetch shared drives — only add the section when drives exist.
	sharedDrives, err := p.svc.ListSharedDrives()
	if err != nil {
		// Non-fatal: log and skip the section.
		sharedDrives = nil
	}

	if len(sharedDrives) > 0 {
		sharedDriveOpts := make([]DiscoverableOption, 0, len(sharedDrives))
		for _, sd := range sharedDrives {
			sharedDriveOpts = append(sharedDriveOpts, DiscoverableOption{
				ID:       sd.ID,
				Name:     sd.Name,
				Selected: currentConfig.Drive.IncludeSharedDrives,
			})
		}

		sections = append(sections, DiscoverySection{
			Name:        "Shared Drives",
			Description: "Select shared drives to include in the sync",
			Options:     sharedDriveOpts,
		})
	}

	return sections, nil
}

// ApplySelections implements DiscoveryProvider. It updates the SourceConfig in-place.
func (p *DriveProvider) ApplySelections(cfg *models.SourceConfig, sectionName string, selectedIDs []string) {
	switch sectionName {
	case "Folders":
		cfg.Drive.FolderIDs = selectedIDs
	case "Workspace Types":
		cfg.Drive.WorkspaceTypes = selectedIDs
	case "Shared Drives":
		cfg.Drive.IncludeSharedDrives = len(selectedIDs) > 0
	}
}

// Preview implements DiscoveryProvider. It lists recent files in the given folder.
func (p *DriveProvider) Preview(folderID string, limit int) ([]string, error) {
	if p.svc == nil {
		return nil, fmt.Errorf("not authenticated")
	}

	files, err := p.svc.ListFiles(drive.ListFilesOptions{
		FolderID:   folderID,
		MaxResults: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list files in folder %s: %w", folderID, err)
	}

	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Name)
	}

	return names, nil
}

// RequiredFields implements DiscoveryProvider.
func (p *DriveProvider) RequiredFields() []RequiredField {
	return []RequiredField{
		{Key: "name", Prompt: "Drive source name", Placeholder: "My Drive"},
	}
}
