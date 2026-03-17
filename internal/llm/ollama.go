package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// OllamaProvider implements Provider for the Ollama local LLM runtime.
type OllamaProvider struct {
	BaseURL string
	Model   string
	client  *http.Client
}

// NewOllama creates an OllamaProvider pointed at the given base URL and model.
func NewOllama(baseURL, model string) *OllamaProvider {
	return &OllamaProvider{
		BaseURL: baseURL,
		Model:   model,
		client:  &http.Client{},
	}
}

func (o *OllamaProvider) Name() string {
	return "ollama"
}

// ollamaChatRequest is the JSON body sent to /api/chat.
type ollamaChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

// ollamaChatResponse is the JSON returned from a non-streaming /api/chat call.
type ollamaChatResponse struct {
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Model         string `json:"model"`
	Done          bool   `json:"done"`
	TotalDuration int64  `json:"total_duration"`
}

// ollamaStreamChunk is one NDJSON line from a streaming /api/chat call.
type ollamaStreamChunk struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

func (o *OllamaProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	start := time.Now()

	msgs := buildMessages(req)

	payload := ollamaChatRequest{
		Model:    o.Model,
		Messages: msgs,
		Stream:   false,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("ollama: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("ollama: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := o.client.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("ollama: do request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return CompletionResponse{}, fmt.Errorf("ollama: unexpected status %d", httpResp.StatusCode)
	}

	var resp ollamaChatResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return CompletionResponse{}, fmt.Errorf("ollama: decode response: %w", err)
	}

	latency := time.Since(start).Milliseconds()

	return CompletionResponse{
		Content:   resp.Message.Content,
		Model:     resp.Model,
		LatencyMs: latency,
	}, nil
}

func (o *OllamaProvider) Stream(ctx context.Context, req CompletionRequest) (<-chan string, error) {
	msgs := buildMessages(req)

	payload := ollamaChatRequest{
		Model:    o.Model,
		Messages: msgs,
		Stream:   true,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("ollama: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama: do request: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		httpResp.Body.Close()
		return nil, fmt.Errorf("ollama: unexpected status %d", httpResp.StatusCode)
	}

	ch := make(chan string)

	go func() {
		defer close(ch)
		defer httpResp.Body.Close()

		scanner := bufio.NewScanner(httpResp.Body)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var chunk ollamaStreamChunk
			if err := json.Unmarshal(line, &chunk); err != nil {
				return
			}

			if chunk.Message.Content != "" {
				select {
				case ch <- chunk.Message.Content:
				case <-ctx.Done():
					return
				}
			}

			if chunk.Done {
				return
			}
		}
	}()

	return ch, nil
}

func (o *OllamaProvider) HealthCheck(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, o.BaseURL+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("ollama: create request: %w", err)
	}

	httpResp, err := o.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ollama: do request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama: health check failed with status %d", httpResp.StatusCode)
	}

	return nil
}

// buildMessages prepends a system message if req.System is set.
func buildMessages(req CompletionRequest) []Message {
	var msgs []Message
	if req.System != "" {
		msgs = append(msgs, Message{Role: "system", Content: req.System})
	}
	msgs = append(msgs, req.Messages...)
	return msgs
}
