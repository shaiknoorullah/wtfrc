# wtfrc — Technical Architecture Design

> AI that reads your dotfiles so you don't have to read your dotfiles.

**Author:** Shaik Noorullah Shareef
**Date:** 2026-03-16
**Status:** Draft

---

## Table of Contents

1. [Vision](#1-vision)
2. [System Overview](#2-system-overview)
3. [Subsystem 1: Indexer](#3-subsystem-1-indexer)
4. [Subsystem 2: Knowledge Base](#4-subsystem-2-knowledge-base)
5. [Subsystem 3: LLM Abstraction Layer](#5-subsystem-3-llm-abstraction-layer)
6. [Subsystem 4: Responder (Popup)](#6-subsystem-4-responder-popup)
7. [Subsystem 5: Session Manager](#7-subsystem-5-session-manager)
8. [Subsystem 6: Supervisor](#8-subsystem-6-supervisor)
9. [Subsystem 7: Usage Tracker (stub for v0.2+)](#9-subsystem-7-usage-tracker)
10. [Data Models](#10-data-models)
11. [Config Format](#11-config-format)
12. [Privacy & Security](#12-privacy--security)
13. [CLI Interface](#13-cli-interface)
14. [Build & Distribution](#14-build--distribution)
15. [Version Roadmap](#15-version-roadmap)

---

## 1. Vision

wtfrc is a local-first, privacy-focused AI assistant that indexes your dotfiles and configs, then answers questions about your own system in plain English.

**Three modes (progressive rollout):**

| Mode | Version | What it does |
|------|---------|--------------|
| **Ask** (popup) | v0.1 | You ask, it answers. Instant popup with local LLM. |
| **Coach** (active) | v0.2 | It watches you use the wrong approach and roasts you. |
| **Train** (tutor) | v1.0 | It tracks your productivity and trains you to use your own setup. |

**Core principles:**
- **Local-first:** Configs never leave your machine unless you explicitly configure a remote LLM.
- **LLM-agnostic:** Swap providers via config. Ollama, OpenAI-compatible APIs, or any future provider.
- **Privacy by design:** Secrets are auto-redacted from the index. Usage data is local SQLite.
- **Offline-capable:** The popup works fully offline with a local model. No internet required.
- **Modular:** Each subsystem is a standalone module with a clean interface. Coach and Tutor plug into the same Knowledge Base and LLM layer.

---

## 2. System Overview

```
                        ┌──────────────────────────┐
                        │     Config Sources        │
                        │  ~/.config/ ~/.zshrc etc  │
                        └────────────┬─────────────┘
                                     │
                              ┌──────▼──────┐
                              │   INDEXER    │ Parses configs, detects changes,
                              │             │ calls Strong LLM to generate
                              │             │ semantic descriptions
                              └──────┬──────┘
                                     │
                              ┌──────▼──────┐
                              │ KNOWLEDGE   │ SQLite DB with semantic index
                              │ BASE        │ intent → keybind → source mappings
                              └──┬───┬───┬──┘
                                 │   │   │
                    ┌────────────┘   │   └────────────┐
                    │                │                 │
             ┌──────▼──────┐ ┌──────▼──────┐  ┌──────▼──────┐
             │  RESPONDER  │ │   COACH     │  │   TUTOR     │
             │  (popup)    │ │  (v0.2)     │  │   (v1.0)    │
             └──────┬──────┘ └──────┬──────┘  └──────┬──────┘
                    │               │                 │
             ┌──────▼──────┐ ┌──────▼──────┐  ┌──────▼──────┐
             │ Fast LLM    │ │ Fast LLM    │  │ Fast LLM    │
             │ (Ollama)    │ │ (Ollama)    │  │ (Ollama)    │
             └─────────────┘ └─────────────┘  └─────────────┘
                    │               │                 │
                    └───────┬───────┘                 │
                            │                         │
                     ┌──────▼──────┐          ┌──────▼──────┐
                     │  SESSION    │          │   USAGE     │
                     │  MANAGER   │          │  TRACKER    │
                     └──────┬──────┘          └──────┬──────┘
                            │                         │
                            └──────────┬──────────────┘
                                       │
                                ┌──────▼──────┐
                                │ SUPERVISOR  │ Reviews sessions & usage,
                                │             │ optimizes index & prompts
                                └─────────────┘
```

**Two tiers of LLM:**
- **Strong LLM** (indexing, supervision): Claude API, GPT-4, or a large local model (30B+). Used infrequently — only during index builds and scheduled reviews.
- **Fast LLM** (responding, coaching): Small local model via Ollama (2-4B). Used per-query. Must be fast (<1s response).

---

## 3. Subsystem 1: Indexer

The Indexer reads config files, detects changes since last run, and produces semantic entries for the Knowledge Base.

### 3.1 Config Source Discovery

The indexer scans a configurable list of paths. Default scan targets:

```toml
# ~/.config/wtfrc/config.toml
[indexer]
scan_paths = [
  "~/.config/i3/config",
  "~/.config/sway/config",
  "~/.config/hypr/hyprland.conf",
  "~/.config/kitty/kitty.conf",
  "~/.config/alacritty/alacritty.toml",
  "~/.tmux.conf",
  "~/.config/tmux/tmux.conf",
  "~/.config/nvim/",
  "~/.zshrc",
  "~/.bashrc",
  "~/.gitconfig",
  "~/.ssh/config",
  "~/.config/Code/User/keybindings.json",
  "~/.config/zed/keymap.json",
]

# Auto-discover: scan ~/.config/ for known tool patterns
auto_discover = true
```

**Auto-discovery** scans `~/.config/` for known config patterns (i3/config, sway/config, hypr/*.conf, etc.) and adds them automatically. Users can override or exclude paths.

### 3.2 Change Detection

The indexer maintains a manifest of previously indexed files with their SHA-256 hashes and modification timestamps.

```
~/.local/share/wtfrc/manifest.json
{
  "files": {
    "~/.config/i3/config": {
      "sha256": "abc123...",
      "mtime": "2026-03-15T10:30:00Z",
      "last_indexed": "2026-03-15T10:35:00Z",
      "entry_count": 47
    }
  }
}
```

**On re-index:**
1. Scan all configured paths
2. Compare SHA-256 hash against manifest
3. If unchanged, skip (no LLM call, no cost)
4. If changed, re-index only that file
5. If new file discovered, index it
6. If file deleted, remove its entries from the Knowledge Base

**Triggers for re-indexing:**
- Manual: `wtfrc index` (full) or `wtfrc index --changed` (incremental)
- File watcher: optional inotify/fswatch daemon that triggers incremental re-index on config file changes
- Scheduled: configurable cron interval (default: daily)

### 3.3 Parsing Pipeline

Each config file goes through a two-stage pipeline:

**Stage 1: Structural parsing (no LLM)**

Tool-specific parsers extract raw bindings/aliases/settings. These are deterministic, fast, and free.

Supported parsers:
| Parser | Files | Extracts |
|--------|-------|----------|
| `i3_parser` | i3/config, sway/config | `bindsym/bindcode` lines with modifiers and exec commands |
| `hyprland_parser` | hyprland.conf | `bind =` lines |
| `tmux_parser` | tmux.conf | `bind-key` / `bind` lines |
| `kitty_parser` | kitty.conf | `map` lines |
| `shell_parser` | .zshrc, .bashrc | `alias`, `export`, `function` definitions |
| `git_parser` | .gitconfig | `[alias]` section entries |
| `ssh_parser` | ssh/config | `Host` blocks (redact IdentityFile values) |
| `nvim_parser` | lua keymaps | `vim.keymap.set()` calls |
| `vscode_parser` | keybindings.json | JSON keybinding objects |
| `zed_parser` | keymap.json | JSON keybinding objects |
| `systemd_parser` | ~/.config/systemd/user/*.service | Unit file descriptions and ExecStart |
| `cron_parser` | crontab -l output | Schedule + command |
| `generic_parser` | Any file | Line-by-line, best-effort extraction |

Each parser outputs a list of `RawEntry` objects:

```typescript
interface RawEntry {
  tool: string;           // "i3", "tmux", "zsh", etc.
  type: "keybind" | "alias" | "function" | "export" | "setting" | "service" | "host";
  raw_binding: string;    // "$mod+Shift+q" or "alias k=kitty"
  raw_action: string;     // "kill" or "kitty"
  source_file: string;    // absolute path
  source_line: number;    // line number in file
  context_lines: string;  // surrounding lines for LLM context
}
```

**Stage 2: Semantic enrichment (LLM)**

Raw entries are batched and sent to the Strong LLM with a prompt like:

```
You are indexing a user's system configuration.
For each entry below, generate:
1. A human-readable description of what this does (1 sentence)
2. 3-5 intent phrases a user might say when looking for this
3. The tool category
4. Any related entries (e.g., "see also: tmux copy mode")

Tool: i3
Raw binding: $mod+Shift+q
Raw action: kill
Source: ~/.config/i3/config:42
Context:
  # close focused window
  bindsym $mod+Shift+q kill
```

The LLM returns structured output:

```json
{
  "description": "Close/kill the currently focused window",
  "intents": [
    "close window",
    "kill window",
    "close focused window",
    "how do I close a window",
    "quit current window"
  ],
  "category": "window_management",
  "see_also": ["$mod+q might be related"]
}
```

**Batching strategy:**
- Group entries by file (reduces context-switching for the LLM)
- Send in batches of 20-30 entries per LLM call
- For a typical rice (~200-400 entries across all configs), this is 10-20 LLM calls total
- Cost estimate: ~$0.05-0.15 for Claude Haiku, ~$0.30-1.00 for Sonnet

### 3.4 Secret Redaction

Before any entry reaches the LLM or Knowledge Base, a redaction pass removes:
- SSH private key paths (replace with `[REDACTED_KEY_PATH]`)
- API keys / tokens (regex: common patterns like `sk-`, `xoxb-`, `ghp_`, etc.)
- Passwords in URLs (`postgres://user:PASSWORD@host`)
- `.env` file values (index keys only, not values)

The redaction is applied to `raw_action`, `context_lines`, and any LLM-generated descriptions.

---

## 4. Subsystem 2: Knowledge Base

SQLite database storing the semantic index. Designed for both query-time search (popup) and real-time lookup (coach mode).

### 4.1 Schema

```sql
-- Core entries table
CREATE TABLE entries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  tool TEXT NOT NULL,                    -- "i3", "tmux", "zsh", etc.
  type TEXT NOT NULL,                    -- "keybind", "alias", "function", etc.
  raw_binding TEXT,                      -- "$mod+Shift+q", "alias k=kitty"
  raw_action TEXT,                       -- "kill", "kitty"
  description TEXT NOT NULL,             -- LLM-generated human description
  source_file TEXT NOT NULL,             -- absolute path
  source_line INTEGER,                   -- line number
  category TEXT,                         -- "window_management", "navigation", etc.
  see_also TEXT,                         -- JSON array of related entry hints
  indexed_at TEXT NOT NULL,              -- ISO timestamp
  file_hash TEXT NOT NULL                -- SHA-256 of source file at index time
);

-- Intent phrases for semantic search
CREATE TABLE intents (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  entry_id INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
  phrase TEXT NOT NULL                   -- "close window", "kill focused window", etc.
);

-- Full-text search index on intents
CREATE VIRTUAL TABLE intents_fts USING fts5(phrase, content=intents, content_rowid=id);

-- Full-text search on descriptions
CREATE VIRTUAL TABLE entries_fts USING fts5(description, content=entries, content_rowid=id);

-- File manifest for change tracking
CREATE TABLE manifest (
  file_path TEXT PRIMARY KEY,
  sha256 TEXT NOT NULL,
  mtime TEXT NOT NULL,
  last_indexed TEXT NOT NULL,
  entry_count INTEGER NOT NULL DEFAULT 0
);

-- Session logs (for supervisor review)
CREATE TABLE sessions (
  id TEXT PRIMARY KEY,                   -- UUID
  started_at TEXT NOT NULL,
  ended_at TEXT,
  query_count INTEGER DEFAULT 0,
  model_used TEXT,                       -- "ollama:gemma3:4b", "claude-sonnet", etc.
  archived INTEGER DEFAULT 0            -- 1 = moved to archive
);

-- Individual queries within sessions
CREATE TABLE queries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  question TEXT NOT NULL,                -- user's raw question
  answer TEXT NOT NULL,                  -- LLM's response
  entries_used TEXT,                     -- JSON array of entry IDs used as context
  response_time_ms INTEGER,             -- latency tracking
  timestamp TEXT NOT NULL,
  -- Supervisor-assigned fields (NULL until reviewed)
  accuracy_score REAL,                  -- 0.0-1.0 from supervisor review
  issues TEXT                           -- JSON: ["hallucinated_keybind", "wrong_tool", etc.]
);

-- Usage events (stub for v0.2 coach mode)
CREATE TABLE usage_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  tool TEXT NOT NULL,                    -- "zsh", "i3", "tmux"
  action TEXT NOT NULL,                  -- what the user actually did
  optimal_action TEXT,                   -- what they should have done (if different)
  entry_id INTEGER REFERENCES entries(id),
  timestamp TEXT NOT NULL,
  coached INTEGER DEFAULT 0             -- 1 = coach message was shown
);

-- Supervisor runs
CREATE TABLE supervisor_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  run_at TEXT NOT NULL,
  sessions_reviewed INTEGER,
  issues_found INTEGER,
  optimizations_applied TEXT,            -- JSON describing what was changed
  model_used TEXT
);

-- Indexes for performance
CREATE INDEX idx_entries_tool ON entries(tool);
CREATE INDEX idx_entries_type ON entries(type);
CREATE INDEX idx_intents_entry ON intents(entry_id);
CREATE INDEX idx_queries_session ON queries(session_id);
CREATE INDEX idx_usage_events_tool ON usage_events(tool);
CREATE INDEX idx_usage_events_ts ON usage_events(timestamp);
```

### 4.2 Query Flow

When the user asks a question:

1. **FTS search** on `intents_fts` and `entries_fts` with the user's query
2. **Rank results** by FTS score
3. **Top 5-10 entries** become context for the Fast LLM
4. **Fast LLM** generates a natural language answer using the entries as grounding
5. **Log** the query, answer, entries used, and response time to `queries` table

For coach mode (v0.2), the lookup is reversed:
1. **Intercept** user action (e.g., typed `docker compose logs -f myservice`)
2. **Hash lookup** in entries where `type = 'alias'` and action matches
3. **If match found** and user didn't use the alias → generate roast

### 4.3 Database Location

```
~/.local/share/wtfrc/
├── wtfrc.db              # Main SQLite database
├── manifest.json         # File change tracking (also in DB, JSON for human inspection)
└── archive/
    └── sessions-2026-03.jsonl   # Archived sessions (rotated monthly)
```

---

## 5. Subsystem 3: LLM Abstraction Layer

A provider-agnostic interface for calling LLMs. Supports Ollama (local) and any OpenAI-compatible API (Claude, GPT, Groq, Together, etc.).

### 5.1 Interface

```typescript
interface LLMProvider {
  name: string;

  // Generate a completion
  complete(request: CompletionRequest): Promise<CompletionResponse>;

  // Stream a completion (for popup display)
  stream(request: CompletionRequest): AsyncIterable<string>;

  // Check if provider is available
  healthCheck(): Promise<boolean>;
}

interface CompletionRequest {
  system?: string;
  messages: Array<{ role: "user" | "assistant"; content: string }>;
  max_tokens?: number;
  temperature?: number;
  response_format?: "text" | "json";
}

interface CompletionResponse {
  content: string;
  model: string;
  usage: { prompt_tokens: number; completion_tokens: number };
  latency_ms: number;
}
```

### 5.2 Provider Implementations

**OllamaProvider:**
```
POST http://localhost:11434/api/chat
```
- Supports streaming via chunked response
- Model specified in config (default: `gemma3:4b`)
- Health check: `GET http://localhost:11434/api/tags`

**OpenAICompatProvider:**
```
POST {base_url}/v1/chat/completions
```
- Works with: Claude (via Anthropic's OpenAI-compatible endpoint), OpenAI, Groq, Together, OpenRouter, any provider supporting the OpenAI chat completions format
- API key from config or environment variable
- Health check: list models endpoint

### 5.3 Configuration

```toml
[llm]
# Fast model — used for popup responses and coaching. Must be fast.
[llm.fast]
provider = "ollama"
model = "gemma3:4b"
# provider = "openai-compat"
# base_url = "https://api.groq.com/openai/v1"
# api_key_env = "GROQ_API_KEY"
# model = "llama-3.1-8b-instant"

# Strong model — used for indexing and supervision. Quality > speed.
[llm.strong]
provider = "openai-compat"
base_url = "https://api.anthropic.com/v1"
api_key_env = "ANTHROPIC_API_KEY"
model = "claude-sonnet-4-20250514"
# Fallback to local large model if no API key:
# provider = "ollama"
# model = "qwen3:30b"
```

### 5.4 Fallback Chain

If the configured provider is unavailable:
1. Try primary provider
2. If fails → try fallback provider (if configured)
3. If all fail → return error with clear message ("Ollama not running. Start with: ollama serve")

No silent degradation. The user always knows which model answered.

---

## 6. Subsystem 4: Responder (Popup)

The user-facing popup terminal for asking questions.

### 6.1 Popup Lifecycle

```
User presses hotkey (e.g., $mod+slash)
        │
        ▼
i3/sway/hyprland exec rule launches:
  kitty --class wtfrc-popup --title "wtfrc" wtfrc ask
        │
        ▼
Window manager floats + centers the window
(via window rule: for_window [class="wtfrc-popup"] floating enable, ...)
        │
        ▼
wtfrc ask starts interactive REPL:
  ┌─────────────────────────────────────┐
  │  wtfrc — ask your own damn configs  │
  │─────────────────────────────────────│
  │  > how do I scroll in tmux          │
  │                                     │
  │  prefix + [ enters copy mode.       │
  │  Then use vim keys (j/k) or         │
  │  Page Up/Down to scroll.            │
  │  Press q or Enter to exit.          │
  │                                     │
  │  source: ~/.config/tmux/tmux.conf:89│
  │                                     │
  │  > _                                │
  └─────────────────────────────────────┘
        │
        ▼
User presses Esc or clicks outside → window closes
Session logged to DB
```

### 6.2 The `wtfrc ask` Command

An interactive REPL (not fzf-based — simpler and more flexible).

**Behavior:**
- Prompt: `> ` (simple, clean)
- On Enter: send question to Fast LLM with relevant Knowledge Base entries as context
- Response streams token-by-token (feels responsive even if model is slow)
- After response, show source file:line reference
- Arrow up: recall previous question in this session
- Esc: exit cleanly (close the terminal window)
- Ctrl+C: exit cleanly

**System prompt for the Fast LLM:**

```
You are wtfrc, a local AI assistant that answers questions about the user's
system configuration. You have access to their indexed config entries below.

Rules:
- Answer concisely. One paragraph max unless the question requires steps.
- Always reference the specific keybind/alias/config with its exact syntax.
- Always cite the source file and line number.
- If the answer isn't in the provided entries, say so. Don't hallucinate.
- If multiple entries are relevant, list them all.
- Use the user's actual config values, not generic documentation.

Indexed entries:
{top_N_relevant_entries}
```

### 6.3 Window Manager Integration

**i3/sway config addition:**
```
# wtfrc popup
bindsym $mod+question exec kitty --class wtfrc-popup --title wtfrc -e wtfrc ask
for_window [class="wtfrc-popup"] floating enable, resize set 800 500, move position center
```

**Hyprland config addition:**
```
bind = $mainMod, SLASH, exec, kitty --class wtfrc-popup --title wtfrc -e wtfrc ask
windowrulev2 = float, class:^(wtfrc-popup)$
windowrulev2 = size 800 500, class:^(wtfrc-popup)$
windowrulev2 = center, class:^(wtfrc-popup)$
```

**Alternative frontends (future):**
- `wtfrc ask --ui rofi` — Rofi-based input with preview
- `wtfrc ask --ui tmux` — tmux display-popup
- `wtfrc ask --ui term` — raw terminal (default, for kitty popup)

---

## 7. Subsystem 5: Session Manager

Manages conversation state within and across popup sessions.

### 7.1 Session Lifecycle

```
Popup opens → new session (UUID generated)
  ↓
User asks question #1 → logged to queries table
  ↓
User asks question #2 → same session, has context of Q1
  ↓ (user can ask follow-ups referencing prior answers)
User closes popup → session.ended_at set
  ↓
Session remains in DB for supervisor review
  ↓ (after configurable retention period)
Session archived to JSONL file, rows deleted from DB
```

### 7.2 In-Session Context

Within an open popup session, the last 3-5 Q&A pairs are included in the LLM context. This allows follow-ups like:

```
> how do I scroll in tmux
[answer about copy mode]

> and how do I exit that mode
[answer about pressing q — knows "that mode" = copy mode from prior context]
```

When the popup closes, context is gone. Next popup is a fresh session.

### 7.3 Session Archive

Sessions are archived to JSONL for supervisor review and long-term analytics:

```jsonl
{"session_id":"abc-123","started_at":"2026-03-16T10:00:00Z","ended_at":"2026-03-16T10:02:30Z","queries":[{"question":"how do I scroll in tmux","answer":"prefix + [ enters copy mode...","response_time_ms":450,"accuracy_score":0.95}]}
```

**Archive rotation:** Monthly files in `~/.local/share/wtfrc/archive/`.

**Retention policy:** Configurable. Default: 90 days in DB, archived indefinitely.

---

## 8. Subsystem 6: Supervisor

A scheduled process that reviews session quality and optimizes the system.

### 8.1 What It Reviews

The supervisor reads recent sessions (since last run) and evaluates:

1. **Answer accuracy:** Did the LLM cite real entries from the Knowledge Base, or hallucinate?
2. **Answer completeness:** Did it miss relevant entries that exist in the index?
3. **Response time:** Are any queries abnormally slow? (flag for investigation)
4. **Index gaps:** Are users asking about things not in the index? (suggest new scan paths)
5. **Prompt quality:** Are the system prompts producing good results?

### 8.2 KPIs Tracked

| KPI | How measured | Target |
|-----|-------------|--------|
| **Accuracy** | Supervisor LLM cross-checks answers against KB entries | >0.9 |
| **Hallucination rate** | Answers citing non-existent keybinds/aliases | <5% |
| **Coverage** | % of queries that had relevant KB entries | >80% |
| **Response time (p50)** | Median response latency | <1000ms |
| **Response time (p95)** | 95th percentile latency | <3000ms |
| **Session length** | Avg queries per session | Tracking only |
| **Repeat queries** | Same question asked across sessions | Flag for index improvement |

### 8.3 Optimization Actions

The supervisor can:

1. **Flag low-accuracy answers** → stored in `queries.issues` for human review
2. **Suggest index additions** → "User asked about Makefile targets 3 times. Add ~/project/Makefile to scan paths?"
3. **Tune system prompts** → If hallucination rate is high, tighten the "don't hallucinate" instruction
4. **Identify stale entries** → If a config file was deleted but entries remain, flag for cleanup
5. **Generate a report** → Human-readable summary written to `~/.local/share/wtfrc/reports/`

### 8.4 Scheduling

```toml
[supervisor]
enabled = true
schedule = "daily"        # "daily", "weekly", or cron expression
model_tier = "strong"     # Uses the strong LLM for review
retain_reports = 30       # Keep last 30 reports
```

Runs via systemd timer or cron. The supervisor itself is a CLI command: `wtfrc supervise`.

---

## 9. Subsystem 7: Usage Tracker (stub for v0.2+)

**Not built in v0.1.** The interface and data model are defined now so the Coach and Tutor can plug in later.

### 9.1 Event Sources (v0.2)

| Source | How | What it captures |
|--------|-----|-----------------|
| **Shell** | zsh preexec hook | Every command typed → check if alias/function exists |
| **i3/sway** | IPC socket subscription | Window management actions (move, resize, close) |
| **Hyprland** | hyprctl socket | Same as i3 IPC |
| **tmux** | tmux hooks | Pane/window/session operations |

### 9.2 Coach Mode (v0.2)

When a usage event matches a "suboptimal" pattern:

```
User types: docker compose logs -f myservice
Alias exists: alias dclogs='docker compose logs -f'
Coach says:  "you just mass-produced 36 characters when `dclogs` exists.
              your keyboard didn't mass-deserve that. use the alias."
```

**Delivery channels:**
- Inline shell message (default, via precmd hook)
- Desktop notification (dunst/mako)
- tmux status line message

**Modes:**
```toml
[coach]
enabled = false           # Off by default in v0.2
mode = "moderate"         # "chill", "moderate", "strict"
delivery = "inline"       # "inline", "notification", "tmux"
cooldown_seconds = 60     # Don't roast for the same thing within 60s
```

### 9.3 Tutor Mode (v1.0)

Longer-term analytics:
- **Keybind adoption tracking:** When a new keybind is added, track if/when user starts using it
- **Efficiency scoring:** Ratio of optimal vs suboptimal actions per tool per day
- **Weekly reports:** "You used 12 of your 50 i3 keybinds this week. Here are 3 you might want to try."
- **Learning curves:** Visualize adoption of new keybinds over time
- **QMK/VIAL layer tracking:** Monitor which keyboard layers are used (requires integration)

---

## 10. Data Models

### 10.1 Knowledge Base Entry (full)

```typescript
interface KBEntry {
  id: number;
  tool: string;
  type: "keybind" | "alias" | "function" | "export" | "setting" | "service" | "host" | "script";
  raw_binding: string | null;
  raw_action: string | null;
  description: string;
  intents: string[];
  source_file: string;
  source_line: number;
  category: string;
  see_also: string[] | null;
  indexed_at: string;
  file_hash: string;
}
```

### 10.2 Session (full)

```typescript
interface Session {
  id: string;              // UUID
  started_at: string;
  ended_at: string | null;
  queries: Query[];
  model_used: string;
}

interface Query {
  id: number;
  session_id: string;
  question: string;
  answer: string;
  entries_used: number[];  // KB entry IDs
  response_time_ms: number;
  timestamp: string;
  accuracy_score: number | null;  // Set by supervisor
  issues: string[] | null;        // Set by supervisor
}
```

### 10.3 Usage Event (v0.2+)

```typescript
interface UsageEvent {
  id: number;
  tool: string;
  action: string;
  optimal_action: string | null;
  entry_id: number | null;
  timestamp: string;
  coached: boolean;
}
```

---

## 11. Config Format

Single TOML file at `~/.config/wtfrc/config.toml`.

```toml
# wtfrc configuration

[general]
# Name shown in popup header and coach messages
assistant_name = "wtfrc"

[indexer]
# Paths to scan for config files
scan_paths = [
  "~/.config/i3/config",
  "~/.config/hypr/hyprland.conf",
  "~/.config/kitty/kitty.conf",
  "~/.tmux.conf",
  "~/.zshrc",
  "~/.gitconfig",
  "~/.ssh/config",
]
# Auto-discover configs in ~/.config/
auto_discover = true
# Paths to exclude from auto-discovery
exclude_paths = [
  "~/.config/chromium",
  "~/.config/Code/Cache",
]
# File watcher for automatic re-indexing
watch = false                    # Enable inotify/fswatch watcher
# Re-index schedule (if watch = false)
reindex_schedule = "daily"

[llm.fast]
provider = "ollama"
model = "gemma3:4b"

[llm.strong]
provider = "openai-compat"
base_url = "https://api.anthropic.com/v1"
api_key_env = "ANTHROPIC_API_KEY"
model = "claude-sonnet-4-20250514"

[popup]
# UI frontend for the ask command
frontend = "term"                # "term", "rofi", "tmux" (future)
# Max KB entries to include as LLM context
max_context_entries = 10
# Max conversation history in session
max_history = 5

[session]
# Retention in SQLite before archiving
retain_days = 90
# Archive format
archive_format = "jsonl"

[supervisor]
enabled = true
schedule = "daily"
model_tier = "strong"
retain_reports = 30

[coach]
enabled = false
mode = "moderate"                # "chill", "moderate", "strict"
delivery = "inline"              # "inline", "notification", "tmux"
cooldown_seconds = 60

[privacy]
# Patterns to always redact from index
redact_patterns = [
  "sk-",
  "xoxb-",
  "ghp_",
  "password",
  "secret",
  "token",
]
# Never index these files
never_index = [
  "~/.ssh/id_*",
  "~/.gnupg/",
  "~/.aws/credentials",
  "**/.env",
]
```

---

## 12. Privacy & Security

### 12.1 Threat Model

- **Threat: Config data exfiltrated to cloud** → Mitigated: local-first. Remote LLM calls only contain pre-redacted entry data, never raw config files.
- **Threat: Secrets in index** → Mitigated: redaction pass before indexing. Known patterns auto-stripped.
- **Threat: Usage tracking feels invasive** → Mitigated: off by default. All data local. User controls retention.
- **Threat: Malicious config parser** → Mitigated: parsers only read files, never execute them.

### 12.2 Data Flow

```
Config files → [REDACTION] → Structural parser → [REDACTION] → LLM enrichment → Knowledge Base
                   ▲                                  ▲
                   │                                  │
            Secrets stripped                    Only descriptions
            before parsing                     and intents sent to
                                               LLM, not raw configs
                                               (when using remote LLM)
```

### 12.3 Offline Mode

When no internet is available (or by choice):
- **Fast LLM:** Ollama (always local) — no change
- **Strong LLM:** Falls back to a large local model or skips enrichment
- **Supervisor:** Runs with local model or skips
- **All features work** except remote LLM enrichment

---

## 13. CLI Interface

```
wtfrc                         # Alias for `wtfrc ask`
wtfrc ask                     # Open interactive popup REPL
wtfrc ask "query"             # One-shot: answer and exit
wtfrc index                   # Full re-index all configs
wtfrc index --changed         # Incremental: only changed files
wtfrc index --status          # Show what would be indexed (dry run)
wtfrc search "query"          # Search Knowledge Base without LLM (FTS only)
wtfrc list                    # List all indexed entries
wtfrc list --tool tmux        # List entries for a specific tool
wtfrc supervise               # Run supervisor review now
wtfrc supervise --report      # Show last supervisor report
wtfrc stats                   # Show index stats, session counts, KPIs
wtfrc config                  # Open config file in $EDITOR
wtfrc config --init           # Generate default config
wtfrc doctor                  # Check health: Ollama running, DB exists, etc.
```

---

## 14. Build & Distribution

### 14.1 Language Choice

**Python** (primary consideration):
- Fastest to develop
- Rich ecosystem for CLI (click/typer), SQLite, HTTP clients
- Ollama has a Python SDK
- Easy to package (pip, pipx)
- AUR packaging is trivial for Python tools

**Rust** (future consideration for performance):
- Would matter for coach mode (real-time shell interception)
- Could rewrite hot paths later
- "Written in Rust" is a community credibility signal

**Recommendation for v0.1:** Python. Ship fast. Rewrite in Rust if/when performance matters.

### 14.2 Package Distribution

| Channel | Command | Priority |
|---------|---------|----------|
| **pipx** | `pipx install wtfrc` | Day 1 |
| **pip** | `pip install wtfrc` | Day 1 |
| **AUR** | `yay -S wtfrc` | Day 1 (critical for target audience) |
| **Homebrew** | `brew install wtfrc` | Week 1 |
| **Nix** | `nix profile install wtfrc` | Week 2 |
| **curl** | `curl -sf wtfrc.sh/install \| sh` | Week 1 |

### 14.3 Dependencies

**Required:**
- Python 3.11+
- SQLite 3.35+ (for FTS5 — ships with Python)
- Ollama (for local LLM — can be installed by `wtfrc doctor --fix`)

**Optional:**
- An OpenAI-compatible API key (for strong LLM features)
- inotifywait (for file watcher on Linux)

---

## 15. Version Roadmap

### v0.1 — "Ask" (launch)
- [ ] Config parser framework + 10 parsers (i3, tmux, kitty, zsh, git, ssh, nvim, vscode, zed, systemd)
- [ ] LLM abstraction layer (Ollama + OpenAI-compat)
- [ ] Indexer with change detection and semantic enrichment
- [ ] Knowledge Base (SQLite + FTS5)
- [ ] Responder (interactive REPL popup)
- [ ] Session manager (stateless + stay-open)
- [ ] Supervisor (scheduled review, accuracy scoring)
- [ ] CLI interface (ask, index, search, list, supervise, stats, doctor)
- [ ] Config format (TOML)
- [ ] Privacy: secret redaction, offline mode
- [ ] README with hero GIF
- [ ] AUR + pip + pipx packages
- [ ] `wtfrc setup` one-command installer

### v0.2 — "Coach"
- [ ] Usage tracker: zsh preexec hook
- [ ] Usage tracker: i3/sway/hyprland IPC
- [ ] Coach mode: inline shell roasts
- [ ] Coach mode: desktop notifications
- [ ] Coach mode: strict/moderate/chill modes
- [ ] Supervisor: coach accuracy review
- [ ] Rofi frontend option

### v1.0 — "Train"
- [ ] Tutor: daily/weekly productivity reports
- [ ] Tutor: keybind adoption curves
- [ ] Tutor: efficiency scoring per tool
- [ ] QMK/VIAL layer tracking
- [ ] Waybar/polybar widget
- [ ] Web dashboard (local, optional)
- [ ] Plugin system for custom parsers
- [ ] Team mode (shared configs)
