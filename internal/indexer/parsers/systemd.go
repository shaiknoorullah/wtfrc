package parsers

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	Register(&SystemdParser{})
}

// SystemdParser extracts service information from systemd .service unit files.
type SystemdParser struct{}

func (p *SystemdParser) Name() string { return "systemd" }

func (p *SystemdParser) CanParse(path string) bool {
	return strings.HasSuffix(path, ".service")
}

func (p *SystemdParser) Parse(path string) ([]RawEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var description, execStart string
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		if strings.HasPrefix(line, "Description=") {
			description = strings.TrimPrefix(line, "Description=")
		} else if strings.HasPrefix(line, "ExecStart=") {
			execStart = strings.TrimPrefix(line, "ExecStart=")
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Only emit an entry if there is an ExecStart directive.
	if execStart == "" {
		return nil, nil
	}

	// Fall back to the filename if Description is not set.
	if description == "" {
		description = filepath.Base(path)
	}

	return []RawEntry{
		{
			Tool:       "systemd",
			Type:       EntryService,
			RawBinding: description,
			RawAction:  execStart,
			SourceFile: path,
			SourceLine: 1,
		},
	}, nil
}
