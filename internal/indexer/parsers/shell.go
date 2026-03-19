package parsers

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func init() {
	Register(&ShellParser{})
}

// ShellParser extracts aliases, exports, and functions from zsh/bash config files.
type ShellParser struct{}

var shellFiles = map[string]bool{
	".zshrc":        true,
	".bashrc":       true,
	".bash_profile": true,
	".zprofile":     true,
}

var (
	// alias k='kitty'
	aliasSingleQuoteRe = regexp.MustCompile(`^alias\s+([^=]+)='([^']*)'`)
	// alias k="kitty"
	aliasDoubleQuoteRe = regexp.MustCompile(`^alias\s+([^=]+)="([^"]*)"`)
	// export EDITOR=nvim
	exportRe = regexp.MustCompile(`^export\s+([^=]+)=(.+)$`)
	// function greet() {
	functionKeywordRe = regexp.MustCompile(`^function\s+(\w+)\s*\(`)
	// backup() {
	functionShorthandRe = regexp.MustCompile(`^(\w+)\s*\(\)\s*\{`)
)

func (p *ShellParser) Name() string { return "shell" }

func (p *ShellParser) CanParse(path string) bool {
	base := filepath.Base(path)
	return shellFiles[base]
}

func (p *ShellParser) Parse(path string) ([]RawEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	tool := toolFromFilename(filepath.Base(path))

	var entries []RawEntry
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if m := aliasSingleQuoteRe.FindStringSubmatch(line); m != nil {
			entries = append(entries, RawEntry{
				Tool:         tool,
				Type:         EntryAlias,
				RawBinding:   m[1],
				RawAction:    m[2],
				SourceFile:   path,
				SourceLine:   lineNum,
				ContextLines: line,
			})
		} else if m := aliasDoubleQuoteRe.FindStringSubmatch(line); m != nil {
			entries = append(entries, RawEntry{
				Tool:         tool,
				Type:         EntryAlias,
				RawBinding:   m[1],
				RawAction:    m[2],
				SourceFile:   path,
				SourceLine:   lineNum,
				ContextLines: line,
			})
		} else if m := exportRe.FindStringSubmatch(line); m != nil {
			entries = append(entries, RawEntry{
				Tool:         tool,
				Type:         EntryExport,
				RawBinding:   m[1],
				RawAction:    m[2],
				SourceFile:   path,
				SourceLine:   lineNum,
				ContextLines: line,
			})
		} else if m := functionKeywordRe.FindStringSubmatch(line); m != nil {
			entries = append(entries, RawEntry{
				Tool:         tool,
				Type:         EntryFunction,
				RawBinding:   m[1],
				RawAction:    "",
				SourceFile:   path,
				SourceLine:   lineNum,
				ContextLines: line,
			})
		} else if m := functionShorthandRe.FindStringSubmatch(line); m != nil {
			entries = append(entries, RawEntry{
				Tool:         tool,
				Type:         EntryFunction,
				RawBinding:   m[1],
				RawAction:    "",
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

func toolFromFilename(base string) string {
	switch base {
	case ".bashrc", ".bash_profile":
		return "bash"
	default:
		return "zsh"
	}
}
