package parsers

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func init() {
	Register(&SSHParser{})
}

// SSHParser extracts Host blocks from SSH configuration files.
type SSHParser struct{}

func (p *SSHParser) Name() string { return "ssh" }

func (p *SSHParser) CanParse(path string) bool {
	return strings.HasSuffix(path, "ssh/config") || strings.HasSuffix(path, ".ssh/config")
}

func (p *SSHParser) Parse(path string) ([]RawEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	type hostBlock struct {
		name     string
		hostname string
		user     string
		line     int
	}

	var entries []RawEntry
	var current *hostBlock
	scanner := bufio.NewScanner(f)
	lineNum := 0

	flush := func() {
		if current == nil {
			return
		}

		hostname := current.hostname
		user := current.user
		var action string

		switch {
		case hostname == "":
			// No hostname set (e.g. wildcard Host *)
			action = fmt.Sprintf("%s -> %s", current.name, current.name)
		case user != "":
			action = fmt.Sprintf("%s -> %s (%s@%s)", current.name, hostname, user, hostname)
		default:
			action = fmt.Sprintf("%s -> %s", current.name, hostname)
		}

		entries = append(entries, RawEntry{
			Tool:       "ssh",
			Type:       EntryHost,
			RawBinding: current.name,
			RawAction:  action,
			SourceFile: path,
			SourceLine: current.line,
		})
		current = nil
	}

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		keyword := strings.ToLower(fields[0])

		if keyword == "host" {
			flush()
			current = &hostBlock{
				name: fields[1],
				line: lineNum,
			}
			continue
		}

		if current == nil {
			continue
		}

		switch keyword {
		case "hostname":
			current.hostname = fields[1]
		case "user":
			current.user = fields[1]
		}
	}

	// Flush the last block.
	flush()

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}
