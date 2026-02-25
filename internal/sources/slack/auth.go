package slack

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// ProgressFunc is called with status updates during auth.
type ProgressFunc func(step, message string)

var tokenRegexp = regexp.MustCompile(`(?m)name="token"\r\n\r\n([^\r\n]+)`)

// RunAuth opens a browser, waits for the user to log in to Slack, then
// extracts the session token by intercepting API requests.
func RunAuth(workspaceURL, configDir string, progress ProgressFunc) (*TokenData, error) {
	if progress == nil {
		progress = func(_, _ string) {}
	}

	profileDir := filepath.Join(configDir, "slack-browser-profile")

	progress("launching", "Opening browser...")

	u := launcher.NewUserMode().UserDataDir(profileDir).Set("no-sandbox").MustLaunch()

	browser := rod.New().ControlURL(u).MustConnect()
	defer browser.MustClose()

	page := browser.MustPage(workspaceURL)

	var capturedToken string

	capturedCookies := make([]CookieEntry, 0)

	// Intercept requests to capture the multipart form token
	router := browser.HijackRequests()

	if err := router.Add("*/api/conversations.history", proto.NetworkResourceTypeXHR, func(ctx *rod.Hijack) {
		body := ctx.Request.Body()
		if capturedToken == "" {
			if m := tokenRegexp.FindStringSubmatch(body); len(m) > 1 {
				capturedToken = strings.TrimSpace(m[1])
				progress("token-captured", fmt.Sprintf("Captured token: %s...", capturedToken[:min(15, len(capturedToken))]))
			}
		}
		ctx.ContinueRequest(&proto.FetchContinueRequest{})
	}); err != nil {
		return nil, fmt.Errorf("failed to add history hijack: %w", err)
	}

	if err := router.Add("*/api/conversations.replies", proto.NetworkResourceTypeXHR, func(ctx *rod.Hijack) {
		body := ctx.Request.Body()
		if capturedToken == "" {
			if m := tokenRegexp.FindStringSubmatch(body); len(m) > 1 {
				capturedToken = strings.TrimSpace(m[1])
				progress("token-captured", fmt.Sprintf("Captured token: %s...", capturedToken[:min(15, len(capturedToken))]))
			}
		}
		ctx.ContinueRequest(&proto.FetchContinueRequest{})
	}); err != nil {
		return nil, fmt.Errorf("failed to add replies hijack: %w", err)
	}

	go router.Run()

	defer func() { _ = router.Stop() }()

	progress("waiting", "Waiting for Slack workspace to load (please complete SSO if prompted)...")

	if err := page.Timeout(120 * time.Second).WaitStable(2 * time.Second); err != nil {
		return nil, fmt.Errorf("workspace did not load: %w", err)
	}

	// Wait for the sidebar (role="tree" is the channel list)
	if _, err := page.Timeout(120 * time.Second).Element(`[role="tree"]`); err != nil {
		return nil, fmt.Errorf("timed out waiting for Slack sidebar: %w", err)
	}

	progress("loaded", "Workspace loaded, triggering API call...")

	// Trigger a conversations.history call by navigating to #general
	// Use Cmd+K on macOS, Ctrl+K elsewhere
	var modKey input.Key
	if runtime.GOOS == "darwin" {
		modKey = input.MetaLeft
	} else {
		modKey = input.ControlLeft
	}

	if err := page.KeyActions().Press(modKey, input.KeyK).Do(); err != nil {
		return nil, fmt.Errorf("failed to open channel switcher: %w", err)
	}

	time.Sleep(500 * time.Millisecond)

	if err := page.Keyboard.Type([]input.Key("general")...); err != nil {
		return nil, fmt.Errorf("failed to type channel name: %w", err)
	}

	time.Sleep(1 * time.Second)

	if err := page.Keyboard.Press(input.Enter); err != nil {
		return nil, fmt.Errorf("failed to press enter: %w", err)
	}

	// Wait up to 10 seconds for the token to be captured
	progress("waiting-token", "Waiting for API token...")

	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) && capturedToken == "" {
		time.Sleep(500 * time.Millisecond)
	}

	if capturedToken == "" {
		return nil, fmt.Errorf("failed to capture API token — try navigating to a channel manually while the browser is open")
	}

	// Grab cookies
	progress("cookies", "Capturing cookies...")

	cookies, err := browser.GetCookies()
	if err != nil {
		return nil, fmt.Errorf("failed to get cookies: %w", err)
	}

	cookieParts := make([]string, 0, len(cookies))

	for _, c := range cookies {
		ce := CookieEntry{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  float64(c.Expires),
			HTTPOnly: c.HTTPOnly,
			Secure:   c.Secure,
			SameSite: string(c.SameSite),
		}
		capturedCookies = append(capturedCookies, ce)
		cookieParts = append(cookieParts, c.Name+"="+c.Value)
	}

	cookieHeader := strings.Join(cookieParts, "; ")

	// Derive workspace name from URL
	workspace := workspaceName(workspaceURL)

	td := &TokenData{
		Token:        capturedToken,
		Cookies:      capturedCookies,
		CookieHeader: cookieHeader,
		Timestamp:    time.Now(),
		Workspace:    workspace,
	}

	if err := SaveToken(configDir, td); err != nil {
		return nil, fmt.Errorf("failed to save token: %w", err)
	}

	progress("complete", "Authentication successful")

	return td, nil
}

// workspaceName extracts a safe identifier from a workspace URL.
func workspaceName(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "slack"
	}

	host := u.Hostname()
	// e.g. "myworkspace.slack.com" -> "myworkspace"
	parts := strings.Split(host, ".")

	if len(parts) > 0 {
		return parts[0]
	}

	return host
}
