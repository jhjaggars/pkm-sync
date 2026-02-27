package sync

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	"pkm-sync/pkg/interfaces"
	"pkm-sync/pkg/models"
)

// SourceEntry pairs a named, pre-created Source with per-source sync options.
type SourceEntry struct {
	Name  string
	Src   interfaces.Source
	Since time.Time // zero = use MultiSyncOptions.DefaultSince
	Limit int       // 0 = use MultiSyncOptions.DefaultLimit
}

// MultiSyncOptions controls the behavior of MultiSyncer.SyncAll.
type MultiSyncOptions struct {
	DefaultSince time.Time
	DefaultLimit int
	SourceTags   bool
	TransformCfg models.TransformConfig
	DryRun       bool
}

// SourceResult records the outcome of fetching a single source.
type SourceResult struct {
	Name      string
	ItemCount int
	Err       error
}

// MultiSyncResult is returned by SyncAll.
type MultiSyncResult struct {
	SourceResults []SourceResult
	// Items holds the transformed items ready for export.
	// In dry-run mode sinks are not written to but Items is still populated.
	Items []models.FullItem
}

// fetchResult holds the outcome of fetching a single source.
type fetchResult struct {
	sr    SourceResult
	items []models.FullItem
}

// MultiSyncer fetches from multiple sources, runs a transformer pipeline,
// and fans out to one or more Sinks.
type MultiSyncer struct {
	pipeline interfaces.TransformPipeline
}

// NewMultiSyncer creates a MultiSyncer. pipeline may be nil to skip transformation.
func NewMultiSyncer(pipeline interfaces.TransformPipeline) *MultiSyncer {
	return &MultiSyncer{pipeline: pipeline}
}

// SyncAll executes the full Sources → Transform → Sinks pipeline.
//
// It fetches from each source in entries concurrently, applies source tags if
// requested, runs the transformer pipeline, and writes to all sinks concurrently
// (unless DryRun is set). Source failures are non-fatal: they are recorded in
// the result and the remaining sources continue to be processed. Sink failures
// are fatal: the first sink error cancels remaining sinks and is returned.
func (m *MultiSyncer) SyncAll(
	ctx context.Context,
	entries []SourceEntry,
	sinks []interfaces.Sink,
	opts MultiSyncOptions,
) (*MultiSyncResult, error) {
	result := &MultiSyncResult{}

	// --- Phase 1: Fetch from all sources (concurrent) ---
	// Pre-allocate indexed slice so each goroutine writes to its own position.
	results := make([]fetchResult, len(entries))
	g, gCtx := errgroup.WithContext(ctx)

	for i, entry := range entries {
		g.Go(func() error {
			if gCtx.Err() != nil {
				return nil
			}

			since := opts.DefaultSince
			if !entry.Since.IsZero() {
				since = entry.Since
			}

			limit := opts.DefaultLimit
			if entry.Limit > 0 {
				limit = entry.Limit
			}

			if limit <= 0 {
				limit = 1000
			}

			items, err := entry.Src.Fetch(since, limit)
			if err != nil {
				fmt.Printf("Warning: failed to fetch from source '%s': %v, skipping\n", entry.Name, err)
				results[i] = fetchResult{sr: SourceResult{Name: entry.Name, Err: err}}

				return nil
			}

			// Apply source tag when enabled
			if opts.SourceTags {
				for _, item := range items {
					item.SetTags(append(item.GetTags(), "source:"+entry.Name))
				}
			}

			fmt.Printf("Fetched %d items from %s\n", len(items), entry.Name)
			results[i] = fetchResult{
				sr:    SourceResult{Name: entry.Name, ItemCount: len(items)},
				items: items,
			}

			return nil
		})
	}

	// goroutines always return nil, so this can only fail if ctx is canceled
	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Merge results in entry order into allItems and SourceResults.
	var allItems []models.FullItem

	for _, r := range results {
		result.SourceResults = append(result.SourceResults, r.sr)
		allItems = append(allItems, r.items...)
	}

	fmt.Printf("Total items collected: %d\n", len(allItems))

	// --- Phase 2: Transform ---
	if m.pipeline != nil && opts.TransformCfg.Enabled {
		if err := m.pipeline.Configure(opts.TransformCfg); err != nil {
			return nil, fmt.Errorf("failed to configure transformer pipeline: %w", err)
		}

		transformed, err := m.pipeline.Transform(allItems)
		if err != nil {
			return nil, fmt.Errorf("failed to transform items: %w", err)
		}

		fmt.Printf("Transformed to %d items\n", len(transformed))
		allItems = transformed
	}

	result.Items = allItems

	// --- Phase 3: Write to sinks (concurrent, skipped in dry-run mode) ---
	// First sink failure cancels remaining sinks via errgroup context.
	if !opts.DryRun {
		gw, gwCtx := errgroup.WithContext(ctx)

		for _, sink := range sinks {
			gw.Go(func() error {
				if err := sink.Write(gwCtx, allItems); err != nil {
					return fmt.Errorf("sink '%s' write failed: %w", sink.Name(), err)
				}

				return nil
			})
		}

		if err := gw.Wait(); err != nil {
			return nil, err
		}
	}

	return result, nil
}
