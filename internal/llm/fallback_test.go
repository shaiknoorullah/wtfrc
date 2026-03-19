package llm

import (
	"context"
	"fmt"
	"testing"
)

func TestFallbackPrimaryHealthy(t *testing.T) {
	primary := &mockProvider{
		name:      "primary",
		responses: []CompletionResponse{{Content: "from primary", Model: "p-model"}},
	}
	fallback := &mockProvider{
		name:      "fallback",
		responses: []CompletionResponse{{Content: "from fallback", Model: "f-model"}},
	}

	fp := &FallbackProvider{Primary: primary, Fallback: fallback}

	resp, err := fp.Complete(context.Background(), CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "from primary" {
		t.Errorf("expected primary response, got %q", resp.Content)
	}
	if fallback.callCount != 0 {
		t.Errorf("fallback should not have been called")
	}
}

func TestFallbackPrimaryDown(t *testing.T) {
	primary := &mockProvider{
		name:   "primary",
		errors: []error{fmt.Errorf("connection refused")},
	}
	fallback := &mockProvider{
		name:      "fallback",
		responses: []CompletionResponse{{Content: "from fallback", Model: "f-model"}},
	}

	fp := &FallbackProvider{Primary: primary, Fallback: fallback}

	resp, err := fp.Complete(context.Background(), CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "from fallback" {
		t.Errorf("expected fallback response, got %q", resp.Content)
	}
}

func TestFallbackBothDown(t *testing.T) {
	primary := &mockProvider{
		name:   "primary",
		errors: []error{fmt.Errorf("primary down")},
	}
	fallback := &mockProvider{
		name:   "fallback",
		errors: []error{fmt.Errorf("fallback down")},
	}

	fp := &FallbackProvider{Primary: primary, Fallback: fallback}

	_, err := fp.Complete(context.Background(), CompletionRequest{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFallbackNilFallback(t *testing.T) {
	primary := &mockProvider{
		name:   "primary",
		errors: []error{fmt.Errorf("primary down")},
	}

	fp := &FallbackProvider{Primary: primary, Fallback: nil}

	_, err := fp.Complete(context.Background(), CompletionRequest{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
