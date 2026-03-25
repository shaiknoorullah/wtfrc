package sources

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/coach"
)

func TestParseTmuxEvent(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantOk  bool
		wantAct string
		wantCtx string
	}{
		{
			name:    "window-pane-changed",
			line:    "%window-pane-changed @1 %3",
			wantOk:  true,
			wantAct: "pane-changed",
			wantCtx: "@1 %3",
		},
		{
			name:    "session-window-changed",
			line:    "%session-window-changed $main @2",
			wantOk:  true,
			wantAct: "window-changed",
			wantCtx: "$main @2",
		},
		{
			name:   "begin notification skip",
			line:   "%begin 1234",
			wantOk: false,
		},
		{
			name:   "regular output skip",
			line:   "regular output",
			wantOk: false,
		},
		{
			name:   "empty line skip",
			line:   "",
			wantOk: false,
		},
		{
			name:   "other percent notification skip",
			line:   "%exit reason",
			wantOk: false,
		},
		{
			name:   "end notification skip",
			line:   "%end 1234",
			wantOk: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseTmuxEvent(tc.line)
			if ok != tc.wantOk {
				t.Fatalf("parseTmuxEvent(%q) ok=%v, want %v", tc.line, ok, tc.wantOk)
			}
			if !tc.wantOk {
				return
			}
			if got.Source != coach.SourceTmux {
				t.Errorf("Source = %q, want %q", got.Source, coach.SourceTmux)
			}
			if got.Action != tc.wantAct {
				t.Errorf("Action = %q, want %q", got.Action, tc.wantAct)
			}
			if got.Context != tc.wantCtx {
				t.Errorf("Context = %q, want %q", got.Context, tc.wantCtx)
			}
		})
	}
}

func TestTmuxNoBinary(t *testing.T) {
	// Point PATH to an empty temp dir so tmux cannot be found.
	tmpDir := t.TempDir()
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir)
	defer func() { _ = os.Setenv("PATH", origPath) }()

	// Make sure there's nothing named tmux in the temp dir.
	_ = filepath.Join(tmpDir, "tmux")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	events := make(chan coach.Event, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunTmuxOptional(ctx, events)
	}()

	select {
	case <-done:
		// returned immediately as expected
	case <-time.After(500 * time.Millisecond):
		t.Fatal("RunTmuxOptional did not return immediately when tmux binary is not found")
	}
}
