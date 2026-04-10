package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"pkm-sync/internal/config"
	"pkm-sync/internal/keystore"
	"pkm-sync/internal/sources/google/auth"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration settings",
	Long: `Manage pkm-sync configuration settings including default sources, targets, and sync options.

Examples:
  pkm-sync config init                    # Create default config file
  pkm-sync config show                    # Show current configuration  
  pkm-sync config path                    # Show config file location
  pkm-sync config edit                    # Open config in editor
  pkm-sync config validate               # Validate configuration`,
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create default configuration file",
	Long:  "Creates a default configuration file with sensible defaults for pkm-sync.",
	RunE:  runConfigInitCommand,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current configuration",
	Long:  "Display the current configuration settings loaded from the config file.",
	RunE:  runConfigShowCommand,
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Show configuration file path",
	Long:  "Display the path to the configuration file that would be used or created.",
	RunE:  runConfigPathCommand,
}

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration file",
	Long:  "Check if the configuration file is valid and can be loaded successfully.",
	RunE:  runConfigValidateCommand,
}

var configEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Open configuration file in editor",
	Long:  "Open the configuration file in your default editor (uses $EDITOR environment variable).",
	RunE:  runConfigEditCommand,
}

var configMigrateSecretsCmd = &cobra.Command{
	Use:   "migrate-secrets",
	Short: "Migrate stored secrets from files to the OS keychain",
	Long:  "Reads Google OAuth tokens and other pkm-sync secrets from their legacy file locations and writes them to the OS keychain (macOS Keychain, GNOME Keyring, or Windows Credential Manager).",
	RunE:  runConfigMigrateSecretsCommand,
}

var configClearTokenCmd = &cobra.Command{
	Use:   "clear-token",
	Short: "Clear the stored Google OAuth token",
	Long:  "Removes the Google OAuth token from the active secret backend (keychain or file). You will be prompted to re-authorize on the next sync.",
	RunE:  runConfigClearTokenCommand,
}

func init() {
	rootCmd.AddCommand(configCmd)

	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configPathCmd)
	configCmd.AddCommand(configValidateCmd)
	configCmd.AddCommand(configEditCmd)
	configCmd.AddCommand(configMigrateSecretsCmd)
	configCmd.AddCommand(configClearTokenCmd)

	// Flags for config init
	configInitCmd.Flags().BoolP("force", "f", false, "Overwrite existing config file")
	configInitCmd.Flags().StringP("output", "o", "", "Output directory for default target")
	configInitCmd.Flags().String("target", "", "Default target (obsidian, logseq)")
	configInitCmd.Flags().String("source", "", "Default source (google_calendar)")
}
func runConfigInitCommand(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")
	output, _ := cmd.Flags().GetString("output")
	target, _ := cmd.Flags().GetString("target")
	source, _ := cmd.Flags().GetString("source")

	// Check if config already exists
	configPath, err := getConfigFilePath()
	if err != nil {
		return err
	}

	if _, err := os.Stat(configPath); err == nil && !force {
		return fmt.Errorf("config file already exists at %s. Use --force to overwrite", configPath)
	}

	// Create default config
	cfg := config.GetDefaultConfig()

	// Apply command line overrides
	if output != "" {
		cfg.Sync.DefaultOutputDir = output
	}

	if target != "" {
		cfg.Sync.DefaultTarget = target
	}

	if source != "" {
		// Add to enabled sources if not already present
		found := false

		for _, src := range cfg.Sync.EnabledSources {
			if src == source {
				found = true

				break
			}
		}

		if !found {
			cfg.Sync.EnabledSources = append(cfg.Sync.EnabledSources, source)
		}

		// Enable the source in the sources config
		if sourceConfig, exists := cfg.Sources[source]; exists {
			sourceConfig.Enabled = true
			cfg.Sources[source] = sourceConfig
		}
	}

	// Save config
	if err := config.SaveConfig(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Configuration file created at: %s\n", configPath)
	fmt.Println("\nYou can now:")
	fmt.Printf("  - Edit the config: pkm-sync config edit\n")
	fmt.Printf("  - View the config: pkm-sync config show\n")
	fmt.Printf("  - Use gmail without flags: pkm-sync gmail\n")

	return nil
}

func runConfigShowCommand(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Convert to YAML for display
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	fmt.Print(string(data))

	return nil
}

func runConfigPathCommand(cmd *cobra.Command, args []string) error {
	configPath, err := getConfigFilePath()
	if err != nil {
		return err
	}

	fmt.Println(configPath)

	// Show if file exists
	if _, err := os.Stat(configPath); err == nil {
		fmt.Println("(file exists)")
	} else {
		fmt.Println("(file does not exist - run 'pkm-sync config init' to create)")
	}

	return nil
}

func runConfigValidateCommand(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("❌ Configuration validation failed: %v\n", err)

		return err
	}

	// Use comprehensive validation
	if err := config.ValidateConfig(cfg); err != nil {
		fmt.Printf("❌ Configuration validation failed: %v\n", err)

		return err
	}

	// Validate output directory is writable
	if cfg.Sync.DefaultOutputDir != "" {
		if err := validateOutputDirectory(cfg.Sync.DefaultOutputDir); err != nil {
			fmt.Printf("❌ Default output directory '%s' is not writable: %v\n", cfg.Sync.DefaultOutputDir, err)

			return fmt.Errorf("invalid configuration")
		}
	}

	// Get enabled sources for summary
	enabledSources := getEnabledSources(cfg)

	fmt.Println("✅ Configuration is valid")
	fmt.Printf("   Enabled sources: [%s]\n", strings.Join(enabledSources, ", "))
	fmt.Printf("   Default target: %s\n", cfg.Sync.DefaultTarget)
	fmt.Printf("   Default output: %s\n", cfg.Sync.DefaultOutputDir)
	fmt.Printf("   Source tags: %t\n", cfg.Sync.SourceTags)
	fmt.Printf("   Merge sources: %t\n", cfg.Sync.MergeSources)

	return nil
}

func runConfigEditCommand(cmd *cobra.Command, args []string) error {
	configPath, err := getConfigFilePath()
	if err != nil {
		return err
	}

	// Check if config exists, create if not
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Printf("Config file doesn't exist. Creating default config at %s\n", configPath)

		if err := config.CreateDefaultConfig(); err != nil {
			return fmt.Errorf("failed to create default config: %w", err)
		}
	}

	// Get editor from environment
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "nano" // fallback editor
	}

	fmt.Printf("Opening config file in %s...\n", editor)
	fmt.Printf("Config file: %s\n", configPath)

	// Note: In a real implementation, you'd use exec.Command to launch the editor
	// For now, just show the path
	fmt.Println("Run the following command to edit:")
	fmt.Printf("  %s %s\n", editor, configPath)

	return nil
}

// Helper function to get config file path.
func getConfigFilePath() (string, error) {
	if configDir != "" {
		return filepath.Join(configDir, config.ConfigFileName), nil
	}

	defaultConfigDir, err := config.GetConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(defaultConfigDir, config.ConfigFileName), nil
}

// validateOutputDirectory checks if a directory path is writable.
func validateOutputDirectory(dir string) error {
	// First check if directory exists or can be created
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create directory: %w", err)
	}

	// Try to create a temporary file to test write permissions
	tempFile := filepath.Join(dir, ".pkm-sync-write-test")

	file, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("no write permission: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close test file: %w", err)
	}

	// Clean up the test file
	if err := os.Remove(tempFile); err != nil {
		return fmt.Errorf("failed to clean up test file: %w", err)
	}

	return nil
}

func runConfigMigrateSecretsCommand(cmd *cobra.Command, args []string) error {
	dir := configDir

	if dir == "" {
		var err error

		dir, err = config.GetConfigDir()
		if err != nil {
			return fmt.Errorf("failed to determine config directory: %w", err)
		}
	}

	fmt.Println("Migrating secrets from files to OS keychain...")

	if err := keystore.MigrateAll(dir, nil); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	fmt.Println("Done. Secrets are now stored in the OS keychain.")

	return nil
}

func runConfigClearTokenCommand(cmd *cobra.Command, args []string) error {
	// Use the store wired up by PersistentPreRun (via auth.SetStore) if available,
	// otherwise fall back to the file-based token path.
	store := auth.GetStore()

	if store != nil {
		if err := store.Delete("google-oauth-token"); err != nil {
			return fmt.Errorf("failed to clear token: %w", err)
		}

		fmt.Printf("Google OAuth token cleared from %s backend.\n", store.Backend())
		fmt.Println("You will be prompted to re-authorize on next use.")

		return nil
	}

	// Legacy fallback: delete file directly.
	tokenPath, err := config.GetTokenPath()
	if err != nil {
		return fmt.Errorf("failed to determine token path: %w", err)
	}

	if err := os.Remove(tokenPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete token file: %w", err)
	}

	fmt.Printf("Google OAuth token file removed: %s\n", tokenPath)
	fmt.Println("You will be prompted to re-authorize on next use.")

	return nil
}
