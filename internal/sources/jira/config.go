package jira

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type jiraCliConfig struct {
	Server       string `yaml:"server"`
	Login        string `yaml:"login"`
	AuthType     string `yaml:"auth_type"`
	APIToken     string `yaml:"api_token"`
	Installation string `yaml:"installation"`
}

func loadJiraConfig() (*jiraCliConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configPath := filepath.Join(homeDir, ".config", ".jira", ".config.yml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &jiraCliConfig{}, nil
		}

		return nil, fmt.Errorf("failed to read jira-cli config: %w", err)
	}

	var cfg jiraCliConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse jira-cli config: %w", err)
	}

	return &cfg, nil
}

func resolveToken(cliCfg *jiraCliConfig) (string, error) {
	if cliCfg.APIToken != "" {
		return cliCfg.APIToken, nil
	}

	if token := os.Getenv("JIRA_API_TOKEN"); token != "" {
		return token, nil
	}

	if token := os.Getenv("JIRA_TOKEN"); token != "" {
		return token, nil
	}

	return "", fmt.Errorf(
		"no Jira API token found: set JIRA_API_TOKEN env var or configure jira-cli with 'jira init'",
	)
}
