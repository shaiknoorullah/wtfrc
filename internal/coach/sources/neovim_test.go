package sources

import (
	"context"
	"testing"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/coach"
)

func TestNeovimRunOptional_NoEnvVar(t *testing.T) {
	t.Setenv("NVIM", "")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	events := make(chan coach.Event, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunNeovimOptional(ctx, events)
	}()

	select {
	case <-done:
		// returned promptly (stub behavior: returns immediately)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("RunNeovimOptional did not return promptly")
	}
}

func TestNeovimRunOptional_WithEnvVar(t *testing.T) {
	t.Setenv("NVIM", "/tmp/nvim.socket")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	events := make(chan coach.Event, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunNeovimOptional(ctx, events)
	}()

	select {
	case <-done:
		// stub returns immediately even when NVIM is set
	case <-time.After(500 * time.Millisecond):
		t.Fatal("RunNeovimOptional (stub) did not return promptly even with NVIM set")
	}
}
