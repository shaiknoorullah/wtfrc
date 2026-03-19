package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAICompatProvider_Complete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key-123" {
			t.Errorf("expected Bearer test-key-123, got %s", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"id": "chatcmpl-abc",
			"object": "chat.completion",
			"model": "gpt-4",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "content": "Hello, world!"},
				"finish_reason": "stop"
			}],
			"usage": {
				"prompt_tokens": 10,
				"completion_tokens": 5
			}
		}`)
	}))
	defer srv.Close()

	t.Setenv("TEST_API_KEY", "test-key-123")
	p := NewOpenAICompat(srv.URL, "gpt-4", "TEST_API_KEY")

	if p.Name() != "openai-compat" {
		t.Fatalf("expected name openai-compat, got %s", p.Name())
	}

	resp, err := p.Complete(context.Background(), CompletionRequest{
		System:      "You are helpful.",
		Messages:    []Message{{Role: "user", Content: "Hi"}},
		MaxTokens:   100,
		Temperature: 0.7,
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	if resp.Content != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", resp.Content)
	}
	if resp.Model != "gpt-4" {
		t.Errorf("expected model gpt-4, got %s", resp.Model)
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("expected 10 prompt tokens, got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 5 {
		t.Errorf("expected 5 completion tokens, got %d", resp.Usage.CompletionTokens)
	}
	if resp.LatencyMs <= 0 {
		t.Errorf("expected positive latency, got %d", resp.LatencyMs)
	}
}

func TestOpenAICompatProvider_Complete_JSONFormat(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"id": "chatcmpl-abc",
			"model": "gpt-4",
			"choices": [{"message": {"role": "assistant", "content": "{\"key\":\"val\"}"}}],
			"usage": {"prompt_tokens": 5, "completion_tokens": 3}
		}`)
	}))
	defer srv.Close()

	t.Setenv("TEST_API_KEY", "test-key-123")
	p := NewOpenAICompat(srv.URL, "gpt-4", "TEST_API_KEY")

	_, err := p.Complete(context.Background(), CompletionRequest{
		Messages:       []Message{{Role: "user", Content: "Give JSON"}},
		ResponseFormat: FormatJSON,
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	if !strings.Contains(gotBody, `"type":"json_object"`) {
		t.Errorf("expected response_format json_object in request body, got: %s", gotBody)
	}
}

func TestOpenAICompatProvider_Complete_SystemMessage(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"id": "chatcmpl-abc",
			"model": "gpt-4",
			"choices": [{"message": {"role": "assistant", "content": "ok"}}],
			"usage": {"prompt_tokens": 5, "completion_tokens": 1}
		}`)
	}))
	defer srv.Close()

	t.Setenv("TEST_API_KEY", "test-key-123")
	p := NewOpenAICompat(srv.URL, "gpt-4", "TEST_API_KEY")

	_, err := p.Complete(context.Background(), CompletionRequest{
		System:   "Be concise.",
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	// The system message should appear before the user message
	sysIdx := strings.Index(gotBody, `"role":"system"`)
	userIdx := strings.Index(gotBody, `"role":"user"`)
	if sysIdx < 0 {
		t.Fatalf("expected system role in body, got: %s", gotBody)
	}
	if sysIdx >= userIdx {
		t.Errorf("system message should precede user message in body")
	}
}

func TestOpenAICompatProvider_Stream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello \"}}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	t.Setenv("TEST_API_KEY", "test-key-123")
	p := NewOpenAICompat(srv.URL, "gpt-4", "TEST_API_KEY")

	ch, err := p.Stream(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var tokens []string
	for tok := range ch {
		tokens = append(tokens, tok)
	}

	got := strings.Join(tokens, "")
	if got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}
	if len(tokens) != 2 {
		t.Errorf("expected 2 tokens, got %d: %v", len(tokens), tokens)
	}
}

func TestOpenAICompatProvider_Stream_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		// Send one token, then hang
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		flusher.Flush()
		// Block until client disconnects
		<-r.Context().Done()
	}))
	defer srv.Close()

	t.Setenv("TEST_API_KEY", "test-key-123")
	p := NewOpenAICompat(srv.URL, "gpt-4", "TEST_API_KEY")

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := p.Stream(ctx, CompletionRequest{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	// Read first token
	tok := <-ch
	if tok != "partial" {
		t.Errorf("expected 'partial', got %q", tok)
	}

	// Cancel and ensure channel closes
	cancel()
	for range ch {
		// drain
	}
}

func TestOpenAICompatProvider_HealthCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[]}`)
	}))
	defer srv.Close()

	t.Setenv("TEST_API_KEY", "test-key-123")
	p := NewOpenAICompat(srv.URL, "gpt-4", "TEST_API_KEY")

	err := p.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}
}

func TestOpenAICompatProvider_HealthCheck_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	t.Setenv("TEST_API_KEY", "test-key-123")
	p := NewOpenAICompat(srv.URL, "gpt-4", "TEST_API_KEY")

	err := p.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected HealthCheck to fail for 500 response")
	}
}

func TestOpenAICompatProvider_Complete_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"message":"rate limited"}}`)
	}))
	defer srv.Close()

	t.Setenv("TEST_API_KEY", "test-key-123")
	p := NewOpenAICompat(srv.URL, "gpt-4", "TEST_API_KEY")

	_, err := p.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
}

func TestOpenAICompatProvider_ImplementsInterface(t *testing.T) {
	var _ Provider = (*OpenAICompatProvider)(nil)
}
