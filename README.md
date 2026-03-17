# wtfrc

**AI that reads your dotfiles so you don't have to.**

<!-- TODO: Record hero GIF with VHS -->

wtfrc indexes your dotfiles, configs, and keybindings into a local semantic knowledge base, then answers natural-language questions about your setup using a local LLM. No cloud, no telemetry, no data leaves your machine.

## Features

- **Local-first** — your configs stay on your machine. Everything runs locally via Ollama.
- **LLM-agnostic** — works with Ollama, Groq, OpenRouter, or any OpenAI-compatible API.
- **Privacy-first** — API keys, SSH paths, and secrets are automatically redacted before indexing.
- **Offline capable** — fallback chain gracefully handles unreachable providers.
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
# First-run setup (detects configs, pulls Ollama model, runs initial index)
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
wtfrc doctor            Health check (Ollama, DB, config)
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

Config lives at `~/.config/wtfrc/config.toml`. Generate the default with `wtfrc config --init`.

```toml
[llm.fast]
provider = "ollama"
model = "gemma3:4b"

[llm.strong]
provider = "openai-compat"
base_url = "https://api.groq.com/openai/v1"
api_key_env = "GROQ_API_KEY"
model = "llama-3.1-8b-instant"

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
