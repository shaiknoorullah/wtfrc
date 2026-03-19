package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOllamaComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/chat" {
			t.Errorf("expected /api/chat, got %s", r.URL.Path)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		if body["model"] != "gemma3:4b" {
			t.Errorf("expected model gemma3:4b, got %v", body["model"])
		}
		if body["stream"] != false {
			t.Errorf("expected stream false, got %v", body["stream"])
		}

		msgs, ok := body["messages"].([]any)
		if !ok {
			t.Fatalf("messages is not an array: %T", body["messages"])
		}
		// Should have system message + user message = 2
		if len(msgs) != 2 {
			t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
		}
		first := msgs[0].(map[string]any)
		if first["role"] != "system" {
			t.Errorf("expected first message role system, got %v", first["role"])
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"message":{"role":"assistant","content":"hello world"},"model":"gemma3:4b","done":true,"total_duration":1000000000}`)
	}))
	defer srv.Close()

	p := NewOllama(srv.URL, "gemma3:4b")

	if p.Name() != "ollama" {
		t.Fatalf("expected name ollama, got %s", p.Name())
	}

	resp, err := p.Complete(context.Background(), CompletionRequest{
		System:   "you are a helper",
		Messages: []Message{{Role: "user", Content: "say hello"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	if resp.Content != "hello world" {
		t.Errorf("expected content 'hello world', got %q", resp.Content)
	}
	if resp.Model != "gemma3:4b" {
		t.Errorf("expected model gemma3:4b, got %s", resp.Model)
	}
	if resp.LatencyMs < 0 {
		t.Errorf("expected non-negative latency, got %d", resp.LatencyMs)
	}
}

func TestOllamaCompleteNoSystem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		msgs := body["messages"].([]any)
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message (no system), got %d", len(msgs))
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"message":{"role":"assistant","content":"ok"},"model":"gemma3:4b","done":true,"total_duration":500000000}`)
	}))
	defer srv.Close()

	p := NewOllama(srv.URL, "gemma3:4b")
	resp, err := p.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("expected content 'ok', got %q", resp.Content)
	}
}

func TestOllamaStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/chat" {
			t.Errorf("expected /api/chat, got %s", r.URL.Path)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		if body["stream"] != true {
			t.Errorf("expected stream true, got %v", body["stream"])
		}

		w.Header().Set("Content-Type", "application/x-ndjson")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not implement Flusher")
		}

		lines := []string{
			`{"message":{"content":"hello "},"done":false}`,
			`{"message":{"content":"world"},"done":true}`,
		}
		for _, line := range lines {
			fmt.Fprintln(w, line)
			flusher.Flush()
		}
	}))
	defer srv.Close()

	p := NewOllama(srv.URL, "gemma3:4b")
	ch, err := p.Stream(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "say hello"}},
	})
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}

	var tokens []string
	for tok := range ch {
		tokens = append(tokens, tok)
	}

	got := strings.Join(tokens, "")
	if got != "hello world" {
		t.Errorf("expected concatenated stream 'hello world', got %q", got)
	}
}

func TestOllamaStreamContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		flusher, _ := w.(http.Flusher)
		// Write one chunk, then hang (simulating a slow server)
		fmt.Fprintln(w, `{"message":{"content":"start"},"done":false}`)
		flusher.Flush()
		// Block until the client goes away
		<-r.Context().Done()
	}))
	defer srv.Close()

	p := NewOllama(srv.URL, "gemma3:4b")
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := p.Stream(ctx, CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}

	// Read the first token
	tok, ok := <-ch
	if !ok || tok != "start" {
		t.Fatalf("expected first token 'start', got %q (ok=%v)", tok, ok)
	}

	// Cancel and verify the channel closes
	cancel()
	for range ch {
		// drain
	}
	// If we get here, the channel was closed — test passes
}

func TestOllamaHealthCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/tags" {
			t.Errorf("expected /api/tags, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"models":[]}`)
	}))
	defer srv.Close()

	p := NewOllama(srv.URL, "gemma3:4b")
	err := p.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("HealthCheck returned error: %v", err)
	}
}

func TestOllamaHealthCheckFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, "service down")
	}))
	defer srv.Close()

	p := NewOllama(srv.URL, "gemma3:4b")
	err := p.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected HealthCheck to return error for non-200 status")
	}
}

func TestOllamaCompleteServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "internal error")
	}))
	defer srv.Close()

	p := NewOllama(srv.URL, "gemma3:4b")
	_, err := p.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected Complete to return error for 500 status")
	}
}

func TestOllamaImplementsProvider(t *testing.T) {
	var _ Provider = (*OllamaProvider)(nil)
}
