package coach

import (
	"fmt"
	"strings"
	"time"
)

// Source constants identify where a coaching event originated.
const (
	SourceShell       = "shell"
	SourceNvim        = "nvim"
	SourceHyprland    = "hyprland"
	SourceTmux        = "tmux"
	SourceKitty       = "kitty"
	SourceQutebrowser = "qutebrowser"
	SourceYazi        = "yazi"
	SourceEvdev       = "evdev"
)

// Event represents a single observed user action that may trigger coaching.
type Event struct {
	Source    string
	Action    string
	Context   string
	Timestamp time.Time
	IsKeybind bool
}

// ParseEvent parses a tab-delimited line in the format: source\taction\tcontext
// The context field is optional (2 or 3 fields accepted).
// If action starts with "kb:", IsKeybind is set to true and the prefix is stripped.
func ParseEvent(line string) (Event, error) {
	if line == "" {
		return Event{}, fmt.Errorf("empty input")
	}

	fields := strings.SplitN(line, "\t", 3)
	if len(fields) < 2 {
		return Event{}, fmt.Errorf("malformed event: expected at least 2 tab-separated fields, got %d", len(fields))
	}

	source := fields[0]
	action := fields[1]
	context := ""
	if len(fields) == 3 {
		context = fields[2]
	}

	if source == "" {
		return Event{}, fmt.Errorf("malformed event: empty source")
	}
	if action == "" {
		return Event{}, fmt.Errorf("malformed event: empty action")
	}

	isKeybind := false
	if strings.HasPrefix(action, "kb:") {
		isKeybind = true
		action = strings.TrimPrefix(action, "kb:")
	}

	return Event{
		Source:    source,
		Action:    action,
		Context:   context,
		Timestamp: time.Now(),
		IsKeybind: isKeybind,
	}, nil
}

// String serializes the event back to its tab-delimited wire format.
// If IsKeybind is true, the "kb:" prefix is prepended to the action.
func (e Event) String() string {
	action := e.Action
	if e.IsKeybind {
		action = "kb:" + action
	}
	return strings.Join([]string{e.Source, action, e.Context}, "\t")
}
