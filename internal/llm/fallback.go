package llm

import (
	"context"
	"fmt"
)

// FallbackProvider tries the Primary provider first, falling back to Fallback on error.
type FallbackProvider struct {
	Primary  Provider
	Fallback Provider // may be nil
}

func (f *FallbackProvider) Name() string {
	if f.Fallback != nil {
		return fmt.Sprintf("%s->%s", f.Primary.Name(), f.Fallback.Name())
	}
	return f.Primary.Name()
}

func (f *FallbackProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	resp, err := f.Primary.Complete(ctx, req)
	if err == nil {
		return resp, nil
	}
	if f.Fallback != nil {
		resp, fbErr := f.Fallback.Complete(ctx, req)
		if fbErr == nil {
			return resp, nil
		}
		return CompletionResponse{}, fmt.Errorf("all providers failed: %s (%v), %s (%v)",
			f.Primary.Name(), err, f.Fallback.Name(), fbErr)
	}
	return CompletionResponse{}, fmt.Errorf("provider %s unavailable: %w", f.Primary.Name(), err)
}

func (f *FallbackProvider) Stream(ctx context.Context, req CompletionRequest) (<-chan string, error) {
	ch, err := f.Primary.Stream(ctx, req)
	if err == nil {
		return ch, nil
	}
	if f.Fallback != nil {
		ch, fbErr := f.Fallback.Stream(ctx, req)
		if fbErr == nil {
			return ch, nil
		}
		return nil, fmt.Errorf("all providers failed: %s (%v), %s (%v)",
			f.Primary.Name(), err, f.Fallback.Name(), fbErr)
	}
	return nil, fmt.Errorf("provider %s unavailable: %w", f.Primary.Name(), err)
}

func (f *FallbackProvider) HealthCheck(ctx context.Context) error {
	if err := f.Primary.HealthCheck(ctx); err == nil {
		return nil
	}
	if f.Fallback != nil {
		return f.Fallback.HealthCheck(ctx)
	}
	return fmt.Errorf("provider %s health check failed", f.Primary.Name())
}
