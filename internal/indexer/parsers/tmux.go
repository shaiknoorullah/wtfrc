package parsers

import (
	"bufio"
	"os"
	"strings"
)

func init() {
	Register(&TmuxParser{})
}

// TmuxParser extracts keybindings from tmux configuration files.
type TmuxParser struct{}

func (p *TmuxParser) Name() string { return "tmux" }

func (p *TmuxParser) CanParse(path string) bool {
	return strings.HasSuffix(path, "tmux.conf") || strings.HasSuffix(path, ".tmux.conf")
}

func (p *TmuxParser) Parse(path string) ([]RawEntry, error) {
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

		// Skip empty lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		// Only handle bind / bind-key directives.
		if fields[0] != "bind" && fields[0] != "bind-key" {
			continue
		}

		// Walk past optional flags to find the key token.
		// Recognised flags: -r, -n (no argument), -T <table> (consumes next token).
		i := 1
		for i < len(fields) {
			tok := fields[i]
			if tok == "-r" || tok == "-n" {
				i++
				continue
			}
			if tok == "-T" {
				i += 2 // skip -T and its argument
				continue
			}
			// Any other flag starting with '-' that we don't know: stop.
			break
		}

		if i >= len(fields) {
			continue // no key found
		}

		key := fields[i]
		action := ""
		if i+1 < len(fields) {
			action = strings.Join(fields[i+1:], " ")
		}

		entries = append(entries, RawEntry{
			Tool:       "tmux",
			Type:       EntryKeybind,
			RawBinding: key,
			RawAction:  action,
			SourceFile: path,
			SourceLine: lineNum,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}
