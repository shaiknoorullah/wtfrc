package parsers

import (
	"bufio"
	"os"
	"strings"
)

// GenericParser is a line-by-line fallback parser. Each non-blank, non-comment
// line becomes an EntrySetting entry. It is NOT registered via init(); callers
// use it explicitly as a last resort when no other parser matches.
type GenericParser struct{}

func (p *GenericParser) Name() string { return "generic" }

func (p *GenericParser) CanParse(_ string) bool { return true }

func (p *GenericParser) Parse(path string) ([]RawEntry, error) {
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
		line := strings.TrimSpace(scanner.Text())

		// Skip blank lines and common comment styles.
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "//") {
			continue
		}

		entries = append(entries, RawEntry{
			Tool:       "generic",
			Type:       EntrySetting,
			RawBinding: line,
			RawAction:  "",
			SourceFile: path,
			SourceLine: lineNum,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}
