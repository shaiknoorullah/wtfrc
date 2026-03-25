# wtfrc v0.2 "Coach" Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an active behavioral coaching daemon that monitors how a user interacts with their Linux desktop tools and coaches them when they do things suboptimally — typing full commands when aliases exist, clicking when keybinds exist, using arrow keys when motions exist.

**Architecture:** A long-running Go daemon managed by systemd, receiving events via a named pipe (FIFO) from shell hooks, editor plugins, and generated keybind interceptors, plus direct socket subscriptions to Hyprland, tmux, neovim, and yazi. Events flow through a matching engine (radix trie + hash map), correlation layer, throttle/graduation gates, message generator, and context-aware dispatcher. Four detection layers: shell preexec (L1), neovim plugin (L2), binding-level interceptors (L3), and opt-in evdev monitor (L4).

**Tech Stack:** Go 1.25, Cobra, Viper, modernc.org/sqlite, errgroup, godbus/dbus, esiqveland/notify, qmuntal/stateless, golang.org/x/time/rate, coreos/go-systemd, neovim/go-client, kenshaw/evdev

**Spec:** `docs/superpowers/specs/2026-03-21-wtfrc-v02-coach-design.md`

---

## Build Order

Tasks build bottom-up. Each task produces testable, committable code. Later tasks depend on earlier ones.

```
Task 0:  Dependency setup (go get new libraries)
Task 1:  Config + Schema + Event types + DB queries (foundation)
Task 2:  FIFO reader (event input)
Task 3:  Matching engine (radix trie + hash map)
Task 4:  Throttle + graduation (anti-annoyance gates)
Task 5:  Message generation (templates + cached LLM + live LLM)
Task 6:  Delivery channels (dunst, inline shell, tmux, waybar, neovim)
Task 7:  Correlator (keybind ↔ result event pairing)
Task 8:  Daemon lifecycle (errgroup, sd_notify, signal handling)
Task 9:  Layer 1 — Shell coaching (preexec/precmd hooks)
Task 10: Layer 2 — Editor coaching (neovim Lua plugin)
Task 11: Layer 3 — Interceptor config generation
Task 12: Layer 3 — Tool event sources (Hyprland, tmux, neovim, yazi)
Task 13: CLI commands (coach start/stop/status/reload/stats/log/graduated)
Task 14: Layer 4 — OS-level input monitor (separate binary)
Task 15: Integration testing + systemd service file
```

---

## File Structure

```
internal/
    config/
        config.go                    # MODIFY: add CoachConfig struct + defaults
    kb/
        db.go                        # MODIFY: add coaching_state, coaching_log, coaching_messages tables
    coach/
        event.go                     # CREATE: Event type, Source enum, tab-delimited serialization
        event_test.go
        fifo.go                      # CREATE: FIFO reader (O_RDWR, line scanner, event parsing)
        fifo_test.go
        matcher.go                   # CREATE: radix trie + hash map, normalization, Suggestion type
        matcher_test.go
        correlator.go                # CREATE: 150ms sliding window, keybind↔result pairing
        correlator_test.go
        throttle.go                  # CREATE: per-action cooldown, hourly budget, quiet hours, focus mode
        throttle_test.go
        graduation.go                # CREATE: stateless FSM (NOVICE→LEARNING→IMPROVING→GRADUATED)
        graduation_test.go
        roaster.go                   # CREATE: 3-tier message generation (template, cached LLM, live)
        roaster_test.go
        dispatcher.go                # CREATE: context-aware delivery routing + channel implementations
        dispatcher_test.go
        interceptor.go               # CREATE: per-tool wrapper config generation (hyprland, tmux, kitty, qb, yazi)
        interceptor_test.go
        daemon.go                    # CREATE: main event loop, errgroup, sd_notify, SIGHUP reload
        daemon_test.go
        sources/
            hyprland.go              # CREATE: socket2 subscriber, event parsing
            hyprland_test.go
            tmux.go                  # CREATE: tmux -C control mode subprocess
            tmux_test.go
            neovim.go                # CREATE: msgpack-RPC manager, multi-instance discovery
            neovim_test.go
            yazi.go                  # CREATE: ya sub subprocess, DDS event parsing
            yazi_test.go
    monitor/
        monitor.go                   # CREATE: Layer 4 main loop, device→classifier→FIFO
        monitor_test.go
        device.go                    # CREATE: evdev device discovery, capability check, hotplug
        device_test.go
        classifier.go                # CREATE: modifier bitmask, key combo formatting, privacy filter
        classifier_test.go
    cli/
        coach.go                     # CREATE: coach command with start/stop/status/reload/stats/log/graduated subcommands
        coach_test.go                # CREATE: tests for stats/log/graduated queries, flag parsing
cmd/
    wtfrc-monitor/
        main.go                      # CREATE: Layer 4 separate binary entry point
scripts/
    wtfrc-coach.zsh                  # CREATE: preexec + precmd hook template
    wtfrc-coach.lua                  # CREATE: neovim vim.on_key() plugin template
configs/
    default.toml                     # MODIFY: add [coach] section
    wtfrc-coach.service              # CREATE: systemd user service unit file
```

---

## Task 0: Dependency Setup

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

Add all new external dependencies required by v0.2:

Steps:
- [ ] `go get github.com/qmuntal/stateless` (state machine for graduation)
- [ ] `go get golang.org/x/time` (rate limiting for throttle)
- [ ] `go get golang.org/x/sync` (errgroup for daemon lifecycle)
- [ ] `go get github.com/godbus/dbus/v5` (D-Bus for dunst notifications)
- [ ] `go get github.com/esiqveland/notify` (freedesktop notification wrapper)
- [ ] `go get github.com/coreos/go-systemd/v22` (sd_notify for systemd integration)
- [ ] `go get github.com/neovim/go-client` (neovim msgpack-RPC)
- [ ] `go get github.com/kenshaw/evdev` (evdev for Layer 4 monitor)
- [ ] Run `go mod tidy` to clean up
- [ ] Commit: `chore: add v0.2 coach dependencies`

---

## Task 1: Config + Schema + Event Types

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/kb/db.go`
- Create: `internal/coach/event.go`
- Test: `internal/coach/event_test.go`

This task establishes the foundation: config structs for the `[coach]` section, three new database tables (coaching_state, coaching_log, coaching_messages), and the core Event type with Source enum.

**Config struct additions:**
- `CoachConfig` with fields matching spec section 9: Enabled, Mode, BudgetPerHour, CooldownSeconds, QuietHours, FocusCategory, GraduationStreak, Layer4, Layer4EmitShiftChars
- `CoachDeliveryConfig` for per-source delivery channel routing
- `CoachPersonalityConfig` for custom LLM prompt override
- Add `Coach CoachConfig` field to root `Config` struct
- Set defaults in `setDefaults()` matching spec section 9 defaults

**Schema additions** (append to existing `schema` const in `db.go`):
- `coaching_state` table: action_id PK, state, consecutive_optimal, total_coached, total_adopted, timestamps (first/last coached, last adopted, next_coach_after, graduated_at). Index on state.
- `coaching_log` table: auto-increment ID, timestamp, source, action_id FK, user_action, optimal_action, message, mode, delivery, was_adopted. Indexes on timestamp and action_id.
- `coaching_messages` table: auto-increment ID, category, mode, template, variables (JSON), generated_at, used_count. Index on (category, mode).

**Config struct nesting:** `CoachConfig` contains `Layer4 CoachLayer4Config` and `Delivery CoachDeliveryConfig` and `Personality CoachPersonalityConfig` as nested structs (matching how `LLMConfig` nests `LLMProviderConfig` in the existing codebase). Viper+mapstructure requires nested structs for dotted TOML paths like `coach.layer4.emit_shift_chars`.

**Event type:**
- `Source` string enum: "shell", "nvim", "hyprland", "tmux", "kitty", "qutebrowser", "yazi", "evdev"
- `Event` struct: Source, Action, Context, Timestamp, IsKeybind bool
- `ParseEvent(line string) (Event, error)` — parse tab-delimited FIFO line. IsKeybind is determined by a `kb:` prefix in the action field (e.g., `kb:hyprland:movefocus l` → IsKeybind=true, Action="movefocus l"). Events without the prefix are result events.
- `Event.String() string` — serialize back to tab-delimited format

**DB query methods (added to `kb/db.go`):**
- `GetEntriesByTypes(types []string) ([]KBEntry, error)` — fetch all entries matching given types (alias, function, keybind). Used by matcher (Task 3) to build the matching engine at startup.
- `GetEntriesByToolAndType(tool string, entryType string) ([]KBEntry, error)` — fetch entries for a specific tool and type. Used by interceptor generator (Task 11) to get keybinds per tool.

Steps:
- [ ] Write failing tests for Event parsing (tab-delimited string to Event struct, round-trip serialization, malformed input handling, `kb:` prefix detection for IsKeybind — table-driven)
- [ ] Run tests to verify they fail
- [ ] Implement Event type, Source constants, ParseEvent, String methods
- [ ] Run tests to verify they pass
- [ ] Write failing tests for CoachConfig defaults (load empty config, verify coach defaults match spec, verify nested Layer4/Delivery/Personality structs)
- [ ] Run tests to verify they fail
- [ ] Add CoachConfig struct with nested sub-structs and mapstructure tags, add defaults to setDefaults()
- [ ] Run tests to verify they pass
- [ ] Add three new tables to schema const in db.go
- [ ] Write a test that opens a temp DB and verifies the new tables exist with correct columns
- [ ] Run tests to verify they pass
- [ ] Write failing tests for GetEntriesByTypes and GetEntriesByToolAndType (insert test entries, query, verify results)
- [ ] Implement the two new DB query methods
- [ ] Run tests to verify they pass
- [ ] Commit: `feat(coach): add config, schema, event types, and DB queries`

---

## Task 2: FIFO Reader

**Files:**
- Create: `internal/coach/fifo.go`
- Test: `internal/coach/fifo_test.go`

The FIFO reader is the central event input. It opens the named pipe with O_RDWR (prevents EOF when no writers connected), scans lines, parses each into an Event, and pushes to a channel.

**Key behaviors:**
- Creates the FIFO directory and file if they don't exist (via `syscall.Mkfifo`)
- Opens with O_RDWR to prevent EOF on no-writer
- Uses `bufio.Scanner` for line-by-line reading
- Parses each line via `ParseEvent()` from task 1
- Pushes valid events to `chan<- Event`; logs and skips malformed lines
- Respects context cancellation for clean shutdown
- `Run(ctx context.Context, events chan<- Event) error` — blocking, returns on ctx.Done
- `Cleanup()` — removes the FIFO file

Steps:
- [ ] Write failing tests: create temp FIFO, write events, verify reader parses them into channel. Test EOF prevention (no writer connected, reader doesn't exit). Test malformed lines are skipped. Test context cancellation stops reader.
- [ ] Run tests to verify they fail
- [ ] Implement FIFOReader struct with New, Run, Cleanup methods
- [ ] Run tests to verify they pass
- [ ] Commit: `feat(coach): add FIFO reader`

---

## Task 3: Matching Engine

**Files:**
- Create: `internal/coach/matcher.go`
- Test: `internal/coach/matcher_test.go`

The matcher holds a hash map for O(1) exact match and a radix trie for O(L) longest-prefix parameterized match. Built from KB entries at startup.

**Types:**
- `Suggestion` struct: ActionID, Tool, UserAction, Optimal, SourceFile, SourceLine, KeysSaved
- `Matcher` struct with `Build(entries []kb.KBEntry)` and `Match(source string, action string) *Suggestion`

**Normalization:** strip leading sudo/noglob/nocorrect/command, strip trailing pipes, collapse whitespace, normalize `&&`/`;` separators.

**Matching logic:**
1. Normalize the input command
2. Exact match against hash map: `normalized == expansion` or `normalized starts with expansion + " "`
3. If no exact match: longest-prefix match in radix trie, extract remaining args, construct suggestion
4. Only suggest if alias is shorter than what was typed (keystroke savings > 0)
5. Return nil if no match

Steps:
- [ ] Write failing tests for normalization (sudo stripping, pipe stripping, whitespace collapse — table-driven, 8+ cases)
- [ ] Run tests to verify they fail
- [ ] Implement normalize function
- [ ] Run tests to verify they pass
- [ ] Write failing tests for exact matching (direct match, prefix match with args, no-match, alias-longer-than-typed rejection — table-driven)
- [ ] Run tests to verify they fail
- [ ] Implement hash map exact matcher
- [ ] Run tests to verify they pass
- [ ] Write failing tests for parameterized matching (longest prefix, remaining args extraction, no-prefix-match — table-driven)
- [ ] Run tests to verify they fail
- [ ] Implement radix trie and prefix matcher
- [ ] Run tests to verify they pass
- [ ] Write failing test for Build (load from mock KB entries, verify both hash map and trie populated)
- [ ] Run tests to verify they pass
- [ ] Commit: `feat(coach): add matching engine with radix trie`

---

## Task 4: Throttle + Graduation

**Files:**
- Create: `internal/coach/throttle.go`
- Create: `internal/coach/graduation.go`
- Test: `internal/coach/throttle_test.go`
- Test: `internal/coach/graduation_test.go`

**Throttle** implements the five anti-annoyance gates. `Allow(actionID string, source string) bool` checks all gates in order and returns false if any blocks.

Gates:
1. Per-action cooldown — `golang.org/x/time/rate.Limiter` per action ID stored in a sync.Mutex-protected map. Default 120s.
2. Hourly budget — in-memory counter, resets on the hour. Default 5/hour.
3. Graduation + per-state interval — delegates to graduation module. If GRADUATED, skip entirely. If LEARNING, check `next_coach_after` timestamp (spaced: 1d→3d→7d). If IMPROVING, check weekly interval. Only NOVICE coaches on every occurrence.
4. Quiet hours — parse "HH:MM-HH:MM" format, handle midnight crossing, check against current local time.
5. Focus mode — if focus_category is set and source's category doesn't match, block.

Both Throttle and Correlator accept an injectable `now func() time.Time` parameter (defaulting to `time.Now`) for deterministic testing. No `time.Now()` calls in production code paths — always use the injected clock.

`Persist(db)` / `Load(db)` — save/restore hourly budget counter and per-action limiter timestamps to SQLite on shutdown/startup.

**Graduation** implements the state machine using `qmuntal/stateless` with external SQLite storage.

States: Novice, Learning, Improving, Graduated
Triggers: OptimalAction, SuboptimalAction

Transitions per spec section 5.2. Each action_id has its own state machine instance. State loaded from `coaching_state` table on first access, cached in memory, persisted on mutation.

`RecordOptimal(actionID)` / `RecordSuboptimal(actionID)` — fire triggers, update DB.
`IsGraduated(actionID) bool` — fast check for throttle gate 3.
`GetState(actionID) string` — for stats display.

Steps:
- [ ] Write failing tests for quiet hours parsing (normal range, midnight crossing, edge cases at boundaries — table-driven)
- [ ] Run tests to verify they fail
- [ ] Implement quiet hours parser and checker
- [ ] Run tests to verify they pass
- [ ] Write failing tests for throttle gates (cooldown blocks within window, allows after; budget exhaustion blocks, resets on hour; focus mode blocks wrong category; per-state interval: LEARNING blocks before next_coach_after, allows after; IMPROVING blocks before 7 days — all using injectable clock, table-driven)
- [ ] Run tests to verify they fail
- [ ] Implement Throttle struct with Allow method and all five gates including per-state interval logic
- [ ] Run tests to verify they pass
- [ ] Write failing tests for graduation state machine (full transition table from spec: novice→learning on first adoption, learning→improving on 3 consecutive, improving→graduated on 7 over 3+ days, relapse from graduated→improving, streak break learning→novice — table-driven)
- [ ] Run tests to verify they fail
- [ ] Implement Graduation with stateless FSM and SQLite persistence (use temp DB in tests)
- [ ] Run tests to verify they pass
- [ ] Write failing test for adoption tracking (deliver coaching, then next event is optimal within window → was_adopted set, graduation counter increments)
- [ ] Run tests to verify they pass
- [ ] Commit: `feat(coach): add throttle gates and graduation state machine`

---

## Task 5: Message Generation

**Files:**
- Create: `internal/coach/roaster.go`
- Test: `internal/coach/roaster_test.go`

Three-tier message generation per spec section 6.

**Tier 1: Static templates.** A map keyed by `(mode, event_type)` returning a template string with `{variable}` placeholders. Variables: alias, typed, source_file, source_line, chars_typed, chars_saved. Interpolation via simple string replacement. Covers chill, moderate, strict modes.

**Tier 2: Cached LLM pool.** On startup or weekly, batch-generate ~50 messages per category via the Fast LLM. Store in `coaching_messages` table. On message request, select a random unused/least-used message for the category+mode, interpolate variables, increment used_count.

**Tier 3: Live LLM.** For complex coaching (multi-step, milestones). Call Fast LLM with context (user action, optimal action, source file, streak stats). Only when budget allows and LLM is reachable.

**Roaster interface:** `Generate(suggestion Suggestion, mode string) string`
- Try Tier 2 (cached pool) first if messages exist for this category+mode
- Fall back to Tier 1 (template) if no cached messages or LLM was unavailable
- Tier 3 only when explicitly requested (e.g., milestone celebrations)

`RefreshPool(ctx context.Context, llm llm.Provider)` — regenerate cached messages. Called on coach start and periodically.

Steps:
- [ ] Write failing tests for Tier 1 template interpolation (all three modes, all variable substitutions, missing variable handled gracefully — table-driven)
- [ ] Run tests to verify they fail
- [ ] Implement template map and interpolation
- [ ] Run tests to verify they pass
- [ ] Write failing tests for Tier 2 (insert cached messages into temp DB, generate returns cached message, rotation avoids repetition, falls back to Tier 1 when pool empty)
- [ ] Run tests to verify they fail
- [ ] Implement cached pool logic with DB interaction
- [ ] Run tests to verify they pass
- [ ] Write failing test for RefreshPool (mock LLM provider, verify messages inserted into DB with correct category/mode)
- [ ] Run tests to verify they pass
- [ ] Write failing test for Tier 3 live LLM generation (mock LLM provider, call Generate with milestone context, verify LLM invoked with correct system prompt and context, verify returned message)
- [ ] Implement Tier 3 live generation path
- [ ] Run tests to verify they pass
- [ ] Commit: `feat(coach): add 3-tier message generation`

---

## Task 6: Delivery Channels

**Files:**
- Create: `internal/coach/dispatcher.go`
- Test: `internal/coach/dispatcher_test.go`

**Dispatcher** routes coaching messages to the correct delivery channel based on event source and config overrides.

**Routing table:** map of source→channel, loaded from `[coach.delivery]` config. Fallback to `delivery.default` (dunst) for unconfigured sources.

**Channel implementations (behind a `Deliverer` interface):**

1. **InlineShell:** Write message to `$XDG_RUNTIME_DIR/wtfrc/coach-msg` file. The precmd hook (task 9) reads and displays it.
2. **Dunst:** D-Bus notification via godbus/dbus + esiqveland/notify. Uses `x-dunst-stack-tag: wtfrc-coach` to replace previous notification. Urgency: normal.
3. **TmuxStatus:** Write message to `$XDG_RUNTIME_DIR/wtfrc/tmux-status` file, then exec `tmux refresh-client -S`.
4. **Waybar:** Write status JSON to `$XDG_RUNTIME_DIR/wtfrc/waybar-status.json`, then exec `pkill -SIGRTMIN+N waybar` where N is configurable.
5. **NeovimNotify:** Call `vim.notify()` via msgpack-RPC on the connected neovim instance.

`Send(ctx context.Context, message string, source string) error` — resolve channel, delegate to implementation.

Steps:
- [ ] Write failing tests for routing logic (each source maps to correct channel, config override works, unknown source falls back to default — table-driven)
- [ ] Run tests to verify they fail
- [ ] Implement Dispatcher struct with routing table and Deliverer interface
- [ ] Run tests to verify they pass
- [ ] Write failing tests for InlineShell (message written to correct path, file removed after read)
- [ ] Implement InlineShell deliverer
- [ ] Run tests to verify they pass
- [ ] Implement Dunst deliverer (test with mock D-Bus connection or skip D-Bus test if no session bus in CI)
- [ ] Implement TmuxStatus deliverer (test file write + verify tmux command would be called)
- [ ] Implement Waybar deliverer (test file write + verify signal would be sent)
- [ ] Implement NeovimNotify deliverer (test with mock neovim connection)
- [ ] Run all tests
- [ ] Commit: `feat(coach): add delivery channels with context-aware routing`

---

## Task 7: Correlator

**Files:**
- Create: `internal/coach/correlator.go`
- Test: `internal/coach/correlator_test.go`

The correlator pairs keybind notification events (from interceptor configs, Layer 3) with result events (from tool event streams). It maintains a 150ms sliding window.

**Logic:**
- When a keybind event (`IsKeybind=true`) arrives, store it in a per-tool buffer with timestamp
- When a result event arrives, check the buffer: if a keybind from the same tool exists within the last 150ms, consume it (keybind was used → no coaching needed, return nil)
- If no keybind precedes the result event within 150ms, return the result event as a coaching opportunity
- Periodically expire old keybind events from the buffer (>150ms)

**Interface:** `Process(ev Event) *Event` — returns nil if keybind was used, returns the event if coaching needed. Accepts injectable `now func() time.Time` for deterministic testing.

Steps:
- [ ] Write failing tests using injectable clock: keybind followed by result within 150ms → returns nil. Result with no preceding keybind → returns event. Keybind from different tool doesn't satisfy correlation. Expired keybind (>150ms, advance clock) doesn't satisfy. Multiple rapid keybinds, one result → first consumed. Table-driven.
- [ ] Run tests to verify they fail
- [ ] Implement Correlator with per-tool buffer, timestamp-based expiry, and injectable clock
- [ ] Run tests to verify they pass
- [ ] Commit: `feat(coach): add keybind-result event correlator`

---

## Task 8: Daemon Lifecycle

**Files:**
- Create: `internal/coach/daemon.go`
- Test: `internal/coach/daemon_test.go`

The daemon wires all components together in an errgroup-based event loop.

**Components wired:**
- FIFOReader (mandatory, from task 2) → events channel
- Tool sources (optional, from task 12) → same events channel
- processEvents goroutine: reads channel → **record to usage_events (ALL events)** → correlator → matcher → throttle → roaster → dispatcher → **record to coaching_log (coached events only)**
- Strict mode socket listener (if strict enabled): Unix domain socket at `$XDG_RUNTIME_DIR/wtfrc/coach-strict.sock` accepting synchronous command-check requests. Queries matcher, responds allow/reject within 100ms budget.
- Signal handler: SIGHUP → reload config + rebuild matcher. SIGTERM/SIGINT → sd_notify STOPPING, cancel context.
- Watchdog: ping sd_notify every 10s with health check (FIFO readable, DB queryable)

**usage_events recording:** Every event that passes through the correlator (including non-matching and already-graduated ones) is recorded in the `usage_events` table. This captures ALL user actions for aggregate efficiency metrics (spec section 10.2). The coaching_log records only events where a message was delivered.

**Config hot reload:** On SIGHUP, re-read config.toml, rebuild matcher from KB, update throttle params. New config stored via `atomic.Pointer[Config]`. Lock-free reads on hot path.

**Daemon struct:** holds all components, config pointer, DB handle, LLM provider.
- `New(cfg *config.Config, db *kb.DB, fastLLM llm.Provider) *Daemon`
- `Run(ctx context.Context) error` — blocking, returns on context cancellation or fatal error

Steps:
- [ ] Write failing test: create daemon with mock components (mock FIFO reader that emits test events, mock matcher, mock throttle, mock roaster, mock dispatcher, mock DB). Verify full pipeline: event → usage_events recorded → correlate → match → throttle → generate → dispatch → coaching_log recorded.
- [ ] Run test to verify it fails
- [ ] Implement Daemon struct with Run method using errgroup, wiring all components including usage_events and coaching_log recording
- [ ] Run test to verify it passes
- [ ] Write failing test for usage_events recording: emit events through pipeline (including non-matching ones), verify ALL events recorded in usage_events table
- [ ] Run test to verify it passes
- [ ] Write failing test for strict mode socket: start daemon with strict enabled, connect to coach-strict.sock, send a command that has an alias match, verify "reject" response. Send a command with no match, verify "allow" response. Test timeout/unreachable → fail-open.
- [ ] Implement strict mode socket listener as a goroutine in the daemon
- [ ] Run test to verify it passes
- [ ] Write failing test for config reload: start daemon, send SIGHUP (or call reload method), verify matcher rebuilt and throttle params updated
- [ ] Run test to verify it passes
- [ ] Write failing test for graceful shutdown: start daemon, cancel context, verify graduation state persisted and all goroutines exit cleanly
- [ ] Run test to verify it passes
- [ ] Commit: `feat(coach): add daemon lifecycle with errgroup, hot reload, and strict mode`

---

## Task 9: Layer 1 — Shell Coaching

**Files:**
- Create: `scripts/wtfrc-coach.zsh`
- Test: (manual testing — shell hooks can't be unit tested in Go)

The shell hook consists of a `preexec` function (writes command to FIFO, backgrounded) and a `precmd` function (checks for coach-msg file, displays, removes).

**wtfrc-coach.zsh contents:**
- `_wtfrc_coach_preexec`: writes `shell\t$1` to FIFO via `>>`, backgrounded with `&!`. Checks FIFO exists before writing.
- `_wtfrc_coach_precmd`: checks for `$XDG_RUNTIME_DIR/wtfrc/coach-msg` file, cats and removes it if present.
- For strict mode: `_wtfrc_strict_preexec` variant that uses socat to synchronously query `coach-strict.sock` with 100ms timeout. Fail-open on timeout.
- Both hooks registered via `autoload -Uz add-zsh-hook; add-zsh-hook preexec _wtfrc_coach_preexec`

The daemon's interceptor module (task 11) handles installing/removing this file to `~/.config/zsh/conf.d/`.

Steps:
- [ ] Write the zsh hook script following the behaviors described above
- [ ] Write a Go test that verifies the script content is syntactically valid (shell-parse or regex check for required function names and hook registrations)
- [ ] Run test to verify it passes
- [ ] Commit: `feat(coach): add zsh preexec/precmd hooks`

---

## Task 10: Layer 2 — Editor Coaching

**Files:**
- Create: `scripts/wtfrc-coach.lua`
- Test: (manual testing — neovim plugins can't be unit tested in Go)

The neovim plugin uses `vim.on_key()` with:
- Single FIFO fd opened lazily on first event, kept open for subsequent writes
- `pcall` wrappers on all I/O (FIFO missing or daemon dead never freezes nvim)
- 200ms debounce via `vim.uv.now()` comparison
- Detection: arrow keys in normal mode, mouse scroll in normal mode
- Cleanup on `VimLeavePre` autocmd

The daemon's interceptor module (task 11) handles installing/removing this file to `~/.config/nvim/plugin/`.

Steps:
- [ ] Write the neovim Lua plugin following the behaviors described above
- [ ] Write a Go test that verifies the Lua script content contains required patterns (vim.on_key, pcall, VimLeavePre)
- [ ] Run test to verify it passes
- [ ] Commit: `feat(coach): add neovim vim.on_key() coaching plugin`

---

## Task 11: Layer 3 — Interceptor Config Generation

**Files:**
- Create: `internal/coach/interceptor.go`
- Test: `internal/coach/interceptor_test.go`

Generates per-tool wrapper configs and manages their lifecycle (install, remove, regenerate).

**Per-tool generators:** Each tool has a generate function that takes a list of KB entries (keybinds for that tool) and produces the interceptor config string in that tool's syntax. The FIFO path is injected as a parameter.

| Tool | Generator | Output |
|------|-----------|--------|
| Hyprland | Wraps each `bind =` line with exec FIFO write + hyprctl dispatch | `.conf` file |
| tmux | Wraps each `bind-key` with run-shell FIFO write + `\;` original | `.conf` file |
| kitty | Wraps each `map` with combine + launch --type=background | `.conf` file |
| qutebrowser | Wraps each `config.bind()` with spawn -d + `;;` original | `.py` file |
| yazi | Wraps each keymap entry with shell FIFO write prepended to run array | `.toml` file |

**Lifecycle methods:**
- `Install(db *kb.DB, fifoPath string) error` — query KB for all keybind entries, generate per-tool configs, write to tool config dirs, reload tools (hyprctl reload, tmux source-file, etc.)
- `Remove() error` — write empty files to all interceptor paths, reload tools
- `Regenerate(db *kb.DB, fifoPath string) error` — Remove then Install

**Source line management:** `EnsureSourceLines()` checks each tool's main config for the source/include line pointing to the interceptor file. If missing, appends it. Called during `wtfrc setup`.

Steps:
- [ ] Write failing tests for Hyprland generator (input: list of KB entries with tool="hyprland", type="keybind"; verify output contains correct bind syntax with FIFO write and hyprctl dispatch — table-driven with 3+ keybind variations)
- [ ] Run tests to verify they fail
- [ ] Implement Hyprland generator
- [ ] Run tests to verify they pass
- [ ] Write failing tests for tmux generator (same pattern — verify `\;` chaining and run-shell syntax)
- [ ] Implement tmux generator
- [ ] Run tests to verify they pass
- [ ] Write failing tests for kitty generator (verify combine + launch --type=background)
- [ ] Implement kitty generator
- [ ] Run tests to verify they pass
- [ ] Write failing tests for qutebrowser generator (verify spawn -d + ;; chaining)
- [ ] Implement qutebrowser generator
- [ ] Run tests to verify they pass
- [ ] Write failing tests for yazi generator (verify TOML array run with shell prepended)
- [ ] Implement yazi generator
- [ ] Run tests to verify they pass
- [ ] Write failing tests for Install/Remove lifecycle (write to temp dirs, verify files created/emptied)
- [ ] Implement Install, Remove, Regenerate, EnsureSourceLines
- [ ] Run tests to verify they pass
- [ ] Commit: `feat(coach): add interceptor config generation for 5 tools`

---

## Task 12: Layer 3 — Tool Event Sources

**Files:**
- Create: `internal/coach/sources/hyprland.go`
- Create: `internal/coach/sources/tmux.go`
- Create: `internal/coach/sources/neovim.go`
- Create: `internal/coach/sources/yazi.go`
- Test: `internal/coach/sources/hyprland_test.go`
- Test: `internal/coach/sources/tmux_test.go`
- Test: `internal/coach/sources/neovim_test.go`
- Test: `internal/coach/sources/yazi_test.go`

Each source is independently failable. If the tool is not running, the source enters dormant/retry state. Interface: `RunOptional(ctx context.Context, events chan<- Event)` — never returns a fatal error, only logs warnings.

**Hyprland source:** Connect to socket2 at `$XDG_RUNTIME_DIR/hypr/$HYPRLAND_INSTANCE_SIGNATURE/.socket2.sock`. Line-scan for `EVENTTYPE>>DATA\n`. Parse into Event structs for events of interest (activewindow, openwindow, movewindow, fullscreen). Reconnect with backoff on connection loss. No-op if `HYPRLAND_INSTANCE_SIGNATURE` env var not set.

**tmux source:** Spawn `tmux -C attach` subprocess. Read stdout for `%` notifications (`%window-pane-changed`, `%session-window-changed`). Parse into Event structs. Restart subprocess with backoff on exit. No-op if `tmux` binary not found.

**neovim source:** Discover neovim instances by scanning for `$NVIM` environment variable or known socket paths. Connect via `neovim/go-client`. Subscribe to custom autocmd events via `rpcnotify`. Manage multiple instances (editors open/close dynamically). No-op if no neovim instances found.

**yazi source:** Spawn `ya sub cd,hover` subprocess. Read stdout for DDS events. Parse into Event structs. Restart with backoff on exit. No-op if `ya` binary not found.

Steps:
- [ ] Write failing tests for Hyprland event parsing (input: raw socket2 lines like "activewindow>>kitty,vim"; verify Event struct with correct Source, Action, Context — table-driven with 4+ event types)
- [ ] Run tests to verify they fail
- [ ] Implement Hyprland source with socket connection, line scanner, event parser, and reconnect logic
- [ ] Run tests to verify they pass
- [ ] Write failing tests for tmux event parsing (input: control mode notifications like "%window-pane-changed @1 %3"; verify Event struct)
- [ ] Implement tmux source with subprocess management and event parser
- [ ] Run tests to verify they pass
- [ ] Write failing tests for yazi event parsing (input: DDS event lines; verify Event struct)
- [ ] Implement yazi source with subprocess management
- [ ] Run tests to verify they pass
- [ ] Implement neovim source with instance discovery and RPC subscription (test with mock socket)
- [ ] Run all source tests
- [ ] Commit: `feat(coach): add tool event sources (hyprland, tmux, neovim, yazi)`

---

## Task 13: CLI Commands

**Files:**
- Create: `internal/cli/coach.go`
- Create: `internal/cli/coach_test.go`
- Modify: `internal/cli/root.go` (update Long description to include coach)
- Modify: `configs/default.toml`

The coach CLI wires everything together. Subcommands: start, stop, status, reload, stats, log, graduated.

**coach start:**
1. Call `newDeps()` for config, DB, LLM providers
2. Create FIFO
3. Call `interceptor.Install()` for Layer 3
4. Install shell hook (copy `scripts/wtfrc-coach.zsh` to `~/.config/zsh/conf.d/`)
5. Install neovim plugin (copy `scripts/wtfrc-coach.lua` to `~/.config/nvim/plugin/`)
6. If layer4 enabled, start wtfrc-monitor subprocess
7. Create Daemon, call `daemon.Run(ctx)` (blocking)

**coach stop:**
1. Send SIGTERM to running daemon (find PID from pidfile or systemd)
2. Call `interceptor.Remove()`
3. Remove shell hook and neovim plugin files
4. Remove FIFO

**coach status:** Query daemon state (via a small status socket or by reading state files): mode, budget remaining this hour, total graduated actions, active sources.

**coach reload:** Send SIGHUP to running daemon.

**coach stats:** Query coaching_log and coaching_state tables, display summary (total coached, adoption rate, top graduated actions, current streaks).

**coach log:** Query coaching_log table, display recent entries with timestamp, source, user action, optimal action, and whether adopted.

**coach graduated:** Query coaching_state where state='graduated', display list with action details and graduation date.

**Flags:** --mode, --snooze, --focus, --layer4, --strict (all per spec section 8).

Note: `coach start` is the foreground-blocking entry point (used by systemd `ExecStart=/usr/bin/wtfrc coach start`). There is no separate `coach daemon` subcommand.

Steps:
- [ ] Write the coach command with subcommand registration following existing cobra patterns in root.go and ask.go
- [ ] Update root.go Long description to include `wtfrc coach`
- [ ] Write failing tests for stats/log/graduated DB query formatting (insert test data, call query functions, verify output structure — table-driven)
- [ ] Implement `coach stats`, `coach log`, `coach graduated` with DB queries and lipgloss formatting
- [ ] Run tests to verify they pass
- [ ] Write failing test for flag parsing (verify --mode, --snooze, --focus, --layer4, --strict flags are registered and parsed correctly)
- [ ] Implement `coach start` wiring all components from tasks 1-12
- [ ] Implement `coach stop` with process signaling and cleanup
- [ ] Implement `coach status` reading daemon state
- [ ] Implement `coach reload` (SIGHUP sender)
- [ ] Add `[coach]` section to configs/default.toml with all defaults from spec
- [ ] Run `go build ./cmd/wtfrc` to verify compilation
- [ ] Run `go test ./internal/cli/... -v` to verify CLI tests pass
- [ ] Commit: `feat(coach): add CLI commands (start, stop, status, reload, stats, log, graduated)`

---

## Task 14: Layer 4 — OS-Level Input Monitor

**Files:**
- Create: `internal/monitor/monitor.go`
- Create: `internal/monitor/device.go`
- Create: `internal/monitor/classifier.go`
- Create: `cmd/wtfrc-monitor/main.go`
- Test: `internal/monitor/classifier_test.go`
- Test: `internal/monitor/device_test.go`

Separate binary that passively reads evdev devices and writes classified key combos + mouse events to the FIFO.

**Classifier:** Tracks modifier state as a bitmask. On non-modifier key-down with active modifiers, formats and emits the combo. Mouse buttons handled before key combo logic to prevent double-emission. Single characters without modifiers are discarded (privacy boundary). Configurable `emit_shift_chars` flag.

**Device discovery:** Enumerate `/dev/input/event*`, check capabilities via ioctl, prefer keyd virtual device, find primary keyboard and mouse. One goroutine per device, fan-in to classifier. Hotplug via inotify on `/dev/input/`.

**Monitor main loop:** Discover devices → spawn reader goroutines → classifier → FIFO writer. Respects context cancellation. Checks `input` group membership on startup, exits with clear error if missing.

**Entry point (`cmd/wtfrc-monitor/main.go`):** Parses FIFO path from args or env, creates monitor, runs until SIGTERM/SIGINT.

Steps:
- [ ] Write failing tests for classifier: modifier tracking (Super down + j down → "$mod+j"), mouse button before key combo logic (Ctrl+click → only mouse:click:left, not also Ctrl+BTN_LEFT), privacy filter (single char without modifier → discarded), emit_shift_chars flag (Shift+G emitted when enabled, discarded when disabled) — table-driven
- [ ] Run tests to verify they fail
- [ ] Implement classifier with modifier bitmask and privacy filter
- [ ] Run tests to verify they pass
- [ ] Write failing tests for device discovery: mock `/dev/input/` directory with test device files, verify correct keyboard and mouse selected, verify keyd virtual device preferred
- [ ] Implement device discovery with capability checking
- [ ] Run tests to verify they pass
- [ ] Implement monitor main loop wiring device readers → classifier → FIFO
- [ ] Write failing test for monitor main loop: create mock devices (via test helpers), verify events flow through classifier to a test FIFO
- [ ] Implement monitor main loop wiring device readers → classifier → FIFO
- [ ] Run test to verify it passes
- [ ] Implement cmd/wtfrc-monitor/main.go entry point
- [ ] Run `go build ./cmd/wtfrc-monitor` to verify compilation
- [ ] Commit: `feat(coach): add Layer 4 OS-level input monitor`

---

## Task 15: Integration Testing + systemd Service

**Files:**
- Create: `configs/wtfrc-coach.service`
- Modify: `Makefile`

**systemd user service file:** Type=notify, ExecStart=/usr/bin/wtfrc coach start, ExecReload=kill -HUP, WatchdogSec=30s, Restart=on-failure, RestartSec=5.

**Makefile additions:**
- `make build-monitor` — builds wtfrc-monitor binary
- `make install-service` — copies service file to `~/.config/systemd/user/` and reloads systemd

**Integration tests:**
- Full pipeline test with mock FIFO: write shell events → verify coaching messages appear in mock dispatcher
- Config reload test: change config, send SIGHUP, verify behavior changes
- Graduation persistence test: graduate an action, restart daemon, verify graduation state preserved
- Interceptor round-trip test: generate interceptor config for each tool, parse back, verify structural correctness

Steps:
- [ ] Write the systemd service unit file
- [ ] Add Makefile targets for build-monitor and install-service
- [ ] Write integration test: full pipeline from FIFO event to dispatcher output (using real SQLite temp DB, mock LLM)
- [ ] Run integration test to verify it passes
- [ ] Write integration test: graduation persistence across daemon restart
- [ ] Run test to verify it passes
- [ ] Run full test suite: `go test ./internal/coach/... ./internal/monitor/... -v -count=1`
- [ ] Run linter: `go vet ./...`
- [ ] Commit: `feat(coach): add systemd service and integration tests`
- [ ] Final commit: update README.md with coach documentation
