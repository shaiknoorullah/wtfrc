package llm

import "context"

type Provider interface {
	Name() string
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
	Stream(ctx context.Context, req CompletionRequest) (<-chan string, error)
	HealthCheck(ctx context.Context) error
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ResponseFormat string

const (
	FormatText ResponseFormat = "text"
	FormatJSON ResponseFormat = "json"
)

type CompletionRequest struct {
	System         string
	Messages       []Message
	MaxTokens      int
	Temperature    float64
	ResponseFormat ResponseFormat
}

type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
}

type CompletionResponse struct {
	Content   string
	Model     string
	Usage     TokenUsage
	LatencyMs int64
}
