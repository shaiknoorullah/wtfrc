package parsers

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

func init() {
	Register(&NvimParser{})
}

// NvimParser extracts keymaps from Neovim Lua configuration files.
type NvimParser struct{}

// nvimKeymapDoubleRe matches vim.keymap.set calls where the action is double-quoted.
// Example: vim.keymap.set("n", "<leader>ff", "<cmd>Telescope find_files<cr>", opts)
var nvimKeymapDoubleRe = regexp.MustCompile(
	`vim\.keymap\.set\(\s*["'](\w+)["']\s*,\s*["']([^"']+)["']\s*,\s*"([^"]+)"`,
)

// nvimKeymapSingleRe matches vim.keymap.set calls where the action is single-quoted.
// Example: vim.keymap.set("v", "<leader>y", '"+y', { desc = "Yank" })
var nvimKeymapSingleRe = regexp.MustCompile(
	`vim\.keymap\.set\(\s*["'](\w+)["']\s*,\s*["']([^"']+)["']\s*,\s*'([^']+)'`,
)

func (p *NvimParser) Name() string { return "nvim" }

func (p *NvimParser) CanParse(path string) bool {
	return strings.Contains(path, "nvim") && strings.HasSuffix(path, ".lua")
}

func (p *NvimParser) Parse(path string) ([]RawEntry, error) {
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
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}

		m := nvimKeymapDoubleRe.FindStringSubmatch(line)
		if m == nil {
			m = nvimKeymapSingleRe.FindStringSubmatch(line)
		}
		if m == nil {
			continue
		}

		// m[1] = mode, m[2] = key binding, m[3] = action
		entries = append(entries, RawEntry{
			Tool:         "nvim",
			Type:         EntryKeybind,
			RawBinding:   m[2],
			RawAction:    m[3],
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
