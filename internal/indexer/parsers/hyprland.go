package parsers

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

func init() {
	Register(&HyprlandParser{})
}

// HyprlandParser extracts keybindings from Hyprland configuration files.
type HyprlandParser struct{}

// bindRe matches lines like:
//
//	bind = $mainMod, Q, killactive
//	bind = $mainMod SHIFT, E, exit
//	bind = $mainMod, Return, exec, kitty
//
// Capture group 1: everything after "bind =" up to the second comma (modifier + key).
// Capture group 2: everything after the second comma (action, possibly with sub-arguments).
var hyprBindRe = regexp.MustCompile(`^\s*bind\s*=\s*([^,]+,\s*[^,]+),\s*(.+)$`)

func (p *HyprlandParser) Name() string { return "hyprland" }

func (p *HyprlandParser) CanParse(path string) bool {
	return strings.HasSuffix(path, "hyprland.conf")
}

func (p *HyprlandParser) Parse(path string) ([]RawEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	var entries []RawEntry
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		m := hyprBindRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		binding := strings.TrimSpace(m[1])
		action := strings.TrimSpace(m[2])

		entries = append(entries, RawEntry{
			Tool:         "hyprland",
			Type:         EntryKeybind,
			RawBinding:   binding,
			RawAction:    action,
			SourceFile:   path,
			SourceLine:   i + 1,
			ContextLines: contextWindow(lines, i, 3),
		})
	}

	return entries, nil
}
