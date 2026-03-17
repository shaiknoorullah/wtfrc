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

The manifest is stored in the SQLite database (see §4.1 `manifest` table). A single source of truth avoids drift between file-based and DB-based state.

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

Each parser implements the `Parser` interface and outputs a slice of `RawEntry` structs:

```go
// Parser is the interface every config parser must implement.
type Parser interface {
    // Name returns the tool identifier (e.g. "i3", "tmux", "zsh").
    Name() string
    // CanParse returns true if this parser handles the given file path.
    CanParse(path string) bool
    // Parse reads a config file and returns extracted entries.
    Parse(path string) ([]RawEntry, error)
}

type EntryType string

const (
    EntryKeybind  EntryType = "keybind"
    EntryAlias    EntryType = "alias"
    EntryFunction EntryType = "function"
    EntryExport   EntryType = "export"
    EntrySetting  EntryType = "setting"
    EntryService  EntryType = "service"
    EntryHost     EntryType = "host"
)

type RawEntry struct {
    Tool         string    // "i3", "tmux", "zsh", etc.
    Type         EntryType // keybind, alias, function, etc.
    RawBinding   string    // "$mod+Shift+q" or "alias k=kitty"
    RawAction    string    // "kill" or "kitty"
    SourceFile   string    // absolute path
    SourceLine   int       // line number in file
    ContextLines string    // surrounding lines for LLM context
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

-- Full-text search index on intents (external content — requires sync triggers)
CREATE VIRTUAL TABLE intents_fts USING fts5(phrase, content=intents, content_rowid=id);

-- Full-text search on descriptions (external content — requires sync triggers)
CREATE VIRTUAL TABLE entries_fts USING fts5(description, content=entries, content_rowid=id);

-- Triggers to keep FTS5 external-content tables in sync
CREATE TRIGGER intents_ai AFTER INSERT ON intents BEGIN
  INSERT INTO intents_fts(rowid, phrase) VALUES (new.id, new.phrase);
END;
CREATE TRIGGER intents_ad AFTER DELETE ON intents BEGIN
  INSERT INTO intents_fts(intents_fts, rowid, phrase) VALUES('delete', old.id, old.phrase);
END;
CREATE TRIGGER intents_au AFTER UPDATE ON intents BEGIN
  INSERT INTO intents_fts(intents_fts, rowid, phrase) VALUES('delete', old.id, old.phrase);
  INSERT INTO intents_fts(rowid, phrase) VALUES (new.id, new.phrase);
END;

CREATE TRIGGER entries_ai AFTER INSERT ON entries BEGIN
  INSERT INTO entries_fts(rowid, description) VALUES (new.id, new.description);
END;
CREATE TRIGGER entries_ad AFTER DELETE ON entries BEGIN
  INSERT INTO entries_fts(entries_fts, rowid, description) VALUES('delete', old.id, old.description);
END;
CREATE TRIGGER entries_au AFTER UPDATE ON entries BEGIN
  INSERT INTO entries_fts(entries_fts, rowid, description) VALUES('delete', old.id, old.description);
  INSERT INTO entries_fts(rowid, description) VALUES (new.id, new.description);
END;

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

-- Usage events (stub for v0.2 coach mode, extended in v1.0)
CREATE TABLE usage_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  tool TEXT NOT NULL,                    -- "zsh", "i3", "tmux"
  action_type TEXT NOT NULL DEFAULT '',  -- "command", "keybind", "mouse", "menu" (v1.0)
  action TEXT NOT NULL,                  -- what the user actually did
  optimal_action TEXT,                   -- what they should have done (if different)
  entry_id INTEGER REFERENCES entries(id),
  timestamp TEXT NOT NULL,
  was_optimal INTEGER NOT NULL DEFAULT 0, -- 1 = used best available method
  coached INTEGER DEFAULT 0,             -- 1 = coach message was shown
  time_saved_ms INTEGER                  -- estimated time saved/wasted (v1.0)
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
├── wtfrc.db              # Main SQLite database (includes manifest table)
└── archive/
    └── sessions-2026-03.jsonl   # Archived sessions (rotated monthly)
```

---

## 5. Subsystem 3: LLM Abstraction Layer

A provider-agnostic interface for calling LLMs. Supports Ollama (local) and any OpenAI-compatible API (Claude, GPT, Groq, Together, etc.).

### 5.1 Interface

```go
// Provider is the interface for any LLM backend (Ollama, OpenAI-compat, etc.).
type Provider interface {
    // Name returns the provider identifier (e.g. "ollama", "openai-compat").
    Name() string
    // Complete generates a full completion.
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
    // Stream generates a streaming completion. Tokens are sent to the channel;
    // the channel is closed when the response is complete.
    Stream(ctx context.Context, req CompletionRequest) (<-chan string, error)
    // HealthCheck returns nil if the provider is reachable.
    HealthCheck(ctx context.Context) error
}

type Message struct {
    Role    string // "user" or "assistant"
    Content string
}

type ResponseFormat string

const (
    FormatText ResponseFormat = "text"
    FormatJSON ResponseFormat = "json"
)

type CompletionRequest struct {
    System         string         // system prompt (optional, empty = none)
    Messages       []Message
    MaxTokens      int            // 0 = provider default
    Temperature    float64        // 0 = provider default
    ResponseFormat ResponseFormat // "text" or "json"
}

type TokenUsage struct {
    PromptTokens     int
    CompletionTokens int
}

type CompletionResponse struct {
    Content   string
    Model     string
    Usage     TokenUsage
    LatencyMs int64
}
```

### 5.1.1 Structured Output Parsing

When `ResponseFormat` is `FormatJSON`, the LLM may still return malformed JSON. All callers that expect structured output must:

1. Attempt `json.Unmarshal` into the expected struct
2. On failure, retry once with an explicit repair prompt: `"Your previous response was not valid JSON. Return ONLY valid JSON matching this schema: {schema}"`
3. If retry also fails, return an error — never silently degrade to unstructured text

This is implemented as a helper:

```go
// CompleteJSON sends a JSON-mode completion and unmarshals the response.
// Retries once with a repair prompt on malformed JSON.
func CompleteJSON[T any](ctx context.Context, p Provider, req CompletionRequest) (T, error)
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

1. **Answer accuracy:** Did the LLM cite real entries from the Knowledge Base, or hallucinate? (see §8.5 Hallucination Detection)
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

### 8.5 Hallucination Detection

The supervisor detects hallucinations via two mechanisms:

**1. Entry ID verification (deterministic, no LLM):**

Each query logs the `entries_used` IDs. The supervisor verifies:
- Every cited entry ID exists in the `entries` table
- The answer text references keybinds/aliases that match the cited entries' `raw_binding`/`raw_action` values
- If the answer mentions a specific keybind (e.g., `$mod+Shift+e`) but no cited entry contains it → flagged as potential hallucination

```go
// VerifyAnswer checks that all keybinds/aliases mentioned in the answer
// actually exist in the cited KB entries. Returns a list of hallucinated references.
func VerifyAnswer(answer string, citedEntries []KBEntry) []string
```

**2. LLM cross-check (for ambiguous cases):**

For answers where deterministic verification is inconclusive (e.g., the answer paraphrases rather than quoting), the Strong LLM is asked:

```
Given this user question, the LLM's answer, and the KB entries that were provided as context:
- Does the answer contain any keybinds, aliases, or config values NOT present in the KB entries?
- Does the answer contradict any of the KB entries?
Return: {accurate: bool, hallucinated_refs: [...], contradictions: [...]}
```

The deterministic check runs on every query during supervisor review. The LLM cross-check only runs on queries where the deterministic check is inconclusive or where `entries_used` is empty but the answer claims to cite specific config values.

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

All models are Go structs. Pointer fields (`*string`, `*int`) indicate nullable columns in SQLite.

### 10.1 Knowledge Base Entry

```go
type KBEntry struct {
    ID          int64
    Tool        string     // "i3", "tmux", "zsh", etc.
    Type        EntryType  // keybind, alias, function, etc.
    RawBinding  *string    // "$mod+Shift+q" — nil for non-keybind entries
    RawAction   *string    // "kill" — nil when not applicable
    Description string     // LLM-generated human description
    Intents     []string   // populated via JOIN on intents table
    SourceFile  string
    SourceLine  int
    Category    string     // "window_management", "navigation", etc.
    SeeAlso     []string   // related entry hints (JSON array in DB)
    IndexedAt   time.Time
    FileHash    string     // SHA-256 of source file at index time
}
```

### 10.2 Session & Query

```go
type Session struct {
    ID        string     // UUID
    StartedAt time.Time
    EndedAt   *time.Time // nil while session is open
    Queries   []Query    // populated on load, not stored inline
    ModelUsed string     // "ollama:gemma3:4b", "claude-sonnet", etc.
}

type Query struct {
    ID             int64
    SessionID      string
    Question       string
    Answer         string
    EntriesUsed    []int64    // KB entry IDs (JSON array in DB)
    ResponseTimeMs int64
    Timestamp      time.Time
    AccuracyScore  *float64   // 0.0–1.0, set by supervisor
    Issues         []string   // ["hallucinated_keybind", ...], set by supervisor
}
```

### 10.3 Usage Event (v0.2+)

Single model for all usage tracking — covers both Coach (v0.2) and Tutor (v1.0) needs.

```go
type UsageEvent struct {
    ID            int64
    Tool          string     // "zsh", "i3", "tmux", "nvim"
    ActionType    string     // "command", "keybind", "mouse", "menu" (v1.0 field, empty in v0.2)
    Action        string     // what the user actually did
    OptimalAction *string    // what they should have done (nil if action was already optimal)
    EntryID       *int64     // linked KB entry (nil if no match)
    Timestamp     time.Time
    WasOptimal    bool       // did they use the best available method?
    Coached       bool       // true if a coach message was shown for this event
    TimeSavedMs   *int64     // estimated time saved/wasted (v1.0 field, nil in v0.2)
}
```

This replaces the earlier `UsageRecord` TypeScript interface from the v1.0 vision section. The SQL `usage_events` table (§4.1) maps directly to this struct — `ActionType`, `WasOptimal`, and `TimeSavedMs` columns are added with defaults (`""`, `false`, `NULL`) so v0.2 code doesn't need to set them.

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

### 14.1 Language & Stack

**Go** with the Charm ecosystem.

| Component | Library | Purpose |
|-----------|---------|---------|
| **TUI framework** | [Bubble Tea](https://github.com/charmbracelet/bubbletea) | Interactive popup REPL, coach messages, tutor dashboard |
| **Styling** | [Lip Gloss](https://github.com/charmbracelet/lipgloss) | Colors, borders, layout — matches the ricing aesthetic |
| **Components** | [Bubbles](https://github.com/charmbracelet/bubbles) | Text input, viewport (scrolling answers), spinners, tables |
| **Forms** | [Huh](https://github.com/charmbracelet/huh) | Setup wizard, config init prompts |
| **Markdown** | [Glamour](https://github.com/charmbracelet/glamour) | Render formatted answers with syntax highlighting |
| **Logging** | [Log](https://github.com/charmbracelet/log) | Structured, pretty logging |
| **CLI framework** | [Cobra](https://github.com/spf13/cobra) | Subcommands (ask, index, coach, train, supervise, etc.) |
| **Config** | [Viper](https://github.com/spf13/viper) | TOML config parsing |
| **SQLite** | [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) | Knowledge base, sessions, usage tracking (pure Go, no CGo) |
| **HTTP client** | `net/http` (stdlib) | Ollama API, OpenAI-compatible API calls |
| **SSE streaming** | `bufio.Scanner` on response body | Stream LLM responses token-by-token |
| **File watching** | [fsnotify](https://github.com/fsnotify/fsnotify) | Auto re-index on config changes |
| **Release** | [GoReleaser](https://goreleaser.com/) | Cross-compiled binaries, AUR, Homebrew, Nix, Snap |
| **Hero GIF** | [VHS](https://github.com/charmbracelet/vhs) | Record terminal demos for README |

**Why Go over Python/Rust:**
- **Single binary.** No runtime dependencies. `curl | sh` installs one file. No `pip`, no venv, no version conflicts.
- **Startup in milliseconds.** The popup must feel instant — Go cold-starts in ~5ms, Python takes 100-300ms.
- **Charm ecosystem.** Purpose-built for beautiful terminal UIs. The popup will look stunning out of the box.
- **Concurrency for coach mode.** Go's goroutines handle IPC socket subscriptions (i3, hyprland, tmux) natively. No async/await complexity.
- **Cross-compilation.** `GOOS=linux GOARCH=amd64 go build` — one binary per platform. GoReleaser automates this for releases.
- **Community credibility.** The target audience respects Go CLI tools (lazygit, fzf, gum are all Go + Charm).

**SQLite choice:** `modernc.org/sqlite` (pure Go). No CGo means truly zero build dependencies and clean cross-compilation. FTS5 is fully supported.

### 14.2 Project Structure

```
wtfrc/
├── cmd/
│   └── wtfrc/
│       └── main.go              # Entry point
├── internal/
│   ├── cli/                     # Cobra command definitions
│   │   ├── root.go
│   │   ├── ask.go               # wtfrc ask
│   │   ├── index.go             # wtfrc index
│   │   ├── search.go            # wtfrc search
│   │   ├── coach.go             # wtfrc coach (v0.2)
│   │   ├── train.go             # wtfrc train (v1.0)
│   │   ├── supervise.go         # wtfrc supervise
│   │   ├── stats.go             # wtfrc stats
│   │   ├── doctor.go            # wtfrc doctor
│   │   └── config.go            # wtfrc config
│   ├── indexer/
│   │   ├── indexer.go           # Orchestrates parsing + enrichment
│   │   ├── manifest.go          # Change detection (SHA-256)
│   │   ├── redactor.go          # Secret stripping
│   │   └── parsers/
│   │       ├── parser.go        # Parser interface
│   │       ├── i3.go
│   │       ├── hyprland.go
│   │       ├── tmux.go
│   │       ├── kitty.go
│   │       ├── shell.go         # zsh/bash aliases, functions, exports
│   │       ├── git.go
│   │       ├── ssh.go
│   │       ├── nvim.go
│   │       ├── vscode.go
│   │       ├── systemd.go
│   │       └── generic.go       # Fallback line-by-line parser
│   ├── kb/                      # Knowledge Base
│   │   ├── db.go                # SQLite schema, migrations
│   │   ├── search.go            # FTS5 query logic
│   │   └── models.go            # Entry, Intent, Session, Query types
│   ├── llm/                     # LLM abstraction layer
│   │   ├── provider.go          # Provider interface
│   │   ├── ollama.go            # Ollama implementation
│   │   ├── openai_compat.go     # OpenAI-compatible implementation
│   │   └── streaming.go         # SSE streaming helpers
│   ├── tui/                     # Bubble Tea models
│   │   ├── ask.go               # Popup REPL model
│   │   ├── styles.go            # Lip Gloss theme
│   │   └── components/
│   │       ├── input.go         # Query input with history
│   │       ├── answer.go        # Streaming answer viewport
│   │       └── source.go        # Source file reference display
│   ├── session/
│   │   ├── manager.go           # Session lifecycle
│   │   └── archive.go           # JSONL archival
│   ├── supervisor/
│   │   ├── supervisor.go        # Review logic
│   │   └── report.go            # Report generation
│   ├── coach/                   # v0.2
│   │   ├── watcher.go           # Event interception orchestrator
│   │   ├── shell_hook.go        # zsh preexec integration
│   │   ├── i3_ipc.go            # i3/sway IPC subscriber
│   │   ├── hyprland_ipc.go      # Hyprland socket subscriber
│   │   ├── roast.go             # Roast message generation
│   │   └── modes.go             # Chill/moderate/strict logic
│   ├── tutor/                   # v1.0
│   │   ├── tracker.go           # Usage event recording
│   │   ├── analytics.go         # Efficiency scoring
│   │   ├── adoption.go          # Keybind adoption curves
│   │   ├── report.go            # Weekly report generation
│   │   └── goals.go             # Learning goals
│   └── config/
│       └── config.go            # Viper config loading
├── configs/
│   └── default.toml             # Default config template
├── docs/
│   └── design/
│       └── 2026-03-16-wtfrc-architecture.md
├── scripts/
│   ├── install.sh               # curl installer
│   └── zsh-hook.zsh             # Coach mode shell hook (v0.2)
├── go.mod
├── go.sum
├── .goreleaser.yml
├── Makefile
├── LICENSE
└── README.md
```

### 14.3 Package Distribution

| Channel | Command | Priority |
|---------|---------|----------|
| **curl** | `curl -sf https://wtfrc.sh/install \| sh` | Day 1 |
| **go install** | `go install github.com/shaiknoorullah/wtfrc/cmd/wtfrc@latest` | Day 1 |
| **AUR** | `yay -S wtfrc-bin` | Day 1 (critical for target audience) |
| **Homebrew** | `brew install shaiknoorullah/tap/wtfrc` | Day 1 (via GoReleaser) |
| **Nix** | `nix profile install wtfrc` | Week 2 |
| **Scoop** | `scoop install wtfrc` | Week 2 (Windows) |
| **GitHub Releases** | Pre-built binaries for linux/mac/windows amd64/arm64 | Day 1 (via GoReleaser) |

### 14.4 Dependencies

**Runtime:** None. Single static binary.

**Build-time:**
- Go 1.22+
- GoReleaser (for releases)

**External (user must have):**
- Ollama (for local LLM — `wtfrc doctor --fix` can install it)

**Optional:**
- An OpenAI-compatible API key (for strong LLM features)

---

## 15. Version Roadmap

### v0.2 Vision: "Coach" — The Roast Engine

The Coach turns wtfrc from a passive lookup tool into an active training partner that watches how you use your system and calls you out when you're doing it wrong.

**The core loop:**

```
User types command in terminal
        │
        ▼
zsh preexec hook intercepts command
        │
        ▼
Check Knowledge Base: does an alias/keybind/shortcut exist
for what the user just typed the long way?
        │
        ├── No match → do nothing, let it through
        │
        └── Match found → generate roast, display inline
```

**Example interactions by mode:**

**Chill mode** — friendly nudge:
```
$ docker compose logs -f myservice

  💡 hey, you have an alias for that: dclogs myservice
     source: ~/.zshrc:47
```

**Moderate mode** — roast:
```
$ docker compose logs -f myservice

  🤡 you just mass-produced 36 characters when `dclogs` exists.
     your keyboard didn't mass-deserve that.
     source: ~/.zshrc:47
```

**Strict mode** — blocks execution:
```
$ docker compose logs -f myservice

  🚫 absolutely not. use the alias.
     you configured `dclogs` yourself on March 3rd.
     I'm not running this until you do it right.

     type: dclogs myservice
$ _
```

**What it tracks beyond aliases:**

| Scenario | Detection | Coach Response |
|----------|-----------|----------------|
| User types `docker compose logs -f x` | Alias `dclogs` exists | "Use the alias" |
| User clicks to resize i3 window | i3 IPC detects mouse resize | "You have $mod+r for that" |
| User uses arrow keys in tmux copy mode | tmux hook detects key | "You have vim keys enabled. j/k." |
| User opens app via mouse/rofi when keybind exists | i3 IPC detects exec vs keybind | "You have $mod+Return for kitty" |
| User Ctrl+C exits nvim | nvim autocmd | "Use :wq like a civilized person" |

**Interception mechanisms:**

- **Shell (zsh/bash):** `preexec` hook — fires before every command. Checks the raw command against indexed aliases/functions. Zero performance impact for non-matching commands (hash lookup).
- **i3/sway:** Subscribe to IPC socket events. Detect mouse-driven actions (click-to-focus, drag-to-resize) where keybinds exist. Detect application launches via exec vs keybind.
- **Hyprland:** `hyprctl dispatch` socket monitoring. Same approach as i3 IPC.
- **tmux:** tmux `after-*` hooks (after-select-pane, after-copy-mode, etc.). Detect suboptimal navigation patterns.
- **nvim:** Lightweight RPC plugin or autocmd-based telemetry. Detect anti-patterns (arrow keys in normal mode, mouse scrolling when keybinds exist).

**Cooldown and anti-annoyance:**
- Per-action cooldown (default: 60s) — don't roast for the same thing twice in a row
- Daily roast budget (default: 20) — don't overwhelm. After budget is exhausted, silent for the day.
- Snooze command: `wtfrc coach --snooze 1h` — peace and quiet when you need it
- Graduation: if user consistently uses the correct action for 7 days, stop coaching on that action

**The roast personality:**

The coach has a distinct voice — like a brutally honest friend who's also technically right. The roasts are generated by the Fast LLM with a personality prompt:

```
You are the wtfrc coach. Your personality:
- Brutally honest but never mean-spirited
- Self-deprecating (you're a tool built by someone with the same problem)
- References the user's own config against them ("YOU configured this")
- Short. One or two sentences max. Never a lecture.
- Occasionally complimentary when the user does something impressive
- Uses humor: sarcasm, exaggeration, absurdity. Never cruelty.

Generate a coach message for: user typed "{long_command}" when alias "{alias}" exists.
Mode: {chill|moderate|strict}
```

**Prior art and differentiation:**
- `zsh-you-should-use` — only checks zsh aliases. No personality, no roasting, no multi-tool support.
- wtfrc Coach — tracks aliases, keybinds, shell functions, editor shortcuts, WM actions, and tmux commands. Has a personality. Has modes. Has graduation logic. Fundamentally different product.

---

### v1.0 Vision: "Train" — The AI Productivity Tutor

The Tutor is the long-term evolution: an AI that tracks your actual system usage patterns over days/weeks/months and helps you systematically improve your efficiency. This is where wtfrc becomes a category-defining tool.

**The insight:**

People (especially those with ADHD) follow a predictable pattern:
1. **Hyperfocus burst:** Configure an incredibly complex, well-thought-out system
2. **Partial adoption:** Use 20% of what they configured
3. **Habit calcification:** Develop inefficient habits that feel "good enough"
4. **Blind spots:** Never realize there's a better way because they forgot what they configured
5. **Repeat:** Get excited about a new tool/layout, go back to step 1

The Tutor breaks this cycle by providing objective, data-driven feedback.

**What it tracks:**

```
~/.local/share/wtfrc/usage/
├── daily/
│   ├── 2026-03-16.jsonl     # Every intercepted action
│   └── 2026-03-17.jsonl
├── analytics/
│   ├── tool-efficiency.json  # Per-tool efficiency scores
│   ├── keybind-adoption.json # Adoption curves for each keybind
│   └── weekly-reports/
│       └── 2026-W12.md       # Human-readable weekly report
└── goals/
    └── active-goals.json     # User-set learning goals
```

**Usage data schema:** Uses the `UsageEvent` struct defined in §10.3 — the same model serves both Coach (v0.2) and Tutor (v1.0). The v1.0-specific fields (`ActionType`, `TimeSavedMs`) are populated by the Tutor's enhanced tracking; v0.2 leaves them at their zero values.

**Core tutor features:**

**1. Keybind Adoption Tracking**

When a new keybind or alias is added to any config, the Tutor automatically starts tracking whether the user actually adopts it.

```
Week 1: Added `alias dclogs='docker compose logs -f'`
  - Day 1: typed full command 4 times, used alias 0 times
  - Day 2: typed full command 3 times, used alias 1 time
  - Day 3: typed full command 1 time, used alias 3 times
  - Day 7: full command 0 times, alias 5 times ✅ Adopted!
```

Visualized as adoption curves in weekly reports. The Tutor identifies which keybinds "stick" and which are abandoned, and adjusts coaching intensity accordingly.

**2. Efficiency Scoring**

Each tool gets a daily efficiency score:

```
Tool Efficiency Report — 2026-03-16

  zsh:      87% (12/14 commands used optimal form)
  i3:       62% (used mouse for 5 actions with keybinds)
  tmux:     45% (arrow keys in copy mode, mouse scrolling)
  nvim:     91% (mostly using motions, minor mouse usage)

  Overall:  71% → up from 68% last week 📈
```

**3. Weekly Productivity Reports**

Generated by the Strong LLM analyzing the week's usage data:

```markdown
# wtfrc Weekly Report — Week 12, 2026

## Wins 🎉
- Fully adopted `dclogs` alias (0 long-form uses this week)
- i3 workspace switching is 100% keyboard now
- tmux pane navigation improved: 80% hjkl vs 20% mouse (was 50/50)

## Opportunities 🎯
- You run `git status && git diff` 8 times/day. Consider: `alias gsd='git status && git diff'`
- You have 12 unused i3 keybinds. Top 3 you'd probably use:
  1. `$mod+Shift+r` — restart i3 in-place (you've been logging out to reload)
  2. `$mod+z` — zoom focused window (you're dragging borders instead)
  3. `$mod+Shift+space` — toggle floating (you're right-clicking title bars)

## Streaks 🔥
- 14 days without using mouse to close a window
- 7 days of using tmux copy mode correctly
- 3 days of using zsh aliases consistently

## This Week's Challenge
Try using `$mod+Shift+r` instead of logging out to reload i3.
I'll track your adoption and report back next week.
```

**4. QMK/VIAL Keyboard Layer Tracking**

For users with programmable keyboards (Corne, Planck, Lily58, etc.):

- Parse QMK/VIAL keymap files (JSON or C) to understand layer definitions
- Track which layers the user actually activates (via evdev or QMK's console output)
- Identify unused layers and keys
- Generate practice suggestions: "Layer 2 has home row mods. Try holding 'f' for Shift today."

**5. Learning Goals**

Users can set explicit goals:

```
$ wtfrc train --goal "learn tmux copy mode" --deadline 7d

  Goal set: Learn tmux copy mode
  Deadline: 2026-03-23
  I'll track your usage and coach you through it.

  Step 1: Enter copy mode with prefix+[
  Step 2: Navigate with vim keys (hjkl)
  Step 3: Select with v, copy with y

  Let's start. Try it now.
```

The Tutor breaks down complex workflows into progressive steps, tracks completion, and adjusts difficulty based on adoption speed.

**6. ADHD-Aware Design**

The Tutor is specifically designed for users who over-configure and under-utilize:

- **No shame, only data.** Reports are framed as progress, not failures.
- **Small daily wins.** Focus on one improvement at a time, not a list of 20 things to fix.
- **Streak mechanics.** Gamification that rewards consistency without punishing breaks.
- **Automatic priority.** The Tutor identifies which improvements would save the most time and focuses there first.
- **Novelty rotation.** Introduces new challenges weekly to prevent boredom — a new keybind to learn, a new workflow to try.

**Offline capability:**

All analytics run locally. The Strong LLM (for generating reports and identifying patterns) can be:
- A local 7B-30B model via Ollama (good enough for pattern analysis)
- A remote API for higher quality (optional)
- A 2B model for basic metric computation (no natural language reports, just numbers)

The core tracking and scoring works with zero LLM — it's just counting optimal vs suboptimal actions. The LLM only adds the natural language reports and creative suggestions.

---

### v0.1 — "Ask" (launch)
- [ ] Config parser framework + 10 parsers (i3, tmux, kitty, zsh, git, ssh, nvim, vscode, zed, systemd)
- [ ] LLM abstraction layer (Ollama + OpenAI-compat)
- [ ] Indexer with change detection and semantic enrichment
- [ ] Knowledge Base (SQLite + FTS5)
- [ ] Responder (interactive REPL popup)
- [ ] Session manager (in-session context window + archival)
- [ ] Supervisor (scheduled review, accuracy scoring)
- [ ] CLI interface (ask, index, search, list, supervise, stats, doctor)
- [ ] Config format (TOML)
- [ ] Privacy: secret redaction, offline mode
- [ ] README with hero GIF
- [ ] AUR + Homebrew + go install + curl installer
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
