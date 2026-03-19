<p align="center">
  <h1 align="center">wtfrc</h1>
  <p align="center"><b>AI that reads your dotfiles so you don't have to read your dotfiles.</b></p>
</p>

<p align="center">
  <a href="https://github.com/shaiknoorullah/wtfrc/actions/workflows/ci.yml"><img src="https://github.com/shaiknoorullah/wtfrc/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/shaiknoorullah/wtfrc/releases/latest"><img src="https://img.shields.io/github/v/release/shaiknoorullah/wtfrc" alt="Release"></a>
  <a href="https://pkg.go.dev/github.com/shaiknoorullah/wtfrc"><img src="https://pkg.go.dev/badge/github.com/shaiknoorullah/wtfrc.svg" alt="Go Reference"></a>
  <a href="https://goreportcard.com/report/github.com/shaiknoorullah/wtfrc"><img src="https://goreportcard.com/badge/github.com/shaiknoorullah/wtfrc" alt="Go Report Card"></a>
  <a href="./LICENSE"><img src="https://img.shields.io/github/license/shaiknoorullah/wtfrc" alt="License"></a>
</p>

<!-- TODO: Record hero GIF with VHS -->

---

You spent 3 hours ricing your i3 config. You added 47 keybindings to kitty. You wrote shell aliases for things you'll use "all the time." That was six months ago. You remember maybe four of them.

**wtfrc** indexes your dotfiles, configs, and keybindings into a local knowledge base and answers questions about your own setup — because you clearly aren't going to read those files yourself.

No cloud. No telemetry. Everything runs locally via Ollama. Your configs never leave your machine.

## The Problem

You know the cycle:

1. **Hyperfocus burst** — spend an entire weekend configuring the perfect system
2. **Partial adoption** — actually use about 20% of what you set up
3. **Habit calcification** — develop inefficient habits that feel "good enough"
4. **Blind spots** — forget the better approach exists because you forgot you configured it
5. **Repeat** — discover a new tool, return to step 1

wtfrc breaks this cycle. It reads what you configured, remembers it for you, and (soon) roasts you when you do things the slow way.

## Features

- **Local-first** — single binary, zero cloud dependencies, runs entirely via Ollama
- **LLM-agnostic** — Ollama, Groq, DeepSeek, OpenRouter, or any OpenAI-compatible API
- **Privacy-first** — API keys, SSH paths, and secrets are automatically redacted before indexing
- **GPU-aware** — auto-detects your VRAM and recommends the best model for your hardware
- **Offline capable** — fallback chain handles unreachable providers gracefully
- **11 parsers** — i3/sway, Hyprland, tmux, kitty, zsh/bash, git, SSH, nvim, VS Code, systemd, plus a generic fallback
- **Semantic search** — FTS5-powered search across intents and descriptions, not just raw `grep`
- **Supervisor** — two-tier hallucination detection verifies answers against the knowledge base

## Install

```bash
# curl (Linux/macOS)
curl -sSL https://raw.githubusercontent.com/shaiknoorullah/wtfrc/main/scripts/install.sh | bash

# go install
go install github.com/shaiknoorullah/wtfrc/cmd/wtfrc@latest

# AUR (btw)
yay -S wtfrc-bin

# Homebrew
brew install shaiknoorullah/tap/wtfrc
```

## Quick Start

```bash
# First-run: detects GPU, picks the right model, indexes your configs
wtfrc setup

# Ask questions about your own setup
wtfrc ask "how do I close a window in i3?"
wtfrc ask "what are my shell aliases?"
wtfrc ask    # interactive REPL (arrow up for history, esc to quit)
```

## Commands

| Command | What it does |
|---------|-------------|
| `wtfrc setup` | First-run wizard — detects GPU, recommends model, indexes configs |
| `wtfrc index` | Index config files into the knowledge base |
| `wtfrc index --changed` | Only re-index files that changed |
| `wtfrc ask` | Interactive REPL with streaming answers |
| `wtfrc ask "query"` | One-shot answer |
| `wtfrc search "query"` | FTS search (no LLM, instant) |
| `wtfrc list` | List all indexed entries |
| `wtfrc list --tool tmux` | Filter by tool |
| `wtfrc stats` | Entry counts, sessions, query stats |
| `wtfrc doctor` | Health check + GPU model recommendation |
| `wtfrc config` | Open config in `$EDITOR` |
| `wtfrc config --init` | Generate default config |
| `wtfrc supervise` | Run hallucination review |
| `wtfrc supervise --report` | Show last supervisor report |

## GPU-Aware Model Selection

`wtfrc setup` detects your GPU and picks the best model for indexing:

| VRAM | Model | Notes |
|------|-------|-------|
| ≤4GB | `qwen2.5-coder:3b` | Integrated GPU / low-end |
| ≤8GB | `qwen2.5-coder:7b` | Sweet spot for most users |
| ≤16GB | `qwen2.5-coder:14b` | High-end consumer |
| 24GB+ | `qwen2.5-coder:32b` | Enthusiast |

The Qwen2.5-Coder family is specifically chosen for reliable JSON structured output and config/code understanding. You can also point the strong LLM at a cloud provider (DeepSeek, Groq, etc.) if you prefer.

## Window Manager Integration

### i3/sway

```
# ~/.config/i3/config
bindsym $mod+slash exec kitty --class=wtfrc -e wtfrc ask
for_window [app_id="wtfrc"] floating enable, resize set 800 600, move position center
```

### Hyprland

```
# ~/.config/hypr/hyprland.conf
bind = $mainMod, SLASH, exec, kitty --class wtfrc -e wtfrc ask
windowrulev2 = float, class:^(wtfrc)$
windowrulev2 = size 800 600, class:^(wtfrc)$
windowrulev2 = center, class:^(wtfrc)$
```

## Configuration

Config lives at `~/.config/wtfrc/config.toml`. `wtfrc setup` generates it automatically.

```toml
[llm.fast]
provider = "ollama"
model = "gemma3:4b"            # Used for ask (streaming answers)

[llm.strong]
provider = "ollama"
model = "qwen2.5-coder:7b"    # Used for indexing (JSON enrichment)
# Or use a cloud provider:
# provider = "openai-compat"
# model = "deepseek-chat"
# base_url = "https://api.deepseek.com"
# api_key_env = "DEEPSEEK_API_KEY"

[privacy]
redact_patterns = ["sk-", "xoxb-", "ghp_", "password", "secret", "token"]
never_index = ["~/.ssh/id_*", "~/.gnupg/", "~/.aws/credentials", "**/.env"]
```

## How It Works

1. **Parse** — 11 tool-specific parsers extract keybindings, aliases, functions, exports, hosts, and services from your config files
2. **Redact** — API keys, SSH paths, passwords, and tokens are stripped before anything touches an LLM
3. **Enrich** — a strong local LLM generates descriptions, search intents, and categories for each entry (with Ollama's JSON schema constraint for guaranteed valid output)
4. **Store** — everything goes into a local SQLite database with FTS5 full-text search indexes
5. **Ask** — your question is searched against intents and descriptions, top matches are fed to a fast LLM, and the answer streams back

## Roadmap

- **v0.1 Ask** — index + search + ask ← you are here
- **v0.2 Coach** — watches what you type, roasts you when you do it the slow way. Three modes: chill (friendly nudge), moderate (humorous roast), strict (blocks the command until you use the alias)
- **v1.0 Train** — tracks keybind adoption over time, generates weekly productivity reports, efficiency scoring per tool, QMK/VIAL keyboard layer tracking, ADHD-aware design with streak mechanics and novelty rotation

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## Security

See [SECURITY.md](SECURITY.md) for reporting vulnerabilities.

## License

[MIT](LICENSE)
