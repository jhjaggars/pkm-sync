package interfaces

import (
	"context"

	"pkm-sync/pkg/models"
)

// Searcher queries a source's local index (FTS or vector store) for items
// matching a text query. This is implemented by local index wrappers, not by
// live source API clients.
//
// The command layer discovers this capability via a runtime type assertion:
//
//	if s, ok := backend.(interfaces.Searcher); ok { ... }
type Searcher interface {
	// Search queries the local index for items matching query and returns up to
	// limit results. Implementations should return results ordered by relevance.
	Search(ctx context.Context, query string, limit int) ([]models.FullItem, error)
}
