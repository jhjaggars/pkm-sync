package resolve

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

// --- mock resolver ---

type mockResolver struct {
	name      string
	canHandle func(url string) bool
	resolve   func(ctx context.Context, url string) (models.FullItem, error)
	calls     atomic.Int64
}

func (m *mockResolver) Name() string { return m.name }

func (m *mockResolver) CanResolve(url string) bool {
	return m.canHandle(url)
}

func (m *mockResolver) Resolve(ctx context.Context, url string) (models.FullItem, error) {
	m.calls.Add(1)

	return m.resolve(ctx, url)
}

var _ interfaces.Resolver = (*mockResolver)(nil)

// makeItem creates a minimal FullItem with a link to the given URL.
func makeItem(id, linkURL string) models.FullItem {
	item := models.NewBasicItem(id, id)
	if linkURL != "" {
		item.SetLinks([]models.Link{{URL: linkURL, Type: "external"}})
	}

	return item
}

// makeResolvedItem returns a FullItem whose own Links point back to sourceURL
// (simulating a resolver that records the URL it was resolved from).
func makeResolvedItem(id, sourceURL string) models.FullItem {
	item := models.NewBasicItem(id, id)
	item.SetLinks([]models.Link{{URL: sourceURL, Type: "document"}})
	item.SetMetadata(map[string]interface{}{})

	return item
}

// --- tests ---

func TestEngine_NoResolvers(t *testing.T) {
	engine := NewEngine(nil)
	items := []models.FullItem{makeItem("a", "https://example.com")}

	result, err := engine.Resolve(context.Background(), items, Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 item, got %d", len(result))
	}
}

func TestEngine_NoMatchingResolver(t *testing.T) {
	r := &mockResolver{
		name:      "noop",
		canHandle: func(url string) bool { return false },
		resolve:   func(_ context.Context, _ string) (models.FullItem, error) { return nil, nil },
	}

	engine := NewEngine([]interfaces.Resolver{r})
	items := []models.FullItem{makeItem("a", "https://example.com")}

	result, err := engine.Resolve(context.Background(), items, Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 item unchanged, got %d", len(result))
	}

	if r.calls.Load() != 0 {
		t.Errorf("resolver should not have been called")
	}
}

func TestEngine_DeduplicatesURLs(t *testing.T) {
	const targetURL = "https://docs.google.com/document/d/abc123"

	callCount := atomic.Int64{}
	r := &mockResolver{
		name:      "dedup",
		canHandle: func(url string) bool { return url == targetURL },
		resolve: func(_ context.Context, url string) (models.FullItem, error) {
			callCount.Add(1)
			return makeResolvedItem("resolved-1", url), nil
		},
	}

	engine := NewEngine([]interfaces.Resolver{r})

	// Two items that both link to the same URL.
	items := []models.FullItem{
		makeItem("a", targetURL),
		makeItem("b", targetURL),
	}

	result, err := engine.Resolve(context.Background(), items, Config{MaxDepth: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callCount.Load() != 1 {
		t.Errorf("URL should be resolved exactly once, got %d calls", callCount.Load())
	}

	// original 2 + 1 resolved
	if len(result) != 3 {
		t.Errorf("expected 3 items, got %d", len(result))
	}
}

func TestEngine_DepthLimit(t *testing.T) {
	// depth-1 URL resolves to an item that itself links to depth-2 URL
	const url1 = "https://jira.example.com/browse/PROJ-1"

	const url2 = "https://jira.example.com/browse/PROJ-2"

	resolveCount := atomic.Int64{}
	r := &mockResolver{
		name:      "jira",
		canHandle: func(url string) bool { return url == url1 || url == url2 },
		resolve: func(_ context.Context, url string) (models.FullItem, error) {
			resolveCount.Add(1)

			if url == url1 {
				// The resolved item itself links to url2.
				item := makeResolvedItem("issue-1", url1)
				item.SetLinks([]models.Link{{URL: url2, Type: "external"}})

				return item, nil
			}

			return makeResolvedItem("issue-2", url2), nil
		},
	}

	engine := NewEngine([]interfaces.Resolver{r})
	items := []models.FullItem{makeItem("msg", url1)}

	// Depth 1: should only resolve url1, NOT url2.
	result, err := engine.Resolve(context.Background(), items, Config{MaxDepth: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolveCount.Load() != 1 {
		t.Errorf("depth=1: expected 1 resolve call, got %d", resolveCount.Load())
	}

	// original + issue-1
	if len(result) != 2 {
		t.Errorf("depth=1: expected 2 items, got %d", len(result))
	}

	// Reset and run depth 2: should resolve url1 then url2.
	resolveCount.Store(0)

	result2, err := engine.Resolve(context.Background(), items, Config{MaxDepth: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolveCount.Load() != 2 {
		t.Errorf("depth=2: expected 2 resolve calls, got %d", resolveCount.Load())
	}

	// original + issue-1 + issue-2
	if len(result2) != 3 {
		t.Errorf("depth=2: expected 3 items, got %d", len(result2))
	}
}

func TestEngine_DefaultDepthIsOne(t *testing.T) {
	const url1 = "https://example.com/a"

	const url2 = "https://example.com/b"

	resolveCount := atomic.Int64{}
	r := &mockResolver{
		name:      "test",
		canHandle: func(url string) bool { return url == url1 || url == url2 },
		resolve: func(_ context.Context, url string) (models.FullItem, error) {
			resolveCount.Add(1)

			item := makeResolvedItem("resolved", url)
			if url == url1 {
				item.SetLinks([]models.Link{{URL: url2}})
			}

			return item, nil
		},
	}

	engine := NewEngine([]interfaces.Resolver{r})
	items := []models.FullItem{makeItem("start", url1)}

	// MaxDepth: 0 should be treated as 1.
	_, err := engine.Resolve(context.Background(), items, Config{MaxDepth: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolveCount.Load() != 1 {
		t.Errorf("MaxDepth=0 should behave as depth=1, got %d resolve calls", resolveCount.Load())
	}
}

func TestEngine_CrossRefMetadata(t *testing.T) {
	const targetURL = "https://docs.google.com/document/d/xyz"

	r := &mockResolver{
		name:      "drive",
		canHandle: func(url string) bool { return url == targetURL },
		resolve: func(_ context.Context, url string) (models.FullItem, error) {
			return makeResolvedItem("drive_xyz", url), nil
		},
	}

	engine := NewEngine([]interfaces.Resolver{r})
	original := makeItem("slack-msg", targetURL)
	original.SetMetadata(map[string]interface{}{})

	result, err := engine.Resolve(context.Background(), []models.FullItem{original}, Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}

	// The original item should have resolved_refs set.
	origMeta := result[0].GetMetadata()
	refs, ok := origMeta["resolved_refs"].([]string)

	if !ok || len(refs) == 0 {
		t.Errorf("original item missing resolved_refs metadata, got: %v", origMeta["resolved_refs"])
	}

	// The resolved item should have referenced_by set.
	resolvedMeta := result[1].GetMetadata()
	refBy, ok := resolvedMeta["referenced_by"].([]string)

	if !ok || len(refBy) == 0 {
		t.Errorf("resolved item missing referenced_by metadata, got: %v", resolvedMeta["referenced_by"])
	}
}

func TestEngine_ResolverError(t *testing.T) {
	const targetURL = "https://example.com/fail"

	r := &mockResolver{
		name:      "failing",
		canHandle: func(url string) bool { return url == targetURL },
		resolve: func(_ context.Context, url string) (models.FullItem, error) {
			return nil, errors.New("network error")
		},
	}

	engine := NewEngine([]interfaces.Resolver{r})
	items := []models.FullItem{makeItem("a", targetURL)}

	// Errors from individual resolvers must not propagate as fatal.
	result, err := engine.Resolve(context.Background(), items, Config{})
	if err != nil {
		t.Fatalf("resolver error should be non-fatal, got: %v", err)
	}

	// Only the original item; nothing resolved due to error.
	if len(result) != 1 {
		t.Errorf("expected 1 item (original only), got %d", len(result))
	}
}

func TestEngine_FreshnessSkip(t *testing.T) {
	const targetURL = "https://example.com/fresh"

	r := &mockResolver{
		name:      "fresh",
		canHandle: func(url string) bool { return url == targetURL },
		// Returning (nil, nil) signals "skip this item".
		resolve: func(_ context.Context, url string) (models.FullItem, error) {
			return nil, nil
		},
	}

	engine := NewEngine([]interfaces.Resolver{r})
	items := []models.FullItem{makeItem("a", targetURL)}

	result, err := engine.Resolve(context.Background(), items, Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("freshness skip should produce no new item, got %d items", len(result))
	}
}

func TestEngine_OriginalItemsAlwaysIncluded(t *testing.T) {
	engine := NewEngine(nil)
	items := []models.FullItem{
		makeItem("a", ""),
		makeItem("b", ""),
	}

	result, err := engine.Resolve(context.Background(), items, Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("original items must always be in result, got %d", len(result))
	}
}

func TestEngine_FirstResolverWins(t *testing.T) {
	const targetURL = "https://example.com/item"

	calls := []string{}

	r1 := &mockResolver{
		name:      "first",
		canHandle: func(url string) bool { return true },
		resolve: func(_ context.Context, url string) (models.FullItem, error) {
			calls = append(calls, "first")
			return makeResolvedItem("from-first", url), nil
		},
	}

	r2 := &mockResolver{
		name:      "second",
		canHandle: func(url string) bool { return true },
		resolve: func(_ context.Context, url string) (models.FullItem, error) {
			calls = append(calls, "second")
			return makeResolvedItem("from-second", url), nil
		},
	}

	engine := NewEngine([]interfaces.Resolver{r1, r2})
	items := []models.FullItem{makeItem("a", targetURL)}

	_, err := engine.Resolve(context.Background(), items, Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(calls) != 1 || calls[0] != "first" {
		t.Errorf("only the first matching resolver should be called, got: %v", calls)
	}
}

// Ensure Engine works correctly with a zero-value time for items without links.
func TestEngine_ItemsWithNoLinks(t *testing.T) {
	r := &mockResolver{
		name:      "noop",
		canHandle: func(url string) bool { return true },
		resolve: func(_ context.Context, url string) (models.FullItem, error) {
			return makeResolvedItem("resolved", url), nil
		},
	}

	engine := NewEngine([]interfaces.Resolver{r})

	item := models.NewBasicItem("no-links", "No Links")
	item.SetLinks(nil)
	item.SetMetadata(map[string]interface{}{})
	item.SetCreatedAt(time.Now())

	result, err := engine.Resolve(context.Background(), []models.FullItem{item}, Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("item with no links should pass through unchanged, got %d items", len(result))
	}

	if r.calls.Load() != 0 {
		t.Errorf("resolver should not be called for items with no links")
	}
}
