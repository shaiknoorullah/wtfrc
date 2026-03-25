package sources

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/coach"
)

func TestParseYaziEvent(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantOk  bool
		wantAct string
		wantCtx string
	}{
		{
			name:    "cd event",
			line:    "cd /home/user/projects",
			wantOk:  true,
			wantAct: "cd",
			wantCtx: "/home/user/projects",
		},
		{
			name:    "hover event",
			line:    "hover somefile.txt",
			wantOk:  true,
			wantAct: "hover",
			wantCtx: "somefile.txt",
		},
		{
			name:    "cd with no data",
			line:    "cd",
			wantOk:  true,
			wantAct: "cd",
			wantCtx: "",
		},
		{
			name:   "empty line skip",
			line:   "",
			wantOk: false,
		},
		{
			name:   "unknown event type skip",
			line:   "rename foo bar",
			wantOk: false,
		},
		{
			name:    "hover with path containing spaces",
			line:    "hover my file with spaces.txt",
			wantOk:  true,
			wantAct: "hover",
			wantCtx: "my file with spaces.txt",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseYaziEvent(tc.line)
			if ok != tc.wantOk {
				t.Fatalf("parseYaziEvent(%q) ok=%v, want %v", tc.line, ok, tc.wantOk)
			}
			if !tc.wantOk {
				return
			}
			if got.Source != coach.SourceYazi {
				t.Errorf("Source = %q, want %q", got.Source, coach.SourceYazi)
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

func TestYaziNoBinary(t *testing.T) {
	tmpDir := t.TempDir()
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir)
	defer func() { _ = os.Setenv("PATH", origPath) }()

	_ = filepath.Join(tmpDir, "ya")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	events := make(chan coach.Event, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunYaziOptional(ctx, events)
	}()

	select {
	case <-done:
		// returned immediately as expected
	case <-time.After(500 * time.Millisecond):
		t.Fatal("RunYaziOptional did not return immediately when ya binary is not found")
	}
}
