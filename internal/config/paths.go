package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	customCredentialsPath string
	customConfigDir       string
)

func SetCustomCredentialsPath(path string) {
	customCredentialsPath = path
}

func SetCustomConfigDir(dir string) {
	customConfigDir = dir
}

func GetConfigDir() (string, error) {
	if customConfigDir != "" {
		return customConfigDir, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("unable to get user home directory: %w", err)
	}

	var configDir string

	switch runtime.GOOS {
	case "windows":
		configDir = filepath.Join(homeDir, "AppData", "Roaming", "pkm-sync")
	case "darwin":
		configDir = filepath.Join(homeDir, ".config", "pkm-sync")
	default:
		configDir = filepath.Join(homeDir, ".config", "pkm-sync")
	}

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", fmt.Errorf("unable to create config directory: %w", err)
	}

	return configDir, nil
}

func GetCredentialsPath() (string, error) {
	if customCredentialsPath != "" {
		return customCredentialsPath, nil
	}

	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(configDir, "credentials.json"), nil
}

func GetTokenPath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(configDir, "token.json"), nil
}

func FindCredentialsFile() (string, error) {
	if customCredentialsPath != "" {
		if _, err := os.Stat(customCredentialsPath); err == nil {
			return customCredentialsPath, nil
		}

		return "", fmt.Errorf("custom credentials file not found: %s", customCredentialsPath)
	}

	paths := getCredentialSearchPaths()

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("credentials.json not found in any search paths: %v", paths)
}

// ExpandPath expands a leading ~ to the user's home directory.
// Paths without a leading ~ are returned unchanged.
func ExpandPath(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("unable to expand path %q: %w", path, err)
	}

	return filepath.Join(homeDir, path[1:]), nil
}

func getCredentialSearchPaths() []string {
	var paths []string

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return []string{"credentials.json"}
	}

	switch runtime.GOOS {
	case "windows":
		paths = append(paths, filepath.Join(homeDir, "AppData", "Roaming", "pkm-sync", "credentials.json"))
	case "darwin":
		paths = append(paths, filepath.Join(homeDir, ".config", "pkm-sync", "credentials.json"))
		paths = append(paths, filepath.Join(homeDir, "Library", "Application Support", "pkm-sync", "credentials.json"))
	default:
		paths = append(paths, filepath.Join(homeDir, ".config", "pkm-sync", "credentials.json"))
	}

	paths = append(paths, "credentials.json")

	return paths
}
