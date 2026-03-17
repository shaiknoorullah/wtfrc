package llm

import (
	"context"
	"fmt"
	"testing"
)

type mockProvider struct {
	name      string
	responses []CompletionResponse
	errors    []error
	callCount int
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) Complete(_ context.Context, _ CompletionRequest) (CompletionResponse, error) {
	i := m.callCount
	m.callCount++
	if i < len(m.errors) && m.errors[i] != nil {
		return CompletionResponse{}, m.errors[i]
	}
	if i < len(m.responses) {
		return m.responses[i], nil
	}
	return CompletionResponse{}, fmt.Errorf("unexpected call %d", i)
}
func (m *mockProvider) Stream(_ context.Context, _ CompletionRequest) (<-chan string, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockProvider) HealthCheck(_ context.Context) error { return nil }

type testResult struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func TestCompleteJSONSuccess(t *testing.T) {
	p := &mockProvider{
		name:      "mock",
		responses: []CompletionResponse{{Content: `{"name":"test","value":42}`}},
	}

	result, err := CompleteJSON[testResult](context.Background(), p, CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != "test" || result.Value != 42 {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestCompleteJSONRetrySuccess(t *testing.T) {
	p := &mockProvider{
		name: "mock",
		responses: []CompletionResponse{
			{Content: "not valid json"},
			{Content: `{"name":"retry","value":99}`},
		},
	}

	result, err := CompleteJSON[testResult](context.Background(), p, CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != "retry" || result.Value != 99 {
		t.Errorf("unexpected result: %+v", result)
	}
	if p.callCount != 2 {
		t.Errorf("expected 2 calls, got %d", p.callCount)
	}
}

func TestCompleteJSONRetryFail(t *testing.T) {
	p := &mockProvider{
		name: "mock",
		responses: []CompletionResponse{
			{Content: "garbage"},
			{Content: "more garbage"},
		},
	}

	_, err := CompleteJSON[testResult](context.Background(), p, CompletionRequest{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCompleteJSONProviderError(t *testing.T) {
	p := &mockProvider{
		name:   "mock",
		errors: []error{fmt.Errorf("connection refused")},
	}

	_, err := CompleteJSON[testResult](context.Background(), p, CompletionRequest{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
