package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shaiknoorullah/wtfrc/internal/indexer/parsers"
	"github.com/shaiknoorullah/wtfrc/internal/llm"
)

const maxBatchSize = 30

// LLMEnricher uses an LLM provider to generate descriptions, intents,
// categories, and see_also references for parsed config entries.
type LLMEnricher struct {
	provider llm.Provider
}

// NewLLMEnricher returns an enricher backed by the given LLM provider.
func NewLLMEnricher(provider llm.Provider) *LLMEnricher {
	return &LLMEnricher{provider: provider}
}

// enrichmentResponse is the JSON structure expected from the LLM.
type enrichmentResponse struct {
	Entries []EnrichedEntry `json:"entries"`
}

// Enrich generates enriched metadata for the given raw entries by sending
// them to the LLM in batches of up to 30.
func (e *LLMEnricher) Enrich(ctx context.Context, entries []parsers.RawEntry) ([]EnrichedEntry, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	var all []EnrichedEntry
	for i := 0; i < len(entries); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(entries) {
			end = len(entries)
		}
		batch := entries[i:end]

		enriched, err := e.enrichBatch(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("enrich batch %d-%d: %w", i, end-1, err)
		}
		all = append(all, enriched...)
	}

	return all, nil
}

func (e *LLMEnricher) enrichBatch(ctx context.Context, entries []parsers.RawEntry) ([]EnrichedEntry, error) {
	prompt := buildEnrichmentPrompt(entries)

	req := llm.CompletionRequest{
		System: `You are a configuration file analyst. For each config entry provided, generate:
- description: a concise, human-readable description of what the entry does
- intents: 2-5 natural-language phrases a user might say when looking for this entry
- category: a single category (e.g., "window_management", "navigation", "shell_alias", "environment", "appearance")
- see_also: related entry bindings/names that the user might also want to know about (may be empty)

Respond with valid JSON matching this schema:
{"entries": [{"description": "...", "intents": ["..."], "category": "...", "see_also": ["..."]}]}

The entries array MUST have exactly the same number of elements as the input, in the same order.`,
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
		MaxTokens:   4096,
		Temperature: 0.3,
	}

	req.ResponseFormat = llm.FormatJSON
	resp, err := e.provider.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}

	result, err := parseEnrichmentResponse(resp.Content)
	if err != nil {
		// Retry once with a correction prompt
		req.Messages = append(req.Messages,
			llm.Message{Role: "assistant", Content: resp.Content},
			llm.Message{Role: "user", Content: "Your previous response was not valid JSON. Respond with raw JSON only — no markdown, no code fences. Use this exact format: [{\"description\":\"...\",\"intents\":[\"...\"],\"category\":\"...\",\"see_also\":[\"...\"]}]"},
		)
		resp, err = e.provider.Complete(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("llm retry: %w", err)
		}
		result, err = parseEnrichmentResponse(resp.Content)
		if err != nil {
			return nil, fmt.Errorf("parse after retry: %w", err)
		}
	}
	for len(result) < len(entries) {
		result = append(result, EnrichedEntry{
			Description: fmt.Sprintf("%s %s %s", entries[len(result)].Tool, entries[len(result)].Type, entries[len(result)].RawBinding),
			Intents:     []string{entries[len(result)].RawBinding},
			Category:    "uncategorized",
		})
	}
	if len(result) > len(entries) {
		result = result[:len(entries)]
	}

	return result, nil
}

func buildEnrichmentPrompt(entries []parsers.RawEntry) string {
	var sb strings.Builder
	sb.WriteString("Analyze these config entries and provide enrichment data:\n\n")
	for i, e := range entries {
		fmt.Fprintf(&sb, "Entry %d:\n", i+1)
		fmt.Fprintf(&sb, "  Tool: %s\n", e.Tool)
		fmt.Fprintf(&sb, "  Type: %s\n", e.Type)
		fmt.Fprintf(&sb, "  Binding: %s\n", e.RawBinding)
		fmt.Fprintf(&sb, "  Action: %s\n", e.RawAction)
		if e.ContextLines != "" {
			fmt.Fprintf(&sb, "  Context:\n    %s\n", e.ContextLines)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// parseEnrichmentResponse handles both {"entries": [...]} and bare [...] formats,
// and strips markdown code fences that small models like to add.
func parseEnrichmentResponse(content string) ([]EnrichedEntry, error) {
	content = strings.TrimSpace(content)
	// Strip markdown code fences
	if strings.HasPrefix(content, "```") {
		if i := strings.Index(content, "\n"); i != -1 {
			content = content[i+1:]
		}
		if i := strings.LastIndex(content, "```"); i != -1 {
			content = content[:i]
		}
		content = strings.TrimSpace(content)
	}

	// Try {"entries": [...]}
	var wrapped enrichmentResponse
	if err := json.Unmarshal([]byte(content), &wrapped); err == nil && len(wrapped.Entries) > 0 {
		return wrapped.Entries, nil
	}

	// Try bare [...]
	var arr []EnrichedEntry
	if err := json.Unmarshal([]byte(content), &arr); err == nil {
		return arr, nil
	}

	return nil, fmt.Errorf("could not parse LLM response as enrichment JSON")
}
