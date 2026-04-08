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
		// Collect URLs from all current items that haven't been fetched yet.
		newURLs := e.collectNewURLs(allItems, fetched)
		if len(newURLs) == 0 {
			break
		}

		var resolved []models.FullItem

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

			annotateReferenced(item, rawURL)
			resolved = append(resolved, item)
		}

		if len(resolved) == 0 {
			break
		}

		// Cross-reference: tell referring items which resolved IDs they point to.
		crossReference(allItems, resolved)

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

// collectNewURLs gathers all link URLs from items that have not been fetched yet.
func (e *Engine) collectNewURLs(items []models.FullItem, fetched map[string]bool) []string {
	seen := make(map[string]bool)

	var urls []string

	for _, item := range items {
		for _, link := range item.GetLinks() {
			if link.URL == "" {
				continue
			}

			norm := normaliseURL(link.URL)

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

	return urls
}

// annotateReferenced adds "referenced_by" metadata to a resolved item.
func annotateReferenced(item models.FullItem, sourceURL string) {
	meta := item.GetMetadata()
	if meta == nil {
		meta = make(map[string]interface{})
	}

	existing, _ := meta["referenced_by"].([]string)
	meta["referenced_by"] = append(existing, sourceURL)
	item.SetMetadata(meta)
}

// crossReference adds "resolved_refs" metadata to items whose links point to
// any of the newly resolved items (matched by the URL that was resolved).
func crossReference(existing []models.FullItem, resolved []models.FullItem) {
	// Build a set of URLs that produced resolved items, keyed by normalised URL.
	// We store the resolved item ID so referring items can point by ID too.
	resolvedByURL := make(map[string]string, len(resolved))

	for _, r := range resolved {
		for _, link := range r.GetLinks() {
			if link.URL != "" {
				resolvedByURL[normaliseURL(link.URL)] = r.GetID()
			}
		}
	}

	for _, item := range existing {
		for _, link := range item.GetLinks() {
			resolvedID, ok := resolvedByURL[normaliseURL(link.URL)]
			if !ok {
				continue
			}

			meta := item.GetMetadata()
			if meta == nil {
				meta = make(map[string]interface{})
			}

			refs, _ := meta["resolved_refs"].([]string)

			// Avoid duplicates.
			alreadyPresent := false

			for _, r := range refs {
				if r == resolvedID {
					alreadyPresent = true

					break
				}
			}

			if !alreadyPresent {
				meta["resolved_refs"] = append(refs, resolvedID)
				item.SetMetadata(meta)
			}
		}
	}
}

// normaliseURL strips trailing slashes and lowercases the scheme+host so that
// http://Example.com/Foo and https://example.com/Foo/ don't count as different.
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
