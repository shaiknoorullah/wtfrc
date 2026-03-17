package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// OpenAICompatProvider implements Provider for any OpenAI-compatible API endpoint.
type OpenAICompatProvider struct {
	BaseURL string
	Model   string
	APIKey  string
	client  *http.Client
}

// NewOpenAICompat creates a new OpenAI-compatible provider. The apiKeyEnv parameter
// names the environment variable from which the API key is read.
func NewOpenAICompat(baseURL, model, apiKeyEnv string) *OpenAICompatProvider {
	return &OpenAICompatProvider{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Model:   model,
		APIKey:  os.Getenv(apiKeyEnv),
		client:  &http.Client{},
	}
}

// Name returns the provider name.
func (p *OpenAICompatProvider) Name() string {
	return "openai-compat"
}

// openaiRequest is the JSON body sent to /v1/chat/completions.
type openaiRequest struct {
	Model          string         `json:"model"`
	Messages       []Message      `json:"messages"`
	MaxTokens      int            `json:"max_tokens,omitempty"`
	Temperature    float64        `json:"temperature,omitempty"`
	Stream         bool           `json:"stream"`
	ResponseFormat *openaiRespFmt `json:"response_format,omitempty"`
}

type openaiRespFmt struct {
	Type string `json:"type"`
}

// openaiResponse is the JSON body returned from /v1/chat/completions (non-streaming).
type openaiResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// openaiStreamChunk is one SSE chunk from a streaming response.
type openaiStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

// Complete sends a non-streaming completion request and returns the response.
func (p *OpenAICompatProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	start := time.Now()

	body := p.buildRequestBody(req, false)
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("openai-compat: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.BaseURL+"/v1/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("openai-compat: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("openai-compat: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return CompletionResponse{}, fmt.Errorf("openai-compat: unexpected status %d", resp.StatusCode)
	}

	var oResp openaiResponse
	if err := json.NewDecoder(resp.Body).Decode(&oResp); err != nil {
		return CompletionResponse{}, fmt.Errorf("openai-compat: decode response: %w", err)
	}

	if len(oResp.Choices) == 0 {
		return CompletionResponse{}, fmt.Errorf("openai-compat: no choices in response")
	}

	elapsed := time.Since(start)
	latency := elapsed.Milliseconds()
	if latency == 0 && elapsed > 0 {
		latency = 1
	}

	return CompletionResponse{
		Content: oResp.Choices[0].Message.Content,
		Model:   oResp.Model,
		Usage: TokenUsage{
			PromptTokens:     oResp.Usage.PromptTokens,
			CompletionTokens: oResp.Usage.CompletionTokens,
		},
		LatencyMs: latency,
	}, nil
}

// Stream sends a streaming completion request and returns a channel of content tokens.
func (p *OpenAICompatProvider) Stream(ctx context.Context, req CompletionRequest) (<-chan string, error) {
	body := p.buildRequestBody(req, true)
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai-compat: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.BaseURL+"/v1/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("openai-compat: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai-compat: request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("openai-compat: unexpected status %d", resp.StatusCode)
	}

	ch := make(chan string)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return
			}

			var chunk openaiStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			content := chunk.Choices[0].Delta.Content
			if content == "" {
				continue
			}

			select {
			case ch <- content:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// HealthCheck verifies the API is reachable by hitting GET /v1/models.
func (p *OpenAICompatProvider) HealthCheck(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		p.BaseURL+"/v1/models", nil)
	if err != nil {
		return fmt.Errorf("openai-compat: create health request: %w", err)
	}
	if p.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("openai-compat: health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("openai-compat: health check returned status %d", resp.StatusCode)
	}

	return nil
}

// buildRequestBody constructs the OpenAI-format request body.
func (p *OpenAICompatProvider) buildRequestBody(req CompletionRequest, stream bool) openaiRequest {
	var msgs []Message
	if req.System != "" {
		msgs = append(msgs, Message{Role: "system", Content: req.System})
	}
	msgs = append(msgs, req.Messages...)

	body := openaiRequest{
		Model:       p.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      stream,
	}

	if req.ResponseFormat == FormatJSON {
		body.ResponseFormat = &openaiRespFmt{Type: "json_object"}
	}

	return body
}
