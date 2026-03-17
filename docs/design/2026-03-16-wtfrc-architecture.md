# wtfrc — Technical Architecture Specification

> AI that reads your dotfiles so you don't have to read your dotfiles.

**Author:** Shaik Noorullah Shareef
**Date:** 2026-03-16
**Status:** Draft
**Intended Status:** Standards Track (Internal)

---

## Status of This Document

This document specifies the architecture and requirements for the wtfrc system, a local-first AI assistant that indexes user configuration files and answers questions about them. It defines subsystem interfaces, data models, storage formats, and behavioral constraints for conforming implementations.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in BCP 14 [RFC 2119] [RFC 8174] when, and only when, they appear in ALL CAPITALS, as shown here.

---

## Table of Contents

1. [Introduction](#1-introduction)
2. [Terminology](#2-terminology)
3. [System Overview](#3-system-overview)
4. [Subsystem 1: Indexer](#4-subsystem-1-indexer)
5. [Subsystem 2: Knowledge Base](#5-subsystem-2-knowledge-base)
6. [Subsystem 3: LLM Abstraction Layer](#6-subsystem-3-llm-abstraction-layer)
7. [Subsystem 4: Responder (Popup)](#7-subsystem-4-responder-popup)
8. [Subsystem 5: Session Manager](#8-subsystem-5-session-manager)
9. [Subsystem 6: Supervisor](#9-subsystem-6-supervisor)
10. [Subsystem 7: Usage Tracker](#10-subsystem-7-usage-tracker)
11. [Data Models](#11-data-models)
12. [Configuration Format](#12-configuration-format)
13. [Privacy and Security Considerations](#13-privacy-and-security-considerations)
14. [CLI Interface](#14-cli-interface)
15. [Build and Distribution](#15-build-and-distribution)
16. [Version Roadmap](#16-version-roadmap)
17. [Conformance](#17-conformance)
18. [References](#18-references)
19. [Appendix A: Knowledge Base SQL Schema](#appendix-a-knowledge-base-sql-schema)
20. [Appendix B: Default Configuration Template](#appendix-b-default-configuration-template)

---

## 1. Introduction

### 1.1. Purpose

wtfrc is a local-first, privacy-focused AI assistant that indexes user dotfiles and configuration files, then answers questions about the user's own system in plain English. This document defines the architecture, subsystem interfaces, data models, and behavioral requirements for conforming implementations.

### 1.2. Operational Modes

wtfrc provides three operational modes, introduced across successive release versions:

- **Ask (v0.1):** The user poses a question and receives an immediate answer via a popup terminal. A local LLM generates responses grounded in the user's indexed configuration entries.

- **Coach (v0.2):** The system actively monitors user behavior across shell commands, window manager actions, and multiplexer operations. When a suboptimal action is detected (i.e., the user performs a task the long way when a configured alias, keybind, or shortcut exists), the system generates a coaching notification. This mode is further described in Section 10.2 and Section 16.1.

- **Train (v1.0):** The system tracks usage patterns over extended periods (days, weeks, months) and generates productivity reports, keybind adoption curves, efficiency scores, and personalized learning goals. This mode is further described in Section 10.3 and Section 16.2.

### 1.3. Core Principles

Conforming implementations MUST adhere to the following principles:

1. **Local-first:** Configuration data MUST NOT leave the user's machine unless the user has explicitly configured a remote LLM provider. When a remote LLM is configured, only pre-redacted entry data SHALL be transmitted; raw configuration file contents MUST NOT be sent.

2. **LLM-agnostic:** The system MUST support swapping LLM providers via configuration. At minimum, Ollama (local) and any OpenAI-compatible API endpoint MUST be supported.

3. **Privacy by design:** Secrets MUST be auto-redacted from the index before any LLM call or persistent storage. All usage data MUST be stored locally in SQLite.

4. **Offline-capable:** The Ask mode (popup) MUST function fully offline when a local LLM model is available. No internet connectivity SHALL be required for core query functionality.

5. **Modular:** Each subsystem MUST be a standalone module with a well-defined interface. The Coach and Tutor modes MUST plug into the same Knowledge Base and LLM layer used by the Ask mode.

---

## 2. Terminology

The following terms are used throughout this document with specific meanings:

- **Config source:** A file or directory on the user's filesystem containing configuration data for a tool (e.g., `~/.config/i3/config`, `~/.zshrc`, `~/.tmux.conf`).

- **Entry:** A discrete unit of configuration knowledge extracted from a config source and stored in the Knowledge Base. An entry represents a single keybind, alias, function, export, setting, service definition, or host block.

- **Entry type:** One of the following classification values: "keybind", "alias", "function", "export", "setting", "service", or "host".

- **Fast LLM:** A small, locally-hosted language model (typically 2-4B parameters) used for per-query response generation and coaching. Latency MUST be under one second for typical queries.

- **Strong LLM:** A large language model (30B+ parameters locally, or a cloud API such as Claude or GPT-4) used infrequently for index-time semantic enrichment and supervisor reviews. Quality is prioritized over speed.

- **Intent phrase:** A natural-language phrase describing what a user might say when searching for a particular entry (e.g., "close window", "kill focused window"). Intent phrases enable semantic full-text search.

- **Knowledge Base (KB):** The SQLite database containing all indexed entries, intent phrases, session logs, and usage events.

- **Manifest:** A record of previously indexed files with their SHA-256 hashes, modification timestamps, and entry counts. Used for change detection.

- **Parser:** A module that reads a specific config file format and extracts raw entries via deterministic structural analysis (no LLM involved).

- **Raw entry:** The output of a parser before semantic enrichment. Contains the tool name, entry type, raw binding text, raw action text, source file path, source line number, and surrounding context lines.

- **Redaction:** The process of removing or replacing secrets (API keys, passwords, private key paths) from entry data before LLM calls or persistent storage.

- **Session:** A bounded conversation between the user and the Responder, beginning when the popup opens and ending when it closes. Each session has a unique identifier (UUID).

- **Supervisor:** A scheduled process that reviews session quality, detects hallucinations, and optimizes index coverage and prompt effectiveness.

---

## 3. System Overview

### 3.1. Architecture Diagram

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

### 3.2. LLM Tiers

The system employs two tiers of LLM, each with distinct usage profiles:

- **Strong LLM (indexing, supervision):** Used during index builds and scheduled supervisor reviews. Acceptable providers include Claude API, GPT-4, or a large local model (30B+ parameters). This tier is invoked infrequently.

- **Fast LLM (responding, coaching):** Used per-query for popup responses and per-event for coaching messages. MUST be served by a small local model via Ollama (2-4B parameters). Response latency MUST be under one second.

---

## 4. Subsystem 1: Indexer

The Indexer reads configuration files, detects changes since the last run, and produces semantic entries for the Knowledge Base.

### 4.1. Config Source Discovery

The Indexer MUST scan a configurable list of filesystem paths. The following default scan targets SHOULD be included in a default configuration:

- `~/.config/i3/config`
- `~/.config/sway/config`
- `~/.config/hypr/hyprland.conf`
- `~/.config/kitty/kitty.conf`
- `~/.config/alacritty/alacritty.toml`
- `~/.tmux.conf`
- `~/.config/tmux/tmux.conf`
- `~/.config/nvim/` (directory, recursive)
- `~/.zshrc`
- `~/.bashrc`
- `~/.gitconfig`
- `~/.ssh/config`
- `~/.config/Code/User/keybindings.json`
- `~/.config/zed/keymap.json`

When the `auto_discover` configuration option is set to true, the Indexer SHOULD additionally scan `~/.config/` for known config patterns (e.g., i3/config, sway/config, hypr/*.conf) and add them automatically. Users MAY override or exclude paths via the `exclude_paths` configuration option.

### 4.2. Change Detection

The Indexer MUST maintain a manifest of previously indexed files. Each manifest record MUST include the file's absolute path, SHA-256 hash, modification timestamp, last-indexed timestamp, and entry count. The manifest MUST be stored in the SQLite Knowledge Base (see Appendix A, `manifest` table). A single source of truth avoids drift between file-based and database-based state.

On re-index, the Indexer MUST perform the following steps in order:

1. Scan all configured paths.
2. Compute the SHA-256 hash of each discovered file and compare it against the manifest.
3. If the hash is unchanged, the file MUST be skipped (no LLM call, no cost incurred).
4. If the hash has changed, the file MUST be re-indexed (only that file).
5. If a new file is discovered (not present in the manifest), it MUST be indexed.
6. If a previously manifested file has been deleted, its entries MUST be removed from the Knowledge Base.

Re-indexing MAY be triggered by any of the following mechanisms:

- **Manual:** The `wtfrc index` command (full re-index) or `wtfrc index --changed` (incremental).
- **File watcher:** An OPTIONAL inotify/fswatch daemon that triggers incremental re-index on config file changes.
- **Scheduled:** A configurable interval (default: daily).

### 4.3. Parsing Pipeline

Each config file MUST pass through a two-stage pipeline.

#### 4.3.1. Stage 1: Structural Parsing (No LLM)

Tool-specific parsers extract raw bindings, aliases, and settings. These parsers are deterministic, fast, and incur no LLM cost. The following parsers MUST be provided in a conforming v0.1 implementation:

| Parser Identifier | Applicable Files | Extracts |
|-------------------|------------------|----------|
| `i3_parser` | i3/config, sway/config | `bindsym`/`bindcode` lines with modifiers and exec commands |
| `hyprland_parser` | hyprland.conf | `bind =` lines |
| `tmux_parser` | tmux.conf | `bind-key` / `bind` lines |
| `kitty_parser` | kitty.conf | `map` lines |
| `shell_parser` | .zshrc, .bashrc | `alias`, `export`, and `function` definitions |
| `git_parser` | .gitconfig | `[alias]` section entries |
| `ssh_parser` | ssh/config | `Host` blocks (IdentityFile values MUST be redacted) |
| `nvim_parser` | Lua keymaps | `vim.keymap.set()` calls |
| `vscode_parser` | keybindings.json | JSON keybinding objects |
| `zed_parser` | keymap.json | JSON keybinding objects |
| `systemd_parser` | ~/.config/systemd/user/*.service | Unit file descriptions and ExecStart |
| `cron_parser` | crontab -l output | Schedule and command |
| `generic_parser` | Any file | Line-by-line best-effort extraction (fallback) |

Each parser MUST implement the Parser interface, which consists of three operations:

- **Name:** Returns the tool identifier as a string (e.g., "i3", "tmux", "zsh").
- **CanParse:** Accepts a file path as a string and returns a boolean indicating whether this parser handles the given file.
- **Parse:** Accepts a file path as a string and returns a list of RawEntry values or an error.

Each RawEntry produced by a parser MUST contain the following fields:

- **Tool** (string): The tool identifier (e.g., "i3", "tmux", "zsh").
- **Type** (EntryType): One of "keybind", "alias", "function", "export", "setting", "service", or "host".
- **RawBinding** (string): The raw binding text (e.g., "$mod+Shift+q" or "alias k=kitty").
- **RawAction** (string): The raw action text (e.g., "kill" or "kitty").
- **SourceFile** (string): The absolute path to the source file.
- **SourceLine** (integer): The line number in the source file.
- **ContextLines** (string): Surrounding lines from the source file, provided for LLM context.

#### 4.3.2. Stage 2: Semantic Enrichment (LLM)

Raw entries MUST be batched and sent to the Strong LLM for semantic enrichment. For each entry, the LLM MUST generate:

1. A human-readable description of what the entry does (one sentence).
2. Three to five intent phrases a user might say when searching for this entry.
3. A tool category string (e.g., "window_management", "navigation").
4. Any related entry hints (e.g., "see also: tmux copy mode").

The prompt sent to the LLM MUST include the tool name, raw binding, raw action, source file with line number, and context lines for each entry in the batch.

The LLM response MUST be structured as JSON containing the following fields: `description` (string), `intents` (array of strings), `category` (string), and `see_also` (array of strings).

**Batching strategy:** Entries SHOULD be grouped by file to reduce context-switching for the LLM. Batches SHOULD contain 20 to 30 entries per LLM call. For a typical configuration set (approximately 200 to 400 entries across all config sources), this results in 10 to 20 LLM calls total.

**Informational note on cost:** Approximate cost at time of writing is $0.05-0.15 for Claude Haiku and $0.30-1.00 for Sonnet for a full index build.

### 4.4. Secret Redaction

Before any entry reaches the LLM or the Knowledge Base, a redaction pass MUST be applied. The redaction pass MUST remove or replace the following:

- SSH private key paths (replaced with `[REDACTED_KEY_PATH]`).
- API keys and tokens matching common patterns (e.g., prefixes `sk-`, `xoxb-`, `ghp_`, and similar).
- Passwords embedded in URLs (e.g., `postgres://user:PASSWORD@host`).
- Values from `.env` files (keys MAY be indexed, but values MUST NOT be).

Redaction MUST be applied to the `RawAction` field, the `ContextLines` field, and any LLM-generated descriptions.

---

## 5. Subsystem 2: Knowledge Base

The Knowledge Base is a SQLite database storing the semantic index. It MUST support both query-time search (popup/Ask mode) and real-time lookup (Coach mode).

### 5.1. Schema

The Knowledge Base schema is defined in Appendix A. Conforming implementations MUST implement the schema as specified. The schema includes the following tables:

- **entries:** Core entries table storing indexed configuration entries with tool, type, raw binding, raw action, description, source file, source line, category, related entry hints, index timestamp, and file hash.
- **intents:** Intent phrases for semantic search, each linked to an entry via foreign key.
- **intents_fts:** FTS5 virtual table on intent phrases (external content, synchronized via triggers).
- **entries_fts:** FTS5 virtual table on entry descriptions (external content, synchronized via triggers).
- **manifest:** File manifest for change tracking.
- **sessions:** Session logs for supervisor review.
- **queries:** Individual queries within sessions, including supervisor-assigned accuracy scores and issues.
- **usage_events:** Usage events for Coach (v0.2) and Tutor (v1.0) modes.
- **supervisor_runs:** Supervisor execution logs.

Implementations MUST create the performance indexes specified in Appendix A.

### 5.2. Query Flow

When the user submits a question, the system MUST execute the following steps in order:

1. Perform FTS5 search on `intents_fts` and `entries_fts` using the user's query text.
2. Rank results by FTS5 score.
3. Select the top 5 to 10 entries as context for the Fast LLM.
4. Send the selected entries and the user's question to the Fast LLM, which generates a natural language answer grounded in the entries.
5. Log the query, answer, entry IDs used, and response time to the `queries` table.

For Coach mode (v0.2), the lookup is reversed:

1. Intercept the user action (e.g., the user typed `docker compose logs -f myservice`).
2. Perform a hash lookup in entries where type is "alias" and the action matches.
3. If a match is found and the user did not use the alias, generate a coaching message.

### 5.3. Database Location

The Knowledge Base and associated files MUST be stored under `~/.local/share/wtfrc/`. The layout MUST be:

```
~/.local/share/wtfrc/
├── wtfrc.db              # Main SQLite database (includes manifest table)
└── archive/
    └── sessions-2026-03.jsonl   # Archived sessions (rotated monthly)
```

---

## 6. Subsystem 3: LLM Abstraction Layer

This subsystem provides a provider-agnostic interface for calling LLMs. It MUST support Ollama (local) and any OpenAI-compatible API (Claude, GPT, Groq, Together, OpenRouter, etc.).

### 6.1. Provider Interface

Every LLM provider MUST implement the Provider interface, which consists of four operations:

- **Name:** Returns the provider identifier as a string (e.g., "ollama", "openai-compat").
- **Complete:** Accepts a context and a CompletionRequest, and returns a CompletionResponse or an error. This operation generates a full (non-streaming) completion.
- **Stream:** Accepts a context and a CompletionRequest, and returns a channel of string tokens or an error. Tokens are sent to the channel as they become available; the channel is closed when the response is complete.
- **HealthCheck:** Accepts a context and returns nil if the provider is reachable, or an error otherwise.

### 6.2. Data Structures

#### 6.2.1. Message

A Message represents a single turn in a conversation. It MUST contain:

- **Role** (string): Either "user" or "assistant".
- **Content** (string): The text content of the message.

#### 6.2.2. CompletionRequest

A CompletionRequest specifies the input to an LLM completion call. It MUST contain:

- **System** (string, OPTIONAL): A system prompt. An empty string indicates no system prompt.
- **Messages** (list of Message): The conversation history.
- **MaxTokens** (integer): The maximum number of tokens to generate. A value of zero indicates the provider default.
- **Temperature** (float): The sampling temperature. A value of zero indicates the provider default.
- **ResponseFormat** (string): Either "text" or "json".

#### 6.2.3. CompletionResponse

A CompletionResponse represents the output of an LLM completion call. It MUST contain:

- **Content** (string): The generated text.
- **Model** (string): The model identifier used for this completion.
- **Usage** (TokenUsage): Token usage statistics.
- **LatencyMs** (integer): The response latency in milliseconds.

#### 6.2.4. TokenUsage

A TokenUsage value reports token consumption. It MUST contain:

- **PromptTokens** (integer): The number of tokens in the prompt.
- **CompletionTokens** (integer): The number of tokens in the completion.

### 6.3. Structured Output Parsing

When the ResponseFormat is "json", the LLM MAY return malformed JSON. All callers that expect structured output MUST:

1. Attempt to unmarshal the response into the expected data structure.
2. On failure, retry exactly once with an explicit repair prompt: "Your previous response was not valid JSON. Return ONLY valid JSON matching this schema: {schema}".
3. If the retry also fails, return an error. Implementations MUST NOT silently degrade to unstructured text.

Implementations SHOULD provide a generic helper function (e.g., `CompleteJSON`) that encapsulates this retry logic and can unmarshal into any target type.

### 6.4. Provider Implementations

#### 6.4.1. OllamaProvider

The OllamaProvider communicates with a local Ollama instance. It MUST send completion requests to `POST http://localhost:11434/api/chat`. It MUST support streaming via chunked response. The model MUST be configurable (default: `gemma3:4b`). The health check MUST use `GET http://localhost:11434/api/tags`.

#### 6.4.2. OpenAICompatProvider

The OpenAICompatProvider communicates with any OpenAI-compatible API endpoint. It MUST send completion requests to `POST {base_url}/v1/chat/completions`. This provider MUST work with Claude (via Anthropic's OpenAI-compatible endpoint), OpenAI, Groq, Together, OpenRouter, and any other provider supporting the OpenAI chat completions format. The API key MUST be read from the environment variable specified in the configuration. The health check MUST use the list models endpoint.

### 6.5. LLM Configuration

LLM providers are configured under the `[llm]` section of the configuration file. Two tiers MUST be configurable independently: `[llm.fast]` for the Fast LLM and `[llm.strong]` for the Strong LLM. See Appendix B for a complete configuration example.

### 6.6. Fallback Chain

If the configured provider is unavailable, the system MUST attempt the following sequence:

1. Try the primary provider.
2. If the primary fails and a fallback provider is configured, try the fallback provider.
3. If all providers fail, return an error with a clear diagnostic message (e.g., "Ollama not running. Start with: ollama serve").

The system MUST NOT silently degrade. The user MUST always be informed which model generated a given response.

---

## 7. Subsystem 4: Responder (Popup)

The Responder is the user-facing popup terminal for asking questions. It is the primary interface for Ask mode (v0.1).

### 7.1. Popup Lifecycle

The popup lifecycle proceeds as follows:

1. The user presses a configured hotkey (e.g., `$mod+slash`).
2. The window manager executes a terminal emulator with the `wtfrc ask` command (e.g., `kitty --class wtfrc-popup --title wtfrc -e wtfrc ask`).
3. The window manager applies floating window rules to center and size the popup window.
4. The `wtfrc ask` command starts an interactive REPL.
5. The user types questions and receives streamed answers.
6. The user presses Escape or closes the window; the session is logged to the database.

### 7.2. The `wtfrc ask` Command

The `wtfrc ask` command MUST provide an interactive REPL (not fzf-based). It MUST behave as follows:

- The prompt MUST be `> ` (simple, clean).
- On Enter, the question MUST be sent to the Fast LLM with relevant Knowledge Base entries as context.
- The response MUST be streamed token-by-token for perceived responsiveness.
- After each response, the source file and line reference MUST be displayed.
- Arrow-up MUST recall the previous question within the current session.
- Escape MUST exit the popup cleanly (close the terminal window).
- Ctrl+C MUST exit the popup cleanly.

The system prompt sent to the Fast LLM MUST instruct the model to:

- Answer concisely (one paragraph maximum unless the question requires steps).
- Always reference the specific keybind, alias, or config value with its exact syntax.
- Always cite the source file and line number.
- If the answer is not found in the provided entries, state so explicitly. The model MUST NOT hallucinate.
- If multiple entries are relevant, list them all.
- Use the user's actual config values, not generic documentation.

The system prompt MUST include the top N relevant entries (where N is controlled by `popup.max_context_entries`, default 10) as grounding context.

### 7.3. Window Manager Integration

Conforming implementations SHOULD provide example window manager configurations. The following are illustrative:

**i3/sway:**
```
bindsym $mod+question exec kitty --class wtfrc-popup --title wtfrc -e wtfrc ask
for_window [class="wtfrc-popup"] floating enable, resize set 800 500, move position center
```

**Hyprland:**
```
bind = $mainMod, SLASH, exec, kitty --class wtfrc-popup --title wtfrc -e wtfrc ask
windowrulev2 = float, class:^(wtfrc-popup)$
windowrulev2 = size 800 500, class:^(wtfrc-popup)$
windowrulev2 = center, class:^(wtfrc-popup)$
```

### 7.4. Alternative Frontends

Future implementations MAY support additional frontends via the `--ui` flag:

- `wtfrc ask --ui rofi` — Rofi-based input with preview.
- `wtfrc ask --ui tmux` — tmux display-popup.
- `wtfrc ask --ui term` — Raw terminal (default, for kitty popup).

---

## 8. Subsystem 5: Session Manager

The Session Manager manages conversation state within and across popup sessions.

### 8.1. Session Lifecycle

A session MUST follow this lifecycle:

1. When the popup opens, a new session is created with a UUID identifier and the current timestamp.
2. Each user question and its answer are logged to the `queries` table, associated with the session.
3. When the popup closes, the session's `ended_at` timestamp is set.
4. The session remains in the database for supervisor review.
5. After a configurable retention period (default: 90 days), the session is archived to a JSONL file and the corresponding rows are deleted from the database.

### 8.2. In-Session Context

Within an open popup session, the last 3 to 5 question-and-answer pairs (controlled by `popup.max_history`, default 5) MUST be included in the LLM context. This enables follow-up questions that reference prior answers.

When the popup closes, all in-session context is discarded. The next popup invocation MUST begin a fresh session with no prior context.

### 8.3. Session Archive

Sessions MUST be archived to JSONL format. Each archived session record MUST include the session ID, start and end timestamps, and an array of query records (each containing the question, answer, response time in milliseconds, and accuracy score if assigned by the supervisor).

Archive files MUST be rotated monthly and stored under `~/.local/share/wtfrc/archive/`.

The retention policy MUST be configurable. The default retention period is 90 days in the active database. Archived sessions MAY be retained indefinitely.

---

## 9. Subsystem 6: Supervisor

The Supervisor is a scheduled process that reviews session quality and optimizes the system. It is invoked via the `wtfrc supervise` CLI command.

### 9.1. Review Scope

The Supervisor MUST read all sessions created since its last run and evaluate the following:

1. **Answer accuracy:** Whether the LLM cited real entries from the Knowledge Base, or fabricated references (see Section 9.5).
2. **Answer completeness:** Whether the LLM missed relevant entries that exist in the index.
3. **Response time:** Whether any queries exhibited abnormally high latency (flagged for investigation).
4. **Index gaps:** Whether users asked about topics not covered by the index (suggesting new scan paths).
5. **Prompt quality:** Whether the system prompts are producing satisfactory results.

### 9.2. Key Performance Indicators

The Supervisor MUST track the following KPIs:

| KPI | Measurement Method | Target |
|-----|-------------------|--------|
| Accuracy | Supervisor LLM cross-checks answers against KB entries | >0.9 |
| Hallucination rate | Answers citing non-existent keybinds or aliases | <5% |
| Coverage | Percentage of queries that had relevant KB entries | >80% |
| Response time (p50) | Median response latency | <1000ms |
| Response time (p95) | 95th percentile response latency | <3000ms |
| Session length | Average queries per session | Tracking only |
| Repeat queries | Same question asked across multiple sessions | Flag for index improvement |

### 9.3. Optimization Actions

The Supervisor MAY take the following optimization actions:

1. **Flag low-accuracy answers:** Store issue details in the `queries.issues` field for human review.
2. **Suggest index additions:** For example, "User asked about Makefile targets 3 times. Add ~/project/Makefile to scan paths?"
3. **Tune system prompts:** If the hallucination rate is high, tighten the anti-hallucination instruction.
4. **Identify stale entries:** If a config file was deleted but entries remain, flag for cleanup.
5. **Generate a report:** Write a human-readable summary to `~/.local/share/wtfrc/reports/`.

### 9.4. Scheduling

The Supervisor MUST be configurable via the `[supervisor]` section of the configuration file. It MUST support scheduling via systemd timer or cron. The `schedule` option MUST accept the values "daily", "weekly", or a cron expression. The Supervisor MUST use the Strong LLM tier for its review process. The number of retained reports MUST be configurable (default: 30).

### 9.5. Hallucination Detection

The Supervisor MUST detect hallucinations via two mechanisms:

#### 9.5.1. Entry ID Verification (Deterministic)

This mechanism requires no LLM and MUST be applied to every query during supervisor review. For each reviewed query, the Supervisor MUST:

1. Verify that every entry ID listed in the `entries_used` field exists in the `entries` table.
2. Verify that the answer text references keybinds or aliases that match the cited entries' `raw_binding` and `raw_action` values.
3. If the answer mentions a specific keybind (e.g., `$mod+Shift+e`) but no cited entry contains it, the reference MUST be flagged as a potential hallucination.

#### 9.5.2. LLM Cross-Check (For Ambiguous Cases)

This mechanism uses the Strong LLM and MUST only be invoked for queries where the deterministic verification is inconclusive (e.g., the answer paraphrases rather than quoting) or where `entries_used` is empty but the answer claims to cite specific config values.

The cross-check prompt MUST ask the LLM to determine whether the answer contains any keybinds, aliases, or config values not present in the KB entries, and whether the answer contradicts any of the KB entries. The response MUST be structured as JSON with the fields: `accurate` (boolean), `hallucinated_refs` (array of strings), and `contradictions` (array of strings).

---

## 10. Subsystem 7: Usage Tracker

The Usage Tracker is not implemented in v0.1. Its interface and data model are defined in this specification so that the Coach (v0.2) and Tutor (v1.0) modes can integrate seamlessly.

### 10.1. Event Sources (v0.2)

The following event sources MUST be supported in a conforming v0.2 implementation:

| Source | Mechanism | Captured Data |
|--------|-----------|---------------|
| Shell | zsh `preexec` hook | Every command typed; checked against existing aliases and functions |
| i3/sway | IPC socket subscription | Window management actions (move, resize, close) |
| Hyprland | hyprctl socket | Window management actions (same scope as i3 IPC) |
| tmux | tmux hooks | Pane, window, and session operations |

### 10.2. Coach Mode (v0.2)

When a usage event matches a suboptimal pattern (the user performed an action the long way when a configured shortcut exists), the Coach MUST generate a coaching message.

The Coach MUST support three operational modes:

- **Chill:** A friendly nudge informing the user that a shortcut exists, with the source reference.
- **Moderate:** A humorous roast calling out the inefficiency, with the source reference.
- **Strict:** A blocking message that refuses to execute the command until the user types the alias. (Strict mode is OPTIONAL for conforming implementations.)

**Delivery channels:** The Coach MUST support at least one of the following delivery mechanisms: inline shell message (default, via `precmd` hook), desktop notification (dunst/mako), or tmux status line message.

**Anti-annoyance safeguards:** The Coach MUST implement the following:

- A per-action cooldown period (default: 60 seconds). The Coach MUST NOT generate a message for the same action within the cooldown window.
- A daily coaching budget (default: 20 messages). After the budget is exhausted, the Coach MUST remain silent for the remainder of the day.
- A snooze command (`wtfrc coach --snooze <duration>`) that temporarily disables coaching.
- Graduation logic: if the user consistently uses the correct action for 7 consecutive days, the Coach SHOULD stop coaching on that action.

**Roast personality:** The Coach's messages are generated by the Fast LLM with a personality prompt. The personality MUST be: brutally honest but never mean-spirited, self-deprecating, referencing the user's own config against them, short (one or two sentences maximum), occasionally complimentary, and humorous without cruelty.

**Detection scope:** The Coach MUST detect suboptimal usage across the following scenarios:

| Scenario | Detection Method | Expected Response |
|----------|-----------------|-------------------|
| User types full command when alias exists | Shell preexec hook | Recommend the alias |
| User resizes window via mouse when keybind exists | i3/sway/Hyprland IPC | Recommend the keybind |
| User uses arrow keys in tmux copy mode when vim keys are enabled | tmux hook | Recommend vim navigation |
| User opens app via mouse/launcher when keybind exists | i3/sway/Hyprland IPC | Recommend the keybind |
| User exits nvim with Ctrl+C | nvim autocmd | Recommend `:wq` |

**Interception mechanisms:**

- **Shell (zsh/bash):** A `preexec` hook fires before every command. It checks the raw command against indexed aliases and functions. Non-matching commands MUST incur zero observable performance impact (hash lookup).
- **i3/sway:** IPC socket subscription detects mouse-driven actions (click-to-focus, drag-to-resize) where keybinds exist, and detects application launches via exec versus keybind.
- **Hyprland:** `hyprctl dispatch` socket monitoring, using the same approach as i3 IPC.
- **tmux:** `after-*` hooks (after-select-pane, after-copy-mode, etc.) detect suboptimal navigation patterns.
- **nvim:** A lightweight RPC plugin or autocmd-based telemetry detects anti-patterns (arrow keys in normal mode, mouse scrolling when keybinds exist).

**Prior art and differentiation:** The `zsh-you-should-use` plugin only checks zsh aliases, has no personality or roasting, and provides no multi-tool support. The wtfrc Coach tracks aliases, keybinds, shell functions, editor shortcuts, window manager actions, and tmux commands; has a configurable personality; supports multiple modes; and implements graduation logic.

### 10.3. Tutor Mode (v1.0)

The Tutor provides long-term analytics. It tracks actual system usage patterns over days, weeks, and months, and helps the user systematically improve their efficiency. This mode is further described in Section 16.2.

The Tutor MUST track the following data under `~/.local/share/wtfrc/usage/`:

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

**Usage data model:** The Tutor uses the UsageEvent data model defined in Section 11.3. The v1.0-specific fields (ActionType, TimeSavedMs) are populated by the Tutor's enhanced tracking; v0.2 leaves them at their zero/null values.

---

## 11. Data Models

This section defines the data structures used throughout the system. All field types and constraints are normative.

### 11.1. Knowledge Base Entry (KBEntry)

A KBEntry represents a single indexed configuration entry. It MUST contain the following fields:

- **ID** (integer, 64-bit): The primary key, auto-generated.
- **Tool** (string, REQUIRED): The tool identifier (e.g., "i3", "tmux", "zsh").
- **Type** (EntryType, REQUIRED): One of "keybind", "alias", "function", "export", "setting", "service", or "host".
- **RawBinding** (string, OPTIONAL, nullable): The raw binding text (e.g., "$mod+Shift+q"). Null for non-keybind entries.
- **RawAction** (string, OPTIONAL, nullable): The raw action text (e.g., "kill"). Null when not applicable.
- **Description** (string, REQUIRED): The LLM-generated human-readable description.
- **Intents** (list of strings): Intent phrases for this entry. Populated via join on the intents table; not stored inline.
- **SourceFile** (string, REQUIRED): The absolute path to the source config file.
- **SourceLine** (integer, REQUIRED): The line number in the source file.
- **Category** (string): The tool category (e.g., "window_management", "navigation").
- **SeeAlso** (list of strings): Related entry hints. Stored as a JSON array in the database.
- **IndexedAt** (timestamp, REQUIRED): The ISO 8601 timestamp when this entry was indexed.
- **FileHash** (string, REQUIRED): The SHA-256 hash of the source file at index time.

### 11.2. Session and Query

A **Session** represents a bounded popup conversation. It MUST contain:

- **ID** (string, REQUIRED): A UUID.
- **StartedAt** (timestamp, REQUIRED): When the popup opened.
- **EndedAt** (timestamp, OPTIONAL, nullable): When the popup closed. Null while the session is open.
- **Queries** (list of Query): The queries in this session. Populated on load; not stored inline in the sessions table.
- **ModelUsed** (string): The model identifier used for this session (e.g., "ollama:gemma3:4b", "claude-sonnet").

A **Query** represents a single user question and its answer within a session. It MUST contain:

- **ID** (integer, 64-bit): The primary key, auto-generated.
- **SessionID** (string, REQUIRED): The UUID of the parent session.
- **Question** (string, REQUIRED): The user's raw question text.
- **Answer** (string, REQUIRED): The LLM's response text.
- **EntriesUsed** (list of integers, OPTIONAL): KB entry IDs used as context. Stored as a JSON array in the database.
- **ResponseTimeMs** (integer): The response latency in milliseconds.
- **Timestamp** (timestamp, REQUIRED): When the query was submitted.
- **AccuracyScore** (float, OPTIONAL, nullable): A value from 0.0 to 1.0, set by the supervisor.
- **Issues** (list of strings, OPTIONAL, nullable): Issue tags such as "hallucinated_keybind" or "wrong_tool", set by the supervisor. Stored as a JSON array in the database.

### 11.3. Usage Event (v0.2+)

A **UsageEvent** is a single model that covers both Coach (v0.2) and Tutor (v1.0) tracking needs. It MUST contain:

- **ID** (integer, 64-bit): The primary key, auto-generated.
- **Tool** (string, REQUIRED): The tool identifier (e.g., "zsh", "i3", "tmux", "nvim").
- **ActionType** (string): One of "command", "keybind", "mouse", or "menu". This is a v1.0 field; v0.2 implementations MUST set it to an empty string.
- **Action** (string, REQUIRED): What the user actually did.
- **OptimalAction** (string, OPTIONAL, nullable): What the user should have done, if different from what they did. Null if the action was already optimal.
- **EntryID** (integer, OPTIONAL, nullable): The linked KB entry ID. Null if no match exists.
- **Timestamp** (timestamp, REQUIRED): When the event occurred.
- **WasOptimal** (boolean, REQUIRED): Whether the user used the best available method. Default: false.
- **Coached** (boolean): Whether a coach message was shown for this event. Default: false.
- **TimeSavedMs** (integer, OPTIONAL, nullable): Estimated time saved or wasted in milliseconds. This is a v1.0 field; v0.2 implementations MUST set it to null.

The SQL `usage_events` table (Appendix A) maps directly to this model. The v1.0-specific columns (`action_type`, `time_saved_ms`) are defined with defaults (empty string and NULL, respectively) so that v0.2 code is not required to set them.

---

## 12. Configuration Format

The system MUST use a single TOML file located at `~/.config/wtfrc/config.toml`. Appendix B provides a complete default configuration template.

The configuration file MUST support the following top-level sections:

- **[general]:** General settings, including the assistant name displayed in popup headers and coach messages.
- **[indexer]:** Indexer settings, including scan paths, auto-discovery toggle, exclude paths, file watcher toggle, and re-index schedule.
- **[llm.fast]:** Fast LLM provider configuration (provider, model, and OPTIONAL base URL and API key environment variable).
- **[llm.strong]:** Strong LLM provider configuration (same fields as Fast LLM).
- **[popup]:** Popup settings, including the UI frontend, maximum context entries, and maximum session history depth.
- **[session]:** Session management settings, including retention period in days and archive format.
- **[supervisor]:** Supervisor settings, including enabled toggle, schedule, model tier, and report retention count.
- **[coach]:** Coach settings, including enabled toggle, mode (chill/moderate/strict), delivery channel, and cooldown in seconds.
- **[privacy]:** Privacy settings, including redaction patterns and never-index paths.

---

## 13. Privacy and Security Considerations

### 13.1. Threat Model

The following threats have been identified and mitigated:

1. **Configuration data exfiltrated to cloud.** Mitigation: local-first architecture. Remote LLM calls MUST contain only pre-redacted entry data. Raw config files MUST NOT be transmitted to any remote endpoint.

2. **Secrets persisted in the index.** Mitigation: a mandatory redaction pass runs before indexing. Known secret patterns (Section 4.4) are automatically stripped. Implementations MUST apply redaction before any data is sent to an LLM or written to the Knowledge Base.

3. **Usage tracking perceived as invasive.** Mitigation: usage tracking is off by default (Coach mode disabled). All data is stored locally. The user controls retention policy and MAY delete usage data at any time.

4. **Malicious config parser.** Mitigation: parsers MUST only read files. Parsers MUST NOT execute any file content, spawn processes based on config values, or perform network operations.

### 13.2. Data Flow

The data flow through the system enforces two redaction boundaries:

```
Config files → [REDACTION] → Structural parser → [REDACTION] → LLM enrichment → Knowledge Base
                   ▲                                  ▲
                   │                                  │
            Secrets stripped                    Only descriptions
            before parsing                     and intents sent to
                                               LLM, not raw configs
                                               (when using remote LLM)
```

Redaction MUST be applied both before structural parsing and before LLM enrichment. When a remote LLM is configured, only descriptions and intent phrases SHALL be transmitted -- never the raw config file content.

### 13.3. Offline Mode

When no internet is available (or by user choice), the system MUST degrade as follows:

- **Fast LLM:** Ollama (always local). No change in behavior.
- **Strong LLM:** Falls back to a large local model via Ollama, or skips enrichment if no suitable local model is available.
- **Supervisor:** Runs with a local model, or skips review if no suitable model is available.

All core Ask-mode features MUST work offline. Only remote LLM enrichment is affected.

---

## 14. CLI Interface

A conforming implementation MUST provide the following CLI commands:

| Command | Description |
|---------|-------------|
| `wtfrc` | Alias for `wtfrc ask`. |
| `wtfrc ask` | Open the interactive popup REPL. |
| `wtfrc ask "query"` | One-shot mode: answer the query and exit. |
| `wtfrc index` | Perform a full re-index of all configured config sources. |
| `wtfrc index --changed` | Incremental re-index: process only changed files. |
| `wtfrc index --status` | Dry run: show what would be indexed without performing indexing. |
| `wtfrc search "query"` | Search the Knowledge Base without LLM (FTS only). |
| `wtfrc list` | List all indexed entries. |
| `wtfrc list --tool <name>` | List entries for a specific tool. |
| `wtfrc supervise` | Run the supervisor review immediately. |
| `wtfrc supervise --report` | Display the last supervisor report. |
| `wtfrc stats` | Display index statistics, session counts, and KPIs. |
| `wtfrc config` | Open the configuration file in `$EDITOR`. |
| `wtfrc config --init` | Generate the default configuration file. |
| `wtfrc doctor` | Perform a health check: verify Ollama is running, the database exists, etc. |

---

## 15. Build and Distribution

### 15.1. Language and Stack

The implementation language is Go, using the Charm ecosystem for terminal UI.

| Component | Library | Purpose |
|-----------|---------|---------|
| TUI framework | Bubble Tea | Interactive popup REPL, coach messages, tutor dashboard |
| Styling | Lip Gloss | Colors, borders, layout |
| Components | Bubbles | Text input, viewport (scrolling answers), spinners, tables |
| Forms | Huh | Setup wizard, config init prompts |
| Markdown rendering | Glamour | Render formatted answers with syntax highlighting |
| Logging | Charm Log | Structured, pretty logging |
| CLI framework | Cobra | Subcommands (ask, index, coach, train, supervise, etc.) |
| Config parsing | Viper | TOML config parsing |
| SQLite | modernc.org/sqlite | Knowledge base, sessions, usage tracking (pure Go, no CGo) |
| HTTP client | net/http (stdlib) | Ollama API, OpenAI-compatible API calls |
| SSE streaming | bufio.Scanner | Stream LLM responses token-by-token |
| File watching | fsnotify | Auto re-index on config changes |
| Release | GoReleaser | Cross-compiled binaries, AUR, Homebrew, Nix, Snap |
| Hero GIF | VHS | Record terminal demos for README |

**Rationale for Go:**

- **Single binary.** No runtime dependencies. Installation requires copying one file. No pip, no venv, no version conflicts.
- **Startup latency.** The popup MUST feel instant. Go cold-starts in approximately 5ms; Python takes 100-300ms.
- **Charm ecosystem.** Purpose-built for terminal UIs. Provides high-quality visual output with minimal effort.
- **Concurrency.** Go's goroutines handle IPC socket subscriptions (i3, hyprland, tmux) natively, without async/await complexity.
- **Cross-compilation.** `GOOS=linux GOARCH=amd64 go build` produces a platform-specific binary. GoReleaser automates this for releases.
- **Community credibility.** The target audience is familiar with Go CLI tools (lazygit, fzf, gum).

**SQLite choice:** `modernc.org/sqlite` (pure Go). No CGo dependency, enabling truly zero build dependencies and clean cross-compilation. FTS5 is fully supported.

### 15.2. Project Structure

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
│   │       ├── zed.go
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

### 15.3. Package Distribution

The following distribution channels MUST be supported at launch (Day 1) or within the timeframes indicated:

| Channel | Command | Priority |
|---------|---------|----------|
| curl | `curl -sf https://wtfrc.sh/install \| sh` | Day 1 |
| go install | `go install github.com/shaiknoorullah/wtfrc/cmd/wtfrc@latest` | Day 1 |
| AUR | `yay -S wtfrc-bin` | Day 1 (critical for target audience) |
| Homebrew | `brew install shaiknoorullah/tap/wtfrc` | Day 1 (via GoReleaser) |
| Nix | `nix profile install wtfrc` | Week 2 |
| Scoop | `scoop install wtfrc` | Week 2 (Windows) |
| GitHub Releases | Pre-built binaries for linux/mac/windows amd64/arm64 | Day 1 (via GoReleaser) |

### 15.4. Dependencies

**Runtime:** None. The output is a single static binary.

**Build-time:** Go 1.22 or later, GoReleaser (for releases).

**External (user-provided):** Ollama (for local LLM). The `wtfrc doctor --fix` command MAY offer to install it.

**Optional:** An OpenAI-compatible API key (for Strong LLM features).

---

## 16. Version Roadmap

### 16.1. v0.2 Vision: Coach -- The Roast Engine

The Coach turns wtfrc from a passive lookup tool into an active training partner that watches how the user employs their system and generates coaching messages when suboptimal patterns are detected.

The core loop is:

1. The user types a command in the terminal.
2. A zsh `preexec` hook intercepts the command.
3. The system checks the Knowledge Base for an alias, keybind, or shortcut that covers the same action.
4. If no match is found, the command proceeds without intervention.
5. If a match is found, the Coach generates a coaching message and displays it inline.

**Example interactions by mode:**

In chill mode (friendly nudge), the user types `docker compose logs -f myservice` and the Coach responds with a hint that the `dclogs myservice` alias exists, citing `~/.zshrc:47`.

In moderate mode (roast), the Coach responds with a humorous message pointing out the user typed 36 characters when a shorter alias exists.

In strict mode (blocking), the Coach refuses to execute the command and requires the user to type the alias form. Strict mode informs the user that they themselves configured the alias and when they did so.

**Informational note on implementation timeline:** The v0.2 roadmap items include usage tracker (zsh preexec hook), usage tracker (i3/sway/hyprland IPC), coach mode (inline shell roasts, desktop notifications, strict/moderate/chill modes), supervisor (coach accuracy review), and a Rofi frontend option.

### 16.2. v1.0 Vision: Train -- The AI Productivity Tutor

The Tutor is the long-term evolution: an AI that tracks actual system usage patterns over days, weeks, and months, and helps the user systematically improve their efficiency. This is where wtfrc becomes a category-defining tool.

**The core insight:**

Users (especially those with ADHD) follow a predictable pattern: (1) hyperfocus burst -- configure an incredibly detailed system; (2) partial adoption -- use 20% of what was configured; (3) habit calcification -- develop inefficient habits that feel "good enough"; (4) blind spots -- never realize better approaches exist because the configuration was forgotten; (5) repeat -- get excited about a new tool, return to step 1.

The Tutor breaks this cycle by providing objective, data-driven feedback.

**Core Tutor features:**

**1. Keybind Adoption Tracking.** When a new keybind or alias is added to any config, the Tutor automatically starts tracking whether the user adopts it. Adoption progress is visualized as curves in weekly reports. The Tutor identifies which keybinds "stick" and which are abandoned, and adjusts coaching intensity accordingly.

**2. Efficiency Scoring.** Each tool receives a daily efficiency score: the ratio of optimal to suboptimal actions. Scores are tracked over time and reported weekly with trend indicators.

**3. Weekly Productivity Reports.** Generated by the Strong LLM analyzing the week's usage data. Reports include wins (adopted aliases, improved keyboard usage), opportunities (suggesting new aliases, identifying unused keybinds), streaks (consecutive days of optimal behavior), and a weekly challenge (one specific improvement to focus on).

**4. QMK/VIAL Keyboard Layer Tracking.** For users with programmable keyboards (Corne, Planck, Lily58, etc.), the system parses QMK/VIAL keymap files (JSON or C) to understand layer definitions, tracks which layers are activated (via evdev or QMK console output), identifies unused layers and keys, and generates practice suggestions.

**5. Learning Goals.** Users MAY set explicit learning goals via `wtfrc train --goal "<description>" --deadline <duration>`. The Tutor breaks down complex workflows into progressive steps, tracks completion, and adjusts difficulty based on adoption speed.

**6. ADHD-Aware Design.** The Tutor is specifically designed for users who over-configure and under-utilize:

- No shame, only data. Reports are framed as progress, not failures.
- Small daily wins. Focus on one improvement at a time.
- Streak mechanics. Gamification that rewards consistency without punishing breaks.
- Automatic priority. The Tutor identifies which improvements would save the most time and focuses there first.
- Novelty rotation. Introduces new challenges weekly to prevent boredom.

**Offline capability:** All analytics run locally. The Strong LLM (for generating reports and identifying patterns) MAY be a local 7B-30B model via Ollama, a remote API for higher quality, or a 2B model for basic metric computation (numbers only, no natural language reports). The core tracking and scoring works with zero LLM -- it is pure counting of optimal versus suboptimal actions. The LLM only adds natural language reports and creative suggestions.

**Informational note on implementation timeline:** The v1.0 roadmap items include the Tutor (daily/weekly productivity reports, keybind adoption curves, efficiency scoring per tool), QMK/VIAL layer tracking, Waybar/polybar widget, web dashboard (local, OPTIONAL), plugin system for custom parsers, and team mode (shared configs).

### 16.3. Release Checklist

#### v0.1 -- "Ask" (Launch)

- Config parser framework plus 10 parsers (i3, tmux, kitty, zsh, git, ssh, nvim, vscode, zed, systemd)
- LLM abstraction layer (Ollama + OpenAI-compat)
- Indexer with change detection and semantic enrichment
- Knowledge Base (SQLite + FTS5)
- Responder (interactive REPL popup)
- Session manager (in-session context window + archival)
- Supervisor (scheduled review, accuracy scoring)
- CLI interface (ask, index, search, list, supervise, stats, doctor)
- Config format (TOML)
- Privacy: secret redaction, offline mode
- README with hero GIF
- AUR + Homebrew + go install + curl installer
- `wtfrc setup` one-command installer

#### v0.2 -- "Coach"

- Usage tracker: zsh preexec hook
- Usage tracker: i3/sway/hyprland IPC
- Coach mode: inline shell roasts
- Coach mode: desktop notifications
- Coach mode: strict/moderate/chill modes
- Supervisor: coach accuracy review
- Rofi frontend option

#### v1.0 -- "Train"

- Tutor: daily/weekly productivity reports
- Tutor: keybind adoption curves
- Tutor: efficiency scoring per tool
- QMK/VIAL layer tracking
- Waybar/polybar widget
- Web dashboard (local, optional)
- Plugin system for custom parsers
- Team mode (shared configs)

---

## 17. Conformance

A conforming v0.1 implementation MUST:

1. Implement all parsers listed in Section 4.3.1.
2. Implement the Parser interface as defined in Section 4.3.1.
3. Implement the two-stage parsing pipeline (structural parsing followed by semantic enrichment) as defined in Section 4.3.
4. Implement secret redaction as defined in Section 4.4.
5. Implement the Knowledge Base schema as defined in Appendix A.
6. Implement the query flow as defined in Section 5.2.
7. Implement the Provider interface as defined in Section 6.1.
8. Implement at least the OllamaProvider and OpenAICompatProvider as defined in Section 6.4.
9. Implement the structured output parsing retry logic as defined in Section 6.3.
10. Implement the Responder with the behaviors defined in Section 7.2.
11. Implement the Session Manager with the lifecycle defined in Section 8.1.
12. Implement the Supervisor with hallucination detection as defined in Section 9.5.
13. Implement all CLI commands listed in Section 14.
14. Implement the configuration format as defined in Section 12.
15. Adhere to all privacy and security requirements defined in Section 13.
16. Store all data locally unless the user explicitly configures a remote LLM provider.

A conforming v0.2 implementation MUST additionally implement all Usage Tracker event sources (Section 10.1) and Coach mode features (Section 10.2).

A conforming v1.0 implementation MUST additionally implement all Tutor mode features (Section 10.3 and Section 16.2).

---

## 18. References

- [RFC 2119] Bradner, S., "Key words for use in RFCs to Indicate Requirement Levels", BCP 14, RFC 2119, March 1997.
- [RFC 8174] Leiba, B., "Ambiguity of Uppercase vs Lowercase in RFC 2119 Key Words", BCP 14, RFC 8174, May 2017.
- [FTS5] SQLite FTS5 Extension, https://www.sqlite.org/fts5.html
- [Ollama API] Ollama REST API, https://github.com/ollama/ollama/blob/main/docs/api.md
- [OpenAI Chat API] OpenAI Chat Completions API, https://platform.openai.com/docs/api-reference/chat

---

## Appendix A: Knowledge Base SQL Schema

The following SQL schema defines the Knowledge Base storage format. Conforming implementations MUST implement this schema.

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

-- Full-text search index on intents (external content -- requires sync triggers)
CREATE VIRTUAL TABLE intents_fts USING fts5(phrase, content=intents, content_rowid=id);

-- Full-text search on descriptions (external content -- requires sync triggers)
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

---

## Appendix B: Default Configuration Template

The following TOML configuration is the default template generated by `wtfrc config --init`. All values shown are the defaults unless otherwise noted.

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
