package parsers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNvimParserCanParse(t *testing.T) {
	p := &NvimParser{}

	shouldMatch := []string{
		"/home/user/.config/nvim/init.lua",
		"/home/user/.config/nvim/lua/keymaps.lua",
		"/home/user/.config/nvim/plugin/mappings.lua",
		"/etc/xdg/nvim/init.lua",
	}
	for _, path := range shouldMatch {
		if !p.CanParse(path) {
			t.Errorf("CanParse(%q) = false, want true", path)
		}
	}

	shouldNotMatch := []string{
		"/home/user/.config/nvim/init.vim",
		"/home/user/.bashrc",
		"/home/user/.config/awesome/rc.lua",
		"/home/user/nvim",
		"/home/user/test.lua",
		"/home/user/.config/wezterm/wezterm.lua",
	}
	for _, path := range shouldNotMatch {
		if p.CanParse(path) {
			t.Errorf("CanParse(%q) = true, want false", path)
		}
	}
}

func TestNvimParserName(t *testing.T) {
	p := &NvimParser{}
	if got := p.Name(); got != "nvim" {
		t.Errorf("Name() = %q, want %q", got, "nvim")
	}
}

func TestNvimParserParse(t *testing.T) {
	content := `-- Neovim keymaps
local opts = { noremap = true, silent = true }

vim.keymap.set("n", "<leader>ff", "<cmd>Telescope find_files<cr>", opts)
vim.keymap.set("n", "<leader>fg", "<cmd>Telescope live_grep<cr>", opts)
vim.keymap.set("v", "<leader>y", '"+y', { desc = "Yank to clipboard" })

-- Some other config
vim.opt.number = true
vim.opt.relativenumber = true
`

	dir := t.TempDir()
	nvimDir := filepath.Join(dir, "nvim", "lua")
	if err := os.MkdirAll(nvimDir, 0o755); err != nil {
		t.Fatalf("failed to create nvim dir: %v", err)
	}
	confPath := filepath.Join(nvimDir, "keymaps.lua")
	if err := os.WriteFile(confPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	p := &NvimParser{}
	entries, err := p.Parse(confPath)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if got := len(entries); got != 3 {
		t.Fatalf("got %d entries, want 3", got)
	}

	// Verify first entry: vim.keymap.set("n", "<leader>ff", ...)
	first := entries[0]
	if first.RawBinding != "<leader>ff" {
		t.Errorf("entries[0].RawBinding = %q, want %q", first.RawBinding, "<leader>ff")
	}
	if first.RawAction != "<cmd>Telescope find_files<cr>" {
		t.Errorf("entries[0].RawAction = %q, want %q", first.RawAction, "<cmd>Telescope find_files<cr>")
	}
	if first.Type != EntryKeybind {
		t.Errorf("entries[0].Type = %q, want %q", first.Type, EntryKeybind)
	}
	if first.Tool != "nvim" {
		t.Errorf("entries[0].Tool = %q, want %q", first.Tool, "nvim")
	}
	if first.SourceFile != confPath {
		t.Errorf("entries[0].SourceFile = %q, want %q", first.SourceFile, confPath)
	}

	// Verify second entry
	second := entries[1]
	if second.RawBinding != "<leader>fg" {
		t.Errorf("entries[1].RawBinding = %q, want %q", second.RawBinding, "<leader>fg")
	}
	if second.RawAction != "<cmd>Telescope live_grep<cr>" {
		t.Errorf("entries[1].RawAction = %q, want %q", second.RawAction, "<cmd>Telescope live_grep<cr>")
	}

	// Verify third entry (single-quoted action)
	third := entries[2]
	if third.RawBinding != "<leader>y" {
		t.Errorf("entries[2].RawBinding = %q, want %q", third.RawBinding, "<leader>y")
	}
	if third.RawAction != `"+y` {
		t.Errorf("entries[2].RawAction = %q, want %q", third.RawAction, `"+y`)
	}

	// All entries should be keybinds with tool "nvim"
	for i, e := range entries {
		if e.Tool != "nvim" {
			t.Errorf("entries[%d].Tool = %q, want %q", i, e.Tool, "nvim")
		}
		if e.Type != EntryKeybind {
			t.Errorf("entries[%d].Type = %q, want %q", i, e.Type, EntryKeybind)
		}
	}
}
