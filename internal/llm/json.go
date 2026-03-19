package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// CompleteJSON sends a completion request with JSON format and unmarshals the response.
// If the first attempt returns malformed JSON, it retries once with a correction prompt.
func CompleteJSON[T any](ctx context.Context, p Provider, req CompletionRequest) (T, error) {
	var zero T
	req.ResponseFormat = FormatJSON

	resp, err := p.Complete(ctx, req)
	if err != nil {
		return zero, fmt.Errorf("complete json: %w", err)
	}

	var result T
	if err := json.Unmarshal([]byte(stripCodeFences(resp.Content)), &result); err == nil {
		return result, nil
	}

	// Retry: append the malformed response and ask for valid JSON
	req.Messages = append(req.Messages,
		Message{Role: "assistant", Content: resp.Content},
		Message{Role: "user", Content: "Your previous response was not valid JSON. Respond with raw JSON only — no markdown, no code fences, no explanation."},
	)

	resp, err = p.Complete(ctx, req)
	if err != nil {
		return zero, fmt.Errorf("complete json retry: %w", err)
	}

	if err := json.Unmarshal([]byte(stripCodeFences(resp.Content)), &result); err != nil {
		return zero, fmt.Errorf("complete json: failed to parse after retry: %w", err)
	}

	return result, nil
}

// stripCodeFences removes markdown code fences that LLMs often wrap JSON in.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// Remove opening fence (```json or ```)
		if i := strings.Index(s, "\n"); i != -1 {
			s = s[i+1:]
		}
		// Remove closing fence
		if i := strings.LastIndex(s, "```"); i != -1 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}
