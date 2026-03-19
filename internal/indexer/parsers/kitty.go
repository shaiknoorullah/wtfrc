package parsers

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

// mapDirectiveRe matches kitty "map <binding> <action>" lines.
var mapDirectiveRe = regexp.MustCompile(`^\s*map\s+(\S+)\s+(.+)`)

// KittyParser extracts keybindings from kitty.conf files.
type KittyParser struct{}

func init() {
	Register(&KittyParser{})
}

func (p *KittyParser) Name() string {
	return "kitty"
}

func (p *KittyParser) CanParse(path string) bool {
	return strings.HasSuffix(path, "kitty.conf")
}

func (p *KittyParser) Parse(path string) ([]RawEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []RawEntry
	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		matches := mapDirectiveRe.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		binding := matches[1]
		action := strings.Fields(matches[2])[0]

		entries = append(entries, RawEntry{
			Tool:         "kitty",
			Type:         EntryKeybind,
			RawBinding:   binding,
			RawAction:    action,
			SourceFile:   path,
			SourceLine:   lineNum,
			ContextLines: line,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}
