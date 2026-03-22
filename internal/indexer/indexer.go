package indexer

import (
	"context"
	"fmt"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/indexer/parsers"
	"github.com/shaiknoorullah/wtfrc/internal/kb"
)

// EnrichedEntry holds the LLM-generated metadata for a single raw entry.
type EnrichedEntry struct {
	Description string   `json:"description"`
	Intents     []string `json:"intents"`
	Category    string   `json:"category"`
	SeeAlso     []string `json:"see_also"`
}

// Enricher takes a batch of raw entries and returns enriched metadata.
type Enricher interface {
	Enrich(ctx context.Context, entries []parsers.RawEntry) ([]EnrichedEntry, error)
}

// Indexer orchestrates the parsing, redaction, enrichment, and storage pipeline.
type Indexer struct {
	db       *kb.DB
	enricher Enricher
	redactor *Redactor
	manifest *Manifest
}

// New creates an Indexer with the given dependencies.
func New(db *kb.DB, enricher Enricher, redactor *Redactor) *Indexer {
	return &Indexer{
		db:       db,
		enricher: enricher,
		redactor: redactor,
		manifest: NewManifest(db),
	}
}

// Index runs the full indexing pipeline for the given file paths.
//
// For each path it:
//  1. Checks the manifest for changes (skips unchanged files).
//  2. Finds the appropriate parser and parses raw entries.
//  3. Redacts sensitive values from each entry.
//  4. Deletes old entries for the file, enriches via the Enricher,
//     then stores new entries and intents in the KB.
//  5. Updates the manifest with the new file hash.
func (idx *Indexer) Index(ctx context.Context, paths []string) error {
	for _, path := range paths {
		if err := idx.indexFile(ctx, path); err != nil {
			return fmt.Errorf("index %s: %w", path, err)
		}
	}
	return nil
}

func (idx *Indexer) indexFile(ctx context.Context, path string) error {
	// Step 1: check manifest for changes.
	changed, err := idx.manifest.IsChanged(path)
	if err != nil {
		return fmt.Errorf("check manifest: %w", err)
	}
	if !changed {
		return nil // file unchanged, skip
	}

	// Step 2: find parser and parse.
	p := parsers.ForFile(path)
	if p == nil {
		return nil // no parser available, skip silently
	}

	rawEntries, err := p.Parse(path)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	// Step 3: redact each entry.
	if idx.redactor != nil {
		for i := range rawEntries {
			rawEntries[i].RawAction = idx.redactor.Redact(rawEntries[i].RawAction)
			rawEntries[i].ContextLines = idx.redactor.Redact(rawEntries[i].ContextLines)
		}
	}

	// Step 4: delete old entries for this file, then enrich and store.
	if err := idx.db.DeleteEntriesByFile(path); err != nil {
		return fmt.Errorf("delete old entries: %w", err)
	}

	enriched, err := idx.enricher.Enrich(ctx, rawEntries)
	if err != nil {
		return fmt.Errorf("enrich: %w", err)
	}

	// Compute hash for the entry records.
	hash, err := idx.manifest.ComputeHash(path)
	if err != nil {
		return fmt.Errorf("compute hash: %w", err)
	}

	now := time.Now()
	for i, raw := range rawEntries {
		if i >= len(enriched) {
			break
		}
		e := enriched[i]

		binding := raw.RawBinding
		action := raw.RawAction

		kbEntry := kb.KBEntry{
			Tool:        raw.Tool,
			Type:        raw.Type,
			RawBinding:  &binding,
			RawAction:   &action,
			Description: e.Description,
			SourceFile:  raw.SourceFile,
			SourceLine:  raw.SourceLine,
			Category:    e.Category,
			SeeAlso:     e.SeeAlso,
			IndexedAt:   now,
			FileHash:    hash,
		}

		if _, err := idx.db.InsertEntry(&kbEntry, e.Intents); err != nil {
			return fmt.Errorf("insert entry: %w", err)
		}
	}

	// Step 5: update manifest.
	if err := idx.manifest.Update(&kb.ManifestEntry{
		FilePath:    path,
		SHA256:      hash,
		Mtime:       now,
		LastIndexed: now,
		EntryCount:  len(rawEntries),
	}); err != nil {
		return fmt.Errorf("update manifest: %w", err)
	}

	return nil
}
