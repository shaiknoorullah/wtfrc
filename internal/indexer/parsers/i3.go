package parsers

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

func init() {
	Register(&I3Parser{})
}

// I3Parser extracts keybindings from i3 and sway config files.
type I3Parser struct{}

var (
	bindsymRe  = regexp.MustCompile(`^\s*bindsym\s+(\S+)\s+(.+)$`)
	bindcodeRe = regexp.MustCompile(`^\s*bindcode\s+(\S+)\s+(.+)$`)
)

func (p *I3Parser) Name() string { return "i3" }

func (p *I3Parser) CanParse(path string) bool {
	return strings.HasSuffix(path, "/i3/config") || strings.HasSuffix(path, "/sway/config")
}

func (p *I3Parser) Parse(path string) ([]RawEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	tool := toolFromPath(path)

	// Read all lines into memory so we can build context windows.
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

		var binding, action string
		if m := bindsymRe.FindStringSubmatch(line); m != nil {
			binding = m[1]
			action = m[2]
		} else if m := bindcodeRe.FindStringSubmatch(line); m != nil {
			binding = m[1]
			action = m[2]
		} else {
			continue
		}

		entries = append(entries, RawEntry{
			Tool:         tool,
			Type:         EntryKeybind,
			RawBinding:   binding,
			RawAction:    strings.TrimSpace(action),
			SourceFile:   path,
			SourceLine:   i + 1, // 1-based
			ContextLines: contextWindow(lines, i, 3),
		})
	}

	return entries, nil
}

// toolFromPath returns "sway" if the path contains /sway/config, otherwise "i3".
func toolFromPath(path string) string {
	if strings.HasSuffix(path, "/sway/config") {
		return "sway"
	}
	return "i3"
}

// contextWindow returns up to n lines before and after index i, joined with newlines.
func contextWindow(lines []string, i, n int) string {
	start := i - n
	if start < 0 {
		start = 0
	}
	end := i + n + 1
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start:end], "\n")
}
