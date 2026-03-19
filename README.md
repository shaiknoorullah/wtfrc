# wtfrc

**AI that reads your dotfiles so you don't have to.**

<!-- TODO: Record hero GIF with VHS -->

wtfrc indexes your dotfiles, configs, and keybindings into a local semantic knowledge base, then answers natural-language questions about your setup using a local LLM. No cloud, no telemetry, no data leaves your machine.

## Features

- **Local-first** — your configs stay on your machine. Everything runs locally via Ollama.
- **LLM-agnostic** — works with Ollama, Groq, OpenRouter, or any OpenAI-compatible API.
- **Privacy-first** — API keys, SSH paths, and secrets are automatically redacted before indexing.
- **Offline capable** — fallback chain gracefully handles unreachable providers.
- **GPU-aware** — auto-detects VRAM and recommends the best local model for your hardware.
- **11 parsers** — i3/sway, Hyprland, tmux, kitty, zsh/bash, git, SSH, nvim, VS Code, systemd, plus a generic fallback.
- **Semantic search** — FTS5-powered search across intents and descriptions, not just raw text.
- **Supervisor** — built-in hallucination detection verifies answers against the knowledge base.

## Install

```bash
# curl
curl -sSL https://raw.githubusercontent.com/shaiknoorullah/wtfrc/main/scripts/install.sh | bash

# go install
go install github.com/shaiknoorullah/wtfrc/cmd/wtfrc@latest

# AUR
yay -S wtfrc-bin

# Homebrew
brew install shaiknoorullah/tap/wtfrc
```

## Quick Start

```bash
# First-run setup (detects GPU, recommends model, indexes configs)
wtfrc setup

# Or manually:
wtfrc index          # Index your config files
wtfrc ask            # Launch the interactive REPL
wtfrc ask "how do I close a window in i3?"   # One-shot mode
```

## Usage

```
wtfrc index             Index config files into the knowledge base
wtfrc index --changed   Only re-index changed files
wtfrc ask               Interactive REPL
wtfrc ask "query"       One-shot answer
wtfrc search "query"    FTS search (no LLM)
wtfrc list              List all indexed entries
wtfrc list --tool tmux  Filter by tool
wtfrc stats             Show index and session statistics
wtfrc doctor            Health check + GPU model recommendation
wtfrc config            Open config in $EDITOR
wtfrc config --init     Generate default config
wtfrc supervise         Run hallucination review
wtfrc supervise --report  Show last supervisor report
```

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

Config lives at `~/.config/wtfrc/config.toml`. Generate the default with `wtfrc config --init`. `wtfrc setup` auto-detects your GPU and picks the right model.

```toml
[llm.fast]
provider = "ollama"
model = "gemma3:4b"          # Used for ask (streaming answers)

[llm.strong]
provider = "ollama"
model = "qwen2.5-coder:7b"  # Used for indexing (JSON enrichment)
# Auto-selected by wtfrc setup based on your GPU:
#   ≤4GB VRAM  → qwen2.5-coder:3b
#   ≤8GB VRAM  → qwen2.5-coder:7b
#   ≤16GB VRAM → qwen2.5-coder:14b
#   24GB+ VRAM → qwen2.5-coder:32b
#
# Or use a cloud provider:
# provider = "openai-compat"
# model = "deepseek-chat"
# base_url = "https://api.deepseek.com"
# api_key_env = "DEEPSEEK_API_KEY"

[privacy]
redact_patterns = ["sk-", "xoxb-", "ghp_", "password", "secret", "token"]
never_index = ["~/.ssh/id_*", "~/.gnupg/", "~/.aws/credentials", "**/.env"]
```

## Roadmap

- **v0.1 Ask** — index + search + ask (you are here)
- **v0.2 Coach** — proactive keyboard shortcut coaching
- **v1.0 Train** — usage-aware muscle memory training

## License

MIT
