package interfaces

import (
	"context"

	"pkm-sync/pkg/models"
)

// Resolver matches URLs from item links and fetches the referenced content as a FullItem.
// Each implementation covers one source type (e.g. Google Drive, Jira).
type Resolver interface {
	// Name returns a short identifier used in logs, e.g. "drive" or "jira".
	Name() string

	// CanResolve returns true when this resolver knows how to handle rawURL.
	// Implementations must be fast (regex/string match only, no network calls).
	CanResolve(rawURL string) bool

	// Resolve fetches the content referenced by rawURL and returns it as a FullItem.
	// Returns (nil, nil) when the URL is valid for this resolver but the content
	// should be skipped (e.g. a freshness check determines it is not stale).
	// Returns a non-nil error only for unexpected failures; callers log the error
	// and continue processing remaining URLs.
	Resolve(ctx context.Context, rawURL string) (models.FullItem, error)
}
