package coach

import (
	"testing"
	"time"
)

func TestParseEvent(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantSource  string
		wantAction  string
		wantContext string
		wantKeybind bool
		wantErr     bool
	}{
		{
			name:        "valid 3-field line",
			input:       "shell\tls -la\t/home/user",
			wantSource:  "shell",
			wantAction:  "ls -la",
			wantContext: "/home/user",
			wantKeybind: false,
			wantErr:     false,
		},
		{
			name:        "valid 2-field line no context",
			input:       "nvim\t:w",
			wantSource:  "nvim",
			wantAction:  ":w",
			wantContext: "",
			wantKeybind: false,
			wantErr:     false,
		},
		{
			name:        "keybind event",
			input:       "hyprland\tkb:hyprland:movefocus l\tworkspace1",
			wantSource:  "hyprland",
			wantAction:  "hyprland:movefocus l",
			wantContext: "workspace1",
			wantKeybind: true,
			wantErr:     false,
		},
		{
			name:    "malformed 1 field",
			input:   "shell",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "empty source",
			input:   "\tsome-action",
			wantErr: true,
		},
		{
			name:    "empty action",
			input:   "shell\t",
			wantErr: true,
		},
		{
			name:        "trailing newline stripped",
			input:       "shell\tls\t/home\n",
			wantSource:  "shell",
			wantAction:  "ls",
			wantContext: "/home",
			wantKeybind: false,
			wantErr:     false,
		},
		{
			name:        "trailing CRLF stripped",
			input:       "shell\tls\t/home\r\n",
			wantSource:  "shell",
			wantAction:  "ls",
			wantContext: "/home",
			wantKeybind: false,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := time.Now()
			e, err := ParseEvent(tt.input)
			after := time.Now()

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if e.Source != tt.wantSource {
				t.Errorf("Source: got %q, want %q", e.Source, tt.wantSource)
			}
			if e.Action != tt.wantAction {
				t.Errorf("Action: got %q, want %q", e.Action, tt.wantAction)
			}
			if e.Context != tt.wantContext {
				t.Errorf("Context: got %q, want %q", e.Context, tt.wantContext)
			}
			if e.IsKeybind != tt.wantKeybind {
				t.Errorf("IsKeybind: got %v, want %v", e.IsKeybind, tt.wantKeybind)
			}
			if e.Timestamp.Before(before) || e.Timestamp.After(after) {
				t.Errorf("Timestamp %v not in expected range [%v, %v]", e.Timestamp, before, after)
			}
		})
	}
}

func TestEventString(t *testing.T) {
	tests := []struct {
		name string
		e    Event
		want string
	}{
		{
			name: "normal event with context",
			e:    Event{Source: "shell", Action: "ls -la", Context: "/home/user", IsKeybind: false, Timestamp: time.Now()},
			want: "shell\tls -la\t/home/user",
		},
		{
			name: "normal event no context",
			e:    Event{Source: "nvim", Action: ":w", Context: "", IsKeybind: false, Timestamp: time.Now()},
			want: "nvim\t:w\t",
		},
		{
			name: "keybind event",
			e:    Event{Source: "hyprland", Action: "hyprland:movefocus l", Context: "workspace1", IsKeybind: true, Timestamp: time.Now()},
			want: "hyprland\tkb:hyprland:movefocus l\tworkspace1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.e.String()
			if got != tt.want {
				t.Errorf("String(): got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEventRoundTrip(t *testing.T) {
	originals := []Event{
		{Source: "shell", Action: "git status", Context: "/repo", IsKeybind: false},
		{Source: "hyprland", Action: "hyprland:movefocus l", Context: "", IsKeybind: true},
		{Source: "tmux", Action: "split-window", Context: "session1", IsKeybind: false},
	}

	for _, orig := range originals {
		serialized := orig.String()
		parsed, err := ParseEvent(serialized)
		if err != nil {
			t.Fatalf("ParseEvent(%q) round-trip failed: %v", serialized, err)
		}
		if parsed.Source != orig.Source {
			t.Errorf("Source: got %q, want %q", parsed.Source, orig.Source)
		}
		if parsed.Action != orig.Action {
			t.Errorf("Action: got %q, want %q", parsed.Action, orig.Action)
		}
		if parsed.Context != orig.Context {
			t.Errorf("Context: got %q, want %q", parsed.Context, orig.Context)
		}
		if parsed.IsKeybind != orig.IsKeybind {
			t.Errorf("IsKeybind: got %v, want %v", parsed.IsKeybind, orig.IsKeybind)
		}
	}
}
