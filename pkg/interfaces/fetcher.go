package interfaces

import (
	"context"

	"pkm-sync/pkg/models"
)

// Fetcher retrieves a single item by its source-native identifier (e.g. an issue
// key like "PROJ-123", a file ID, or a message ID). Sources implement this
// interface alongside Source when they support single-item access.
//
// The command layer discovers this capability via a runtime type assertion:
//
//	if f, ok := src.(interfaces.Fetcher); ok { ... }
type Fetcher interface {
	// FetchOne retrieves a single item by its source-native key.
	// The key format is source-specific: "PROJ-123" for Jira, a Drive file ID,
	// a Gmail thread ID, etc.
	FetchOne(ctx context.Context, key string) (models.FullItem, error)
}
