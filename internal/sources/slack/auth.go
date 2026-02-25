package slack

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

//go:embed playwright/auth.js
var authScriptJS []byte

//go:embed playwright/package.json
var authScriptPackageJSON []byte

// ProgressFunc is called with status updates during auth.
type ProgressFunc func(step, message string)

// RunAuth shells out to a Node/Playwright script that opens a browser,
// waits for the user to log in to Slack, and extracts the session token
// by intercepting API requests. The token is saved to configDir.
func RunAuth(workspaceURL, configDir string, progress ProgressFunc) (*TokenData, error) {
	if progress == nil {
		progress = func(_, _ string) {}
	}

	// Write the embedded auth script and package.json to a stable dir in configDir.
	scriptDir := filepath.Join(configDir, "slack-auth")

	if err := os.MkdirAll(scriptDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create auth script dir: %w", err)
	}

	if err := os.WriteFile(filepath.Join(scriptDir, "auth.js"), authScriptJS, 0600); err != nil {
		return nil, fmt.Errorf("failed to write auth.js: %w", err)
	}

	if err := os.WriteFile(filepath.Join(scriptDir, "package.json"), authScriptPackageJSON, 0600); err != nil {
		return nil, fmt.Errorf("failed to write package.json: %w", err)
	}

	// Install node_modules on first use (or after update).
	nodeModules := filepath.Join(scriptDir, "node_modules", "playwright")

	if _, err := os.Stat(nodeModules); os.IsNotExist(err) {
		progress("npm-install", "Installing Playwright (first time only, may take a minute)...")

		npmCmd := exec.Command("npm", "install")
		npmCmd.Dir = scriptDir

		if out, err := npmCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("npm install failed: %w\n%s", err, out)
		}

		progress("install-browsers", "Installing Playwright Chromium browser...")

		npxCmd := exec.Command("npx", "playwright", "install", "chromium")
		npxCmd.Dir = scriptDir

		if out, err := npxCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("playwright install chromium failed: %w\n%s", err, out)
		}
	}

	profileDir := filepath.Join(configDir, "slack-browser-profile")

	progress("launching", "Opening browser...")

	// Run the auth script. Token JSON goes to stdout; progress JSON lines go to stderr.
	cmd := exec.Command("node", filepath.Join(scriptDir, "auth.js"), workspaceURL, profileDir)
	cmd.Dir = scriptDir

	var stdout strings.Builder

	cmd.Stdout = &stdout

	// Pipe stderr so we can forward progress messages in real time.
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()

		return nil, fmt.Errorf("failed to start auth script: %w", err)
	}

	// Parent closes the write end so the goroutine gets EOF when the child exits.
	pw.Close()

	// Forward progress JSON lines from stderr.
	go func() {
		defer pr.Close()

		scanner := bufio.NewScanner(pr)

		for scanner.Scan() {
			var msg struct {
				Step    string `json:"step"`
				Message string `json:"message"`
			}

			if err := json.Unmarshal(scanner.Bytes(), &msg); err == nil {
				progress(msg.Step, msg.Message)
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("auth script failed: %w", err)
	}

	// Parse token JSON written to stdout by the script.
	var raw struct {
		Token        string        `json:"token"`
		Cookies      []CookieEntry `json:"cookies"`
		CookieHeader string        `json:"cookie_header"`
		Workspace    string        `json:"workspace"`
	}

	if err := json.Unmarshal([]byte(stdout.String()), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse token from auth script: %w", err)
	}

	td := &TokenData{
		Token:        raw.Token,
		Cookies:      raw.Cookies,
		CookieHeader: raw.CookieHeader,
		Timestamp:    time.Now(),
		Workspace:    raw.Workspace,
	}

	if err := SaveToken(configDir, td); err != nil {
		return nil, fmt.Errorf("failed to save token: %w", err)
	}

	return td, nil
}

// workspaceName extracts a safe identifier from a workspace URL.
func workspaceName(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "slack"
	}

	host := u.Hostname()
	parts := strings.Split(host, ".")

	if len(parts) > 0 {
		return parts[0]
	}

	return host
}
