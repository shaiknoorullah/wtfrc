package parsers

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

func init() {
	Register(&GitParser{})
}

// GitParser extracts aliases from git configuration files.
type GitParser struct{}

var (
	gitSectionRe = regexp.MustCompile(`^\s*\[(\S+)\]`)
	gitKVRe      = regexp.MustCompile(`^\s*(\S+)\s*=\s*(.+)$`)
)

func (p *GitParser) Name() string { return "git" }

func (p *GitParser) CanParse(path string) bool {
	return strings.HasSuffix(path, ".gitconfig") || strings.HasSuffix(path, "git/config")
}

func (p *GitParser) Parse(path string) ([]RawEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []RawEntry
	scanner := bufio.NewScanner(f)
	lineNum := 0
	inAlias := false

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}

		// Check for section headers.
		if m := gitSectionRe.FindStringSubmatch(trimmed); m != nil {
			inAlias = strings.EqualFold(m[1], "alias")
			continue
		}

		if !inAlias {
			continue
		}

		// Parse key = value inside [alias] section.
		if m := gitKVRe.FindStringSubmatch(trimmed); m != nil {
			entries = append(entries, RawEntry{
				Tool:         "git",
				Type:         EntryAlias,
				RawBinding:   strings.TrimSpace(m[1]),
				RawAction:    strings.TrimSpace(m[2]),
				SourceFile:   path,
				SourceLine:   lineNum,
				ContextLines: line,
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}
