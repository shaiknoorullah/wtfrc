# wtfrc v0.2 "Coach" — Design Specification

> The Roast Engine: an active training partner that watches how you use your system and coaches you when you do things the long way.

**Date:** 2026-03-21
**Status:** Draft
**Spec:** `docs/design/2026-03-16-wtfrc-architecture.md` (sections 10.1, 10.2, 16.1)
**Depends on:** v0.1 "Ask" (shipped on main)

---

## 1. Overview

Coach transforms wtfrc from a passive lookup tool into an active behavioral coach. When the user performs an action suboptimally — typing a full command when an alias exists, clicking to focus a window when a keybind exists, using arrow keys in neovim when motions exist — the Coach detects it and delivers a coaching message.

**Core loop:**
1. User performs an action
2. wtfrc detects the action and its method (keybind, command, mouse click)
3. wtfrc matches against the indexed Knowledge Base for a better alternative
4. If a match passes throttle/graduation gates, a coaching message is generated
5. The message is delivered via the appropriate channel (inline shell, dunst, tmux status, waybar)

**Design principles:**
- Privacy-first: no raw keystrokes leave the monitoring layer
- Layered detection: simple layers first, complex layers opt-in
- ADHD-aware: never interrupt hyperfocus, one improvement at a time
- No shame, only data: frame as progress, not failure

---

## 2. Detection Architecture — Four Layers

Detection is organized into four independent, additive layers. Layers 1-3 ship in v0.2. Layer 4 is opt-in for power users.

| Layer | Mechanism | Coverage | Value |
|-------|-----------|----------|-------|
| 1. Shell Coaching | zsh `preexec` hook | Full command text for alias/function matching | ~80% |
| 2. Editor Coaching | neovim `vim.on_key()` | Full keystroke access for keybind/motion matching | ~15% |
| 3. Binding-Level Interceptor | Generated wrapper configs + tool event streams | Keybind activation detection via correlation | ~4% |
| 4. OS-Level Input Monitor | Passive evdev reader | Raw key combos + mouse clicks | ~1% |

### 2.1. Layer 1 — Shell Coaching

A zsh `preexec` hook intercepts every command before execution. The hook writes the command text to the daemon's FIFO, backgrounded and disowned so the shell prompt returns instantly.

**Matching algorithm — two tiers:**

**Tier 1: Exact match.** A hash map of `alias_expansion -> alias_name` built from the KB at daemon startup. For each command, check exact match and prefix match (command starts with expansion followed by a space). Only suggest if the alias is actually shorter than what was typed.

**Tier 2: Parameterized match.** A radix trie stores alias expansions as keys. For commands like `docker compose logs -f myservice`, the trie finds the longest matching prefix (`docker compose logs -f`) and suggests the alias (`dclogs`) with the remaining arguments appended (`dclogs myservice`).

Shell functions that replace pipelines use the same prefix matching. Multi-command patterns (e.g., `cd foo && npm install`) use exact match after normalization (collapse whitespace, normalize `&&`/`;`).

**Normalization before matching:** strip leading `sudo`/`noglob`/`nocorrect`/`command`, strip trailing pipes (`| less`, `| head`), collapse whitespace.

**Not coached:** commands with no matching alias/function, commands that are already optimal, non-interactive shell sessions.

### 2.2. Layer 2 — Editor Coaching

A neovim Lua plugin using `vim.on_key()` callback intercepts keystrokes and writes classified events to the daemon's FIFO.

The plugin opens the FIFO once (not per-event) with non-blocking writes and lazy reconnection if the daemon restarts. A 200ms debounce prevents flooding on rapid keystrokes. All I/O is wrapped in error handlers so a missing FIFO or dead daemon never freezes the editor. The FIFO fd is closed on `VimLeavePre`.

**Detection scope:**
- Arrow keys in normal mode (suggest hjkl)
- Mouse scrolling (suggest Ctrl+d/u)
- Suboptimal exits like `:q!` or Ctrl+C (suggest `:wq` or `ZZ`)
- Unused custom keybinds (tracked via activation frequency vs. KB entries)

### 2.3. Layer 3 — Binding-Level Interceptor

For each tool that supports command chaining in keybinds, the daemon generates wrapper configs that notify the daemon via FIFO write, then execute the original action. Combined with tool event streams (result events), the daemon correlates to determine input method.

#### 2.3.1. FIFO Design

A named pipe at `$XDG_RUNTIME_DIR/wtfrc/coach.fifo`. Message format: tab-separated `source\taction\tcontext\n`.

Key properties:
- Writes under PIPE_BUF (4096 bytes on Linux) are atomic — no interleaving from concurrent writers. Messages are under 100 bytes.
- The daemon opens the FIFO with O_RDWR to prevent EOF when no writers are connected (standard Unix idiom).
- Writers check FIFO existence before writing and silently no-op if absent. Writers handle EPIPE gracefully.
- On daemon restart, the FIFO is recreated. Stale writer fds fail silently on next write.

#### 2.3.2. Interceptor Config Generation

On `wtfrc coach start`, the daemon reads all indexed keybindings from the KB and generates per-tool interceptor config files. Each wrapped keybind writes a keybind notification to the FIFO (backgrounded), then executes the original action.

**Supported tools and their chaining mechanisms:**

| Tool | Chaining syntax | Interceptor config path |
|------|----------------|------------------------|
| Hyprland | `exec` dispatcher with backgrounded FIFO write before `hyprctl dispatch` | `~/.config/hypr/wtfrc-intercept.conf` |
| tmux | `run-shell` chained with `\;` separator | `~/.config/tmux/wtfrc-intercept.conf` |
| kitty | `combine` action with `launch --type=background` | `~/.config/kitty/wtfrc-intercept.conf` |
| qutebrowser | `spawn -d` chained with `;;` separator | `~/.config/qutebrowser/wtfrc-intercept.py` |
| yazi | Array `run` field with `shell` command prepended | `~/.config/yazi/wtfrc-intercept.toml` |

**Zed:** Limited extension API. Keybind wrapping may not be feasible. Falls back to Layer 4 or statistical tracking. Revisit when Zed's extension API matures.

**i3/sway:** Supported via IPC socket subscription (same pattern as Hyprland but using i3 IPC protocol). Deferred from initial v0.2 implementation; added when user base warrants it.

Each tool config needs a single `source`/`include` line added once during `wtfrc setup`. When coach stops, interceptor files are emptied — the source lines become harmless no-ops.

Interceptor configs are regenerated on `wtfrc coach start`, `wtfrc index` (keybindings may have changed), and `wtfrc coach reload`.

#### 2.3.3. Result Event Streams

The daemon subscribes to tool event streams as a client to observe state changes:

| Tool | Connection method | Events of interest |
|------|------------------|-------------------|
| Hyprland | Socket2 at `$XDG_RUNTIME_DIR/hypr/$SIG/.socket2.sock` | `activewindow`, `openwindow`, `movewindow`, `fullscreen` |
| tmux | Control mode subprocess (`tmux -C`) | `%window-pane-changed`, `%session-window-changed`, format subscriptions |
| neovim | msgpack-RPC via `$NVIM` socket (per-instance) | Autocmd notifications via `rpcnotify()` |
| yazi | `ya sub cd,hover` subprocess | DDS events on stdout |

Tool sources are **individually failable**. If a tool is not running or not installed, its source enters a dormant/retry state. A missing tool never brings down the daemon. Only the FIFO reader is mandatory.

#### 2.3.4. Correlation Logic

The daemon maintains a sliding 150ms correlation window. When a keybind notification arrives, it is paired with any state change event from the same tool within the window. A state change with no preceding keybind notification within the window is classified as mouse/manual — a coaching opportunity.

### 2.4. Layer 4 — OS-Level Input Monitor (Opt-In)

Full input visibility for power users who want strict mode and detection of built-in/default keybinds not in the user's config.

**Activation:** `wtfrc coach --layer4` flag or `[coach] layer4 = true` in config. Displays a consent warning about reading raw keyboard/mouse events from `/dev/input`.

#### 2.4.1. Architecture

The monitor is a **separate binary** (`wtfrc-monitor`) that passively reads from evdev devices. It does NOT grab devices — it reads alongside the normal input path, adding zero latency to user input.

The normal input path (hardware -> keyd -> libinput -> Hyprland -> applications) is completely untouched. The monitor is a passive observer only.

#### 2.4.2. Key Combo Classification

The monitor tracks modifier state (Shift, Ctrl, Super, Alt) as a bitmask. On a non-modifier key-down event with active modifiers, it emits a classified combo (e.g., "$mod+j", "Ctrl+Shift+t") to the FIFO.

Mouse button presses (BTN_LEFT, BTN_RIGHT, BTN_MIDDLE) and scroll wheel events (REL_WHEEL) are classified and emitted separately.

Mouse buttons are `EV_KEY` type events in evdev. The classifier handles them before the keyboard combo logic to prevent double-emission (e.g., Ctrl+click being emitted as both a key combo and a mouse click).

#### 2.4.3. Privacy Boundary

This is the critical design constraint. The monitor process has access to all raw keystrokes but enforces a strict emission policy:

| Input | Emitted? | Rationale |
|-------|:--------:|-----------|
| Modifier+key combos ($mod+j, Ctrl+t) | Yes | Matches keybind patterns, no content exposure |
| Single characters without modifiers | **No** | Could be password/sensitive text |
| Enter without modifiers | **No** | Could be form submission |
| Shift+letter (typing capitals) | **No** (default, configurable) | Could be password character |
| Mouse clicks | Yes | No content exposure |
| Mouse scroll | Yes | No content exposure |

The monitor NEVER writes raw keystrokes to disk, sends them over any network, logs individual characters, or reconstructs typed words.

`[coach.layer4] emit_shift_chars` is configurable (default false) for users who want to detect vim-style Shift+key commands like `G` (go to end of file).

#### 2.4.4. Device Discovery

The monitor enumerates `/dev/input/event*` and selects devices by capability. It prefers keyd's virtual device (post-remap keycodes matching the user's config). If keyd is not running, it finds the primary physical keyboard.

For mouse: devices with relative axis and button capabilities.

Multiple devices are supported (split keyboards, multiple mice) — one reader goroutine per device, fan-in to a single classifier.

Device hotplug is handled via inotify on `/dev/input/`.

#### 2.4.5. Permissions

Requires membership in the `input` group. `wtfrc doctor` checks for this. `wtfrc coach --layer4` refuses to start without the required permission.

#### 2.4.6. Strict Mode

Strict mode is a **behavioral setting independent from Layer 4**. `--strict` controls whether the coach blocks suboptimal shell commands. `--layer4` controls whether the OS-level input monitor runs. They can be combined but are independently toggleable.

**Shell blocking in strict mode:** Uses a dedicated Unix domain socket (`coach-strict.sock`) with synchronous request-response, NOT the FIFO. The preexec hook sends the command to the socket and blocks on the response with a 100ms timeout. If the daemon responds "reject", the command is canceled. If the daemon is unreachable or times out, the command proceeds (fail-open). `socat` is required for strict mode shell hooks.

**Non-shell strict mode:** More aggressive messaging (every occurrence, reduced cooldown) but never blocks mouse or keyboard events at the evdev level — that would make the system unusable.

#### 2.4.7. Performance

evdev reads are ~24 bytes per event. Processing is pure integer comparisons. FIFO writes only on classified combos, not every keystroke. Human input frequency is ~10-100Hz. Expected CPU: <0.1%. Expected memory: ~5MB. Zero latency on the normal input path.

---

## 3. Daemon Architecture

### 3.1. Process Model

The coach daemon is a single long-running Go process managed by a systemd user service. It uses `errgroup` for goroutine lifecycle management with context-based cancellation.

The daemon's modules:

| Module | Responsibility |
|--------|---------------|
| FIFO reader | Reads events from all layers via the named pipe |
| Tool sources | Optional subscribers to Hyprland socket2, tmux control mode, neovim RPC, yazi DDS |
| Matcher | Radix trie + hash map matching against the KB |
| Correlator | Pairs keybind notifications with result events in a 150ms sliding window |
| Throttle | Per-action cooldown, hourly budget, quiet hours, focus mode gates |
| Graduation | SM-2-inspired spaced repetition state machine per action |
| Roaster | Three-tier message generation (templates, cached LLM pool, live LLM) |
| Dispatcher | Routes messages to the correct delivery channel based on event source |
| Interceptor | Generates and removes wrapper configs per tool |

### 3.2. Lifecycle

**Start:** Create FIFO. Generate interceptor configs. Install shell hook and neovim plugin. Reload tool configs. Start daemon: open FIFO (O_RDWR), connect to available tool sockets, optionally start wtfrc-monitor, load matching engine from KB, load throttle/graduation state from SQLite, signal sd_notify READY.

**Stop:** Signal SIGTERM. Daemon flushes pending messages, persists throttle/graduation state, closes connections, exits. Remove interceptor configs (write empty files). Remove shell hook and neovim plugin. Reload tool configs. Remove FIFO.

**Reload (SIGHUP):** Re-read config, rebuild matching engine from KB, update throttle parameters. Config swapped atomically via atomic pointer for lock-free reads on the hot path.

### 3.3. systemd Integration

Type=notify user service with sd_notify for readiness signaling. Watchdog pings every 10s with a health check (FIFO readable, SQLite responsive). Restart on failure with 5s delay.

Uses `coreos/go-systemd/v22/daemon` for sd_notify integration.

### 3.4. Event Processing Pipeline

Events flow through five stages:

1. **Correlate** — pair keybind notifications with result events. If a keybind preceded the state change, the user used the keybind — no coaching needed. If a state change arrived alone, it was mouse/manual.
2. **Match** — look up the action in the radix trie and hash map for a better alternative.
3. **Throttle** — check all five anti-annoyance gates (cooldown, budget, graduation, quiet hours, focus mode). If any gate blocks, discard.
4. **Generate** — produce a coaching message via the three-tier system.
5. **Dispatch** — route to the correct delivery channel.

---

## 4. Matching Engine

### 4.1. Data Structures

**Hash map** keyed by normalized alias expansion for O(1) exact match. **Radix trie** keyed by alias expansion tokens for O(L) longest-prefix match on parameterized commands.

Both built at daemon startup from KB entries where type is "alias", "function", or "keybind". Rebuilt on SIGHUP or after `wtfrc index`.

### 4.2. Match Result

A suggestion contains: a stable action ID (for throttle/graduation tracking), the tool name, what the user did, the optimal alternative, the source file and line where the optimal action is defined, and the number of keystrokes saved.

---

## 5. Throttle and Graduation

### 5.1. Anti-Annoyance Gates

All coaching messages pass through five gates:

| Gate | Description | Default |
|------|-------------|---------|
| Per-action cooldown | Minimum interval between coaching the same action | 120 seconds |
| Hourly budget | Maximum coaching messages per hour across all actions | 5 |
| Graduation check | Skip coaching for graduated actions | 7 consecutive optimal uses |
| Quiet hours | No coaching during configured hours (local time, midnight-crossing supported) | 22:00-08:00 |
| Focus mode | Restrict coaching to one tool category | All categories |

Per-action cooldowns use a token bucket limiter per action ID. The hourly budget resets on the hour, tracked in-memory with SQLite persistence on shutdown.

### 5.2. Spaced Repetition-Inspired Graduation

Each coachable action has a graduation state machine with external SQLite storage.

**States:** NOVICE -> LEARNING -> IMPROVING -> GRADUATED

| From | Trigger | To | Condition |
|------|---------|-----|-----------|
| NOVICE | user uses optimal action | LEARNING | First adoption |
| NOVICE | user ignores coaching | NOVICE | Cooldown increases |
| LEARNING | optimal action used | IMPROVING | 3 consecutive optimal uses |
| LEARNING | suboptimal action | NOVICE | Streak broken |
| IMPROVING | optimal action used | GRADUATED | 7 consecutive uses over 3+ days |
| IMPROVING | suboptimal action | LEARNING | Streak broken, partial retention |
| GRADUATED | suboptimal action | IMPROVING | Relapse |

**Coaching frequency by state:**

| State | Interval |
|-------|----------|
| NOVICE | Every occurrence (within cooldown) |
| LEARNING | Spaced: 1 day, 3 days, 7 days |
| IMPROVING | Weekly at most |
| GRADUATED | Silent unless relapse |

Uses `qmuntal/stateless` library with guard clauses and external state storage.

**Adoption tracking:** When a coaching message is delivered, a "pending adoption" marker is set. On the next event for the same action, if the user used the optimal method within a configured time window, the coaching log entry is retroactively marked as adopted and the graduation counter increments.

---

## 6. Message Generation — Three Tiers

### 6.1. Tier 1: Static Templates

Pre-defined message templates per coaching mode (chill, moderate, strict) and event type. Covers ~80% of coaching scenarios with zero latency. Templates use variable interpolation for the alias name, typed command, source file, line number, and keystroke savings.

### 6.2. Tier 2: Cached LLM Pool

A pool of ~50 message variations per coaching category, pre-generated by the Fast LLM on `wtfrc coach start` or on a weekly schedule. Stored in SQLite. Rotated through randomly to avoid repetition.

The LLM personality prompt enforces: brutally honest but never mean-spirited, self-deprecating, referencing the user's own config against them, max 2 sentences, occasionally complimentary, humorous without cruelty.

**Fallback:** If LLM unavailable, falls back to Tier 1 templates. Coaching always works offline.

### 6.3. Tier 3: Live LLM

Live generation for complex coaching: multi-step workflow suggestions, milestone celebrations, context-aware roasts. Only triggered when hourly budget has capacity, LLM is reachable, and no template or cached message fits.

---

## 7. Delivery Channels

### 7.1. Context-Aware Routing

The delivery channel is selected automatically based on event source:

| Event source | Default channel | Rationale |
|-------------|----------------|-----------|
| Shell (zsh) | Inline shell (precmd hook) | Appears right in the terminal |
| Hyprland | dunst notification | Desktop-level event = desktop notification |
| tmux | tmux status line | Stays in tmux context |
| Neovim | vim.notify() via RPC | Stays in editor context |
| qutebrowser | dunst notification | No in-app messaging API |
| yazi | dunst notification | TUI, inline messaging not practical |
| kitty | dunst notification | No in-app callback |

All channels are configurable per-source in config. Default fallback for unconfigured sources: dunst.

### 7.2. Channel Implementations

**Inline shell:** A `precmd` hook checks for a pending message file written by the daemon. Displays before the next prompt, then removes the file.

**dunst:** D-Bus notification via `godbus/dbus/v5` + `esiqveland/notify`. Uses `x-dunst-stack-tag` hint to replace (not stack) previous coaching notifications.

**tmux status line:** Daemon writes to a status file. tmux reads it via `#(cat ...)` in `status-right`. Updated by `tmux refresh-client -S`.

**waybar:** Signal-based update. Daemon sends `SIGRTMIN+N` to waybar on coaching events. Waybar re-executes the status command only when signaled. Zero cost between updates. Signal number is configurable.

**neovim:** via msgpack-RPC `vim.notify()` to the connected instance.

---

## 8. CLI Interface

| Command | Description |
|---------|-------------|
| `wtfrc coach start` | Start the coach daemon, generate interceptors, install hooks |
| `wtfrc coach stop` | Stop daemon, remove interceptors and hooks |
| `wtfrc coach status` | Show mode, budget remaining, graduated count, active sources |
| `wtfrc coach reload` | Regenerate interceptor configs, reload matching engine |
| `wtfrc coach --snooze <duration>` | Suppress coaching for specified duration |
| `wtfrc coach --mode <mode>` | Switch mode (chill/moderate/strict) |
| `wtfrc coach --focus <category>` | Focus coaching on one category only |
| `wtfrc coach --layer4` | Enable Layer 4 OS-level input monitor |
| `wtfrc coach --strict` | Enable strict mode (shell command blocking) |
| `wtfrc coach graduated` | List all graduated actions |
| `wtfrc coach stats` | Coaching statistics (total coached, adoption rate, streaks) |
| `wtfrc coach log` | Show recent coaching events |

---

## 9. Configuration

New `[coach]` section in config.toml. Existing configs without it use defaults (coach disabled).

| Key | Default | Description |
|-----|---------|-------------|
| `coach.enabled` | `true` | Master toggle |
| `coach.mode` | `"chill"` | Behavioral mode: chill, moderate, strict |
| `coach.budget_per_hour` | `5` | Max coaching messages per hour |
| `coach.cooldown_seconds` | `120` | Min seconds between coaching same action |
| `coach.quiet_hours` | `"22:00-08:00"` | No coaching (local time, HH:MM-HH:MM, midnight-crossing supported) |
| `coach.focus_category` | `""` | Empty = all categories; "shell", "editor", "wm", etc. |
| `coach.graduation_streak` | `7` | Consecutive optimal uses to graduate |
| `coach.layer4` | `false` | Enable OS-level input monitor |
| `coach.layer4.emit_shift_chars` | `false` | Emit Shift+letter combos (for vim G, etc.) |
| `coach.delivery.shell` | `"inline"` | "inline" or "dunst" |
| `coach.delivery.hyprland` | `"dunst"` | Delivery channel for WM events |
| `coach.delivery.tmux` | `"status"` | "status" or "dunst" |
| `coach.delivery.neovim` | `"notify"` | "notify" or "dunst" |
| `coach.delivery.default` | `"dunst"` | Fallback for unconfigured sources |
| `coach.delivery.waybar_signal` | `8` | SIGRTMIN+N signal number for waybar updates |
| `coach.personality.custom_prompt` | `""` | Override default LLM personality prompt |

Note: message tone is derived from `coach.mode`. The custom_prompt only overrides the LLM prompt for Tier 2/3 generation.

---

## 10. Data Model

### 10.1. New Tables

**coaching_state:** Per-action graduation tracking. Fields: action_id (PK), state (novice/learning/improving/graduated), consecutive_optimal count, total_coached count, total_adopted count, timestamps for first/last coached, last adopted, next scheduled coaching, and graduation date. Indexed on state for queries like "list all graduated actions."

**coaching_log:** Record of every coaching message delivered. Fields: auto-increment ID, timestamp, source tool, action_id (FK to coaching_state), what the user did, the optimal alternative, the message shown, mode, delivery channel, and whether the coaching was subsequently adopted. Indexed on timestamp and action_id.

**coaching_messages:** Cached LLM-generated message templates. Fields: auto-increment ID, category (shell_alias, wm_keybind, editor_motion, etc.), mode (chill/moderate/strict), template text with variable placeholders, variable names as JSON array, generation timestamp, and usage count. Indexed on (category, mode).

### 10.2. Relationship to Existing Tables

The `usage_events` table (stubbed in v0.1 schema) is now actively populated. It records EVERY detected action (both optimal and suboptimal, whether coached or not). The `coaching_log` records only events where a coaching message was actually delivered. They serve different query patterns: `usage_events` for aggregate efficiency metrics, `coaching_log` for coaching effectiveness analysis.

New tables use `CREATE TABLE IF NOT EXISTS` in the existing monolithic schema string, consistent with v0.1's migration approach.

---

## 11. Key Library Choices

| Concern | Library | Rationale |
|---------|---------|-----------|
| systemd notify | `coreos/go-systemd/v22/daemon` | Used by containerd, etcd — battle-tested |
| Goroutine lifecycle | `golang.org/x/sync/errgroup` | Context-integrated shutdown |
| D-Bus (dunst only) | `godbus/dbus/v5` + `esiqveland/notify` | Only needed for freedesktop notifications |
| SQLite | `modernc.org/sqlite` (existing) | Continuing v0.1 driver; migration to ncruces deferred |
| Rate limiting | `golang.org/x/time/rate` | Per-key token bucket for cooldowns |
| State machine | `qmuntal/stateless` | External SQLite storage, guard clauses, thread-safe |
| Neovim RPC | `neovim/go-client` | Official, msgpack-RPC |
| evdev (Layer 4) | `kenshaw/evdev` | Pure Go, no CGO |
| Hyprland | Direct socket connection | Trivial protocol; third-party libs have uncertain maintenance |
| Tmux | `tmux -C` subprocess | Control mode is the proper programmatic interface |
| Yazi | `ya sub` subprocess | DDS CLI is the supported external interface |
| Config swap | `sync/atomic.Pointer[Config]` | Lock-free reads on hot path |

---

## 12. ADHD-Aware Design

These are core design constraints, not optional polish.

1. **Never interrupt hyperfocus.** All notifications are passive (inline prompt, status bar, dismissable notification). Never modal. Never blocking (except strict mode, explicitly opt-in).

2. **One improvement at a time.** Each coaching message suggests exactly one action.

3. **Focus mode.** Restricts coaching to one tool category. Prevents overwhelm from being coached on 10 tools simultaneously. Recommended for new users.

4. **Quiet hours.** Default 22:00-08:00. No coaching during rest time.

5. **Progress framing.** Messages emphasize what was learned, not what was missed.

6. **Novelty rotation.** As the user graduates from shell aliases, coaching naturally shifts to WM keybinds — providing novelty without manual configuration.

---

## 13. File Structure

The coach subsystem lives in `internal/coach/` with a `sources/` subdirectory for tool-specific event subscribers. Layer 4's monitor is in `internal/monitor/` and builds as a separate binary (`cmd/wtfrc-monitor/`).

Shell hook and neovim plugin templates live in `scripts/`. The default config (`configs/default.toml`) is updated with the `[coach]` section.

---

## 14. Testing Strategy

**Unit tests:** Each module tested in isolation with mocked dependencies. Table-driven tests for matcher, correlator, throttle, and graduation.

**Integration tests:** Full pipeline with mock event sources through to mock dispatchers. SQLite graduation state persistence and reload. Interceptor config generation verified by parsing back the generated configs per tool format.

**FIFO tests:** Ephemeral FIFOs in temp directories. Atomic writes, concurrent writers, reader EOF behavior.

**Layer 4 tests:** Mock evdev devices via uinput (create virtual keyboard, inject events, verify classification output).

**End-to-end tests:** Manual test plan with real Hyprland, tmux, neovim sessions. Not in CI (requires running tools).
