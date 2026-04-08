// Package resolve implements cross-source reference resolution.
// When synced items contain links to content in other sources (e.g. a Slack
// message linking to a Jira issue), the engine fetches the referenced content
// and appends it to the item set so it flows through the normal Sink pipeline.
package resolve

import (
	"context"
	"log/slog"
	"net/url"
	"strings"

	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

// Config controls the behavior of a single Resolve call.
type Config struct {
	// MaxDepth is the maximum number of resolution rounds to run.
	// 0 is treated as 1 — at least one round is always attempted.
	MaxDepth int
}

// Engine runs multi-depth cross-source reference resolution.
type Engine struct {
	resolvers []interfaces.Resolver
}

// NewEngine creates an Engine backed by the given resolvers.
// Resolver order matters: the first resolver whose CanResolve returns true
// for a given URL wins.
func NewEngine(resolvers []interfaces.Resolver) *Engine {
	return &Engine{resolvers: resolvers}
}

// Resolve runs up to cfg.MaxDepth rounds of link scanning and resolution
// against items. The returned slice always includes the original items;
// resolved items are appended after them.
//
// Each unique URL is fetched at most once per Resolve call (dedup by
// normalised URL). Resolver errors are logged and skipped — they do not abort
// the run. Items returned by resolvers flow into subsequent rounds so that
// transitive references are followed up to cfg.MaxDepth.
func (e *Engine) Resolve(
	ctx context.Context,
	items []models.FullItem,
	cfg Config,
) ([]models.FullItem, error) {
	if len(e.resolvers) == 0 {
		return items, nil
	}

	maxDepth := cfg.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 1
	}

	// fetched tracks normalised URLs already resolved in this call.
	fetched := make(map[string]bool)
	// allItems accumulates original + resolved items across all rounds.
	allItems := make([]models.FullItem, len(items))
	copy(allItems, items)

	for depth := 0; depth < maxDepth; depth++ {
		// Collect URLs from all current items that haven't been fetched yet,
		// along with a map of which item IDs link to each target URL.
		newURLs, referrerMap := e.collectNewURLs(allItems, fetched)
		if len(newURLs) == 0 {
			break
		}

		var resolved []models.FullItem

		// resolvedURLToID maps normalised rawURL → resolved item ID, keyed by the
		// URL used to resolve rather than the resolved item's outbound links.
		resolvedURLToID := make(map[string]string, len(newURLs))

		for _, rawURL := range newURLs {
			norm := normaliseURL(rawURL)
			fetched[norm] = true

			item, err := e.resolveOne(ctx, rawURL)
			if err != nil {
				slog.Warn("Reference resolution failed", "url", rawURL, "error", err)

				continue
			}

			if item == nil {
				// Resolver signaled skip (e.g. freshness check passed).
				continue
			}

			// Record which items referred to this URL so the resolved item
			// knows who links to it.
			annotateReferenced(item, referrerMap[norm])
			resolvedURLToID[norm] = item.GetID()
			resolved = append(resolved, item)
		}

		if len(resolved) == 0 {
			break
		}

		// Cross-reference: tell referring items which resolved IDs they point to.
		crossReference(allItems, resolvedURLToID)

		allItems = append(allItems, resolved...)

		slog.Info("Resolved references",
			"depth", depth+1,
			"new_items", len(resolved),
			"total_items", len(allItems),
		)
	}

	return allItems, nil
}

// resolveOne tries each resolver in order and returns the first match.
func (e *Engine) resolveOne(ctx context.Context, rawURL string) (models.FullItem, error) {
	for _, r := range e.resolvers {
		if r.CanResolve(rawURL) {
			return r.Resolve(ctx, rawURL)
		}
	}

	return nil, nil // no resolver matched
}

// collectNewURLs gathers resolvable link URLs not yet fetched, together with a
// referrerMap that maps each normalised target URL to the IDs of items that
// link to it. The referrerMap is used to populate "referenced_by" metadata on
// resolved items.
func (e *Engine) collectNewURLs(
	items []models.FullItem,
	fetched map[string]bool,
) (urls []string, referrerMap map[string][]string) {
	seen := make(map[string]bool)
	referrerMap = make(map[string][]string)

	for _, item := range items {
		for _, link := range item.GetLinks() {
			if link.URL == "" {
				continue
			}

			norm := normaliseURL(link.URL)

			// Always track referrers regardless of dedup state.
			referrerMap[norm] = appendUnique(referrerMap[norm], item.GetID())

			if fetched[norm] || seen[norm] {
				continue
			}

			// Only include URLs that at least one resolver can handle.
			for _, r := range e.resolvers {
				if r.CanResolve(link.URL) {
					seen[norm] = true

					urls = append(urls, link.URL)

					break
				}
			}
		}
	}

	return urls, referrerMap
}

// annotateReferenced sets "referenced_by" metadata on a resolved item to the
// IDs of the items that contained the link to it.
func annotateReferenced(item models.FullItem, referrerIDs []string) {
	if len(referrerIDs) == 0 {
		return
	}

	meta := item.GetMetadata()
	if meta == nil {
		meta = make(map[string]interface{})
	}

	existing, _ := meta["referenced_by"].([]string)
	for _, id := range referrerIDs {
		existing = appendUnique(existing, id)
	}

	meta["referenced_by"] = existing
	item.SetMetadata(meta)
}

// crossReference adds "resolved_refs" metadata to existing items whose links
// were resolved. resolvedURLToID maps normalised target URL → resolved item ID,
// built directly from the rawURLs used to resolve each item so that items with
// no self-link (e.g. some Jira items) are still cross-referenced correctly.
func crossReference(existing []models.FullItem, resolvedURLToID map[string]string) {
	for _, item := range existing {
		for _, link := range item.GetLinks() {
			if link.URL == "" {
				continue
			}

			resolvedID, ok := resolvedURLToID[normaliseURL(link.URL)]
			if !ok {
				continue
			}

			meta := item.GetMetadata()
			if meta == nil {
				meta = make(map[string]interface{})
			}

			refs, _ := meta["resolved_refs"].([]string)
			meta["resolved_refs"] = appendUnique(refs, resolvedID)
			item.SetMetadata(meta)
		}
	}
}

// appendUnique appends s to slice only if it is not already present.
func appendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}

	return append(slice, s)
}

// normaliseURL lowercases the scheme and host and strips trailing slashes.
// Path casing is preserved: some targets are case-sensitive so paths are not
// lowercased. http://Example.com/path/ and https://example.com/path resolve to
// the same key, but https://example.com/Path and /path are distinct.
func normaliseURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return strings.ToLower(strings.TrimRight(raw, "/"))
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	result := parsed.String()

	return strings.TrimRight(result, "/")
}
