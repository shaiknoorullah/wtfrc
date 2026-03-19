package parsers

import (
	"encoding/json"
	"os"
	"strings"
)

func init() {
	Register(&VSCodeParser{})
}

// VSCodeParser extracts keybindings from VS Code keybindings.json files.
type VSCodeParser struct{}

type vscodeKeybinding struct {
	Key     string `json:"key"`
	Command string `json:"command"`
	When    string `json:"when,omitempty"`
}

func (p *VSCodeParser) Name() string { return "vscode" }

func (p *VSCodeParser) CanParse(path string) bool {
	return strings.HasSuffix(path, "keybindings.json")
}

func (p *VSCodeParser) Parse(path string) ([]RawEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var bindings []vscodeKeybinding
	if err := json.Unmarshal(data, &bindings); err != nil {
		return nil, err
	}

	var entries []RawEntry
	for i, b := range bindings {
		if b.Key == "" || b.Command == "" {
			continue
		}

		entries = append(entries, RawEntry{
			Tool:       "vscode",
			Type:       EntryKeybind,
			RawBinding: b.Key,
			RawAction:  b.Command,
			SourceFile: path,
			SourceLine: i + 1, // approximate line (JSON array index + 1)
		})
	}

	return entries, nil
}
