package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/shaiknoorullah/wtfrc/internal/indexer/parsers"
	"github.com/shaiknoorullah/wtfrc/internal/llm"
)

// mockLLMProvider returns valid enrichment JSON for any request.
type mockLLMProvider struct {
	calls int
}

func (m *mockLLMProvider) Name() string { return "mock" }

func (m *mockLLMProvider) Complete(_ context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	m.calls++

	// Count how many entries are in the prompt by counting "Entry N:" lines.
	count := 0
	for i := 0; i < len(req.Messages[0].Content); i++ {
		// Simple counting: look for "Entry " followed by a digit.
		if i+6 < len(req.Messages[0].Content) && req.Messages[0].Content[i:i+6] == "Entry " {
			count++
		}
	}

	var entries []EnrichedEntry
	for i := 0; i < count; i++ {
		entries = append(entries, EnrichedEntry{
			Description: fmt.Sprintf("Mock description %d", i+1),
			Intents:     []string{fmt.Sprintf("intent %d", i+1), fmt.Sprintf("query %d", i+1)},
			Category:    "mock_category",
			SeeAlso:     []string{},
		})
	}

	resp := enrichmentResponse{Entries: entries}
	data, _ := json.Marshal(resp)

	return llm.CompletionResponse{
		Content: string(data),
		Model:   "mock",
	}, nil
}

func (m *mockLLMProvider) Stream(_ context.Context, _ llm.CompletionRequest) (<-chan string, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockLLMProvider) HealthCheck(_ context.Context) error {
	return nil
}

func TestLLMEnricherSingleBatch(t *testing.T) {
	provider := &mockLLMProvider{}
	enricher := NewLLMEnricher(provider)

	// Create 5 entries (well under the 30 batch limit).
	var entries []parsers.RawEntry
	for i := 0; i < 5; i++ {
		entries = append(entries, parsers.RawEntry{
			Tool:       "i3",
			Type:       parsers.EntryKeybind,
			RawBinding: fmt.Sprintf("$mod+%d", i),
			RawAction:  fmt.Sprintf("workspace %d", i),
			SourceFile: "/tmp/i3/config",
			SourceLine: i + 1,
		})
	}

	ctx := context.Background()
	enriched, err := enricher.Enrich(ctx, entries)
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}

	if len(enriched) != 5 {
		t.Fatalf("expected 5 enriched entries, got %d", len(enriched))
	}

	// Should be exactly 1 LLM call.
	if provider.calls != 1 {
		t.Errorf("expected 1 LLM call, got %d", provider.calls)
	}

	// Verify enrichment data is present.
	for i, e := range enriched {
		if e.Description == "" {
			t.Errorf("entry %d has empty description", i)
		}
		if len(e.Intents) == 0 {
			t.Errorf("entry %d has no intents", i)
		}
		if e.Category == "" {
			t.Errorf("entry %d has empty category", i)
		}
	}
}

func TestLLMEnricherMultipleBatches(t *testing.T) {
	provider := &mockLLMProvider{}
	enricher := NewLLMEnricher(provider)

	// Create 60 entries — should result in 2 batches.
	var entries []parsers.RawEntry
	for i := 0; i < 60; i++ {
		entries = append(entries, parsers.RawEntry{
			Tool:       "i3",
			Type:       parsers.EntryKeybind,
			RawBinding: fmt.Sprintf("$mod+key%d", i),
			RawAction:  fmt.Sprintf("action %d", i),
			SourceFile: "/tmp/i3/config",
			SourceLine: i + 1,
		})
	}

	ctx := context.Background()
	enriched, err := enricher.Enrich(ctx, entries)
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}

	if len(enriched) != 60 {
		t.Fatalf("expected 60 enriched entries, got %d", len(enriched))
	}

	// Should be exactly 2 LLM calls (30 + 30).
	if provider.calls != 2 {
		t.Errorf("expected 2 LLM calls for 60 entries, got %d", provider.calls)
	}
}

func TestLLMEnricherExactBatchBoundary(t *testing.T) {
	provider := &mockLLMProvider{}
	enricher := NewLLMEnricher(provider)

	// Create exactly 30 entries — should be 1 batch.
	var entries []parsers.RawEntry
	for i := 0; i < 30; i++ {
		entries = append(entries, parsers.RawEntry{
			Tool:       "zsh",
			Type:       parsers.EntryAlias,
			RawBinding: fmt.Sprintf("alias%d", i),
			RawAction:  fmt.Sprintf("command %d", i),
			SourceFile: "/tmp/.zshrc",
			SourceLine: i + 1,
		})
	}

	ctx := context.Background()
	enriched, err := enricher.Enrich(ctx, entries)
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}

	if len(enriched) != 30 {
		t.Fatalf("expected 30 enriched entries, got %d", len(enriched))
	}
	if provider.calls != 1 {
		t.Errorf("expected 1 LLM call for exactly 30 entries, got %d", provider.calls)
	}
}

func TestLLMEnricherEmptyInput(t *testing.T) {
	provider := &mockLLMProvider{}
	enricher := NewLLMEnricher(provider)

	ctx := context.Background()
	enriched, err := enricher.Enrich(ctx, nil)
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if enriched != nil {
		t.Errorf("expected nil for empty input, got %v", enriched)
	}
	if provider.calls != 0 {
		t.Errorf("expected 0 LLM calls for empty input, got %d", provider.calls)
	}
}
