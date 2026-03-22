package sources

import (
	"context"
	"testing"
	"time"

	"github.com/shaiknoorullah/wtfrc/internal/coach"
)

func TestParseHyprlandEvent(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    *coach.Event // nil means skip
		wantAct string
		wantCtx string
	}{
		{
			name:    "activewindow",
			line:    "activewindow>>kitty,vim",
			wantAct: "activewindow",
			wantCtx: "kitty,vim",
		},
		{
			name:    "openwindow",
			line:    "openwindow>>abc123,1,kitty,terminal",
			wantAct: "openwindow",
			wantCtx: "abc123,1,kitty,terminal",
		},
		{
			name:    "movewindow",
			line:    "movewindow>>abc123,2",
			wantAct: "movewindow",
			wantCtx: "abc123,2",
		},
		{
			name:    "fullscreen on",
			line:    "fullscreen>>1",
			wantAct: "fullscreen",
			wantCtx: "1",
		},
		{
			name:    "fullscreen off",
			line:    "fullscreen>>0",
			wantAct: "fullscreen",
			wantCtx: "0",
		},
		{
			name: "configreloaded not in capture list",
			line: "configreloaded>>",
			want: nil,
		},
		{
			name: "empty line skip",
			line: "",
			want: nil,
		},
		{
			name: "malformed no separator",
			line: "malformed",
			want: nil,
		},
		{
			name: "unknown event skip",
			line: "workspace>>5",
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseHyprlandEvent(tc.line)
			if tc.want == nil && tc.wantAct == "" {
				// expect skip
				if ok {
					t.Errorf("expected skip, got Event{Action:%q, Context:%q}", got.Action, got.Context)
				}
				return
			}
			if !ok {
				t.Fatalf("expected event, got skip")
			}
			if got.Source != coach.SourceHyprland {
				t.Errorf("Source = %q, want %q", got.Source, coach.SourceHyprland)
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

func TestHyprlandNoSignature(t *testing.T) {
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	events := make(chan coach.Event, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunHyprlandOptional(ctx, events)
	}()

	select {
	case <-done:
		// returned immediately as expected
	case <-time.After(500 * time.Millisecond):
		t.Fatal("RunHyprlandOptional did not return immediately when HYPRLAND_INSTANCE_SIGNATURE is unset")
	}
}
