# wtfrc E2E Test Harness — Design Specification

> Full end-to-end testing against real Linux desktop tools in an isolated, reproducible environment.

**Date:** 2026-03-23
**Status:** Draft
**Depends on:** v0.2 "Coach" (in progress on `feat/v0.2-coach`)

---

## 1. Overview

The existing test suite covers unit tests (mocked dependencies) and integration tests (real DB, mock I/O). What is missing is end-to-end validation against real running tools: a real Hyprland compositor processing real input events, a real tmux server, a real neovim instance, real dunst delivering real D-Bus notifications, and a real zsh shell executing real commands with real preexec hooks.

The E2E harness provides this by running tests inside a QEMU virtual machine with a fully provisioned Arch Linux desktop. Tests simulate real user input via uinput virtual devices and assert on real outputs via D-Bus signal capture, file content inspection, and database queries.

**Design goals:**
- Test the exact same code path a real user exercises, from keystroke to coaching notification
- Work both locally (developer on an Arch/Hyprland desktop) and in CI (GitHub Actions with KVM)
- Remain invisible to the developer: `make e2e` handles everything
- Fail loud and fast: each test has a bounded timeout, VM boots are cached

---

## 2. Environment Strategy

### 2.1. Why a VM Instead of Nested Compositors

Hyprland cannot run headless without a GPU or DRM backend since its Aquamarine rendering library requires at minimum a software-rendered DRM device. Running Hyprland nested inside another Wayland compositor (cage, weston) is fragile: Hyprland's Wayland backend expects a functional wl_display and the interaction between two compositors' input handling makes uinput injection unreliable.

A QEMU VM with virtio-gpu-pci and LLVMpipe provides a real DRM device that Aquamarine accepts. Hyprland runs as the primary compositor on a virtual VT, identical to bare metal. uinput devices inside the VM are processed through libinput into Hyprland exactly as on real hardware.

### 2.2. VM Configuration

The VM runs with:
- KVM acceleration (mandatory for acceptable boot time)
- virtio-gpu-pci (provides a DRM device; Mesa LLVMpipe handles GL in software)
- 2 vCPUs, 2 GB RAM (sufficient for compositor + test tools)
- virtio-net with user-mode networking and host SSH port forward
- A cloud-init seed ISO for first-boot provisioning
- Shared 9p mount for passing the wtfrc binary and test artifacts between host and guest

### 2.3. Local vs. CI Detection

The test runner auto-detects the environment:
- If `HYPRLAND_INSTANCE_SIGNATURE` is set and a Hyprland IPC socket exists, tests run directly on the host inside a dedicated Hyprland workspace (local developer mode)
- Otherwise, tests boot the VM image and run inside the guest via SSH (CI mode, or local without Hyprland)

In local mode, the test runner creates a scratch workspace in Hyprland, runs all tools there, and tears it down afterward. This avoids interfering with the developer's session.

---

## 3. VM Image Build Pipeline

### 3.1. Base Image

The official Arch Linux cloud image (archlinux/arch-boxes, qcow2 format) is used as the base. It ships with cloud-init, systemd, and a minimal package set. The image is downloaded from the Arch mirror and cached.

### 3.2. Provisioning

A cloud-init user-data file handles first-boot setup:
- Creates a test user with passwordless sudo and membership in the `input` group (for uinput)
- Installs packages: hyprland, tmux, neovim, zsh, dunst, kitty, waybar, mesa (LLVMpipe), dbus, socat
- Configures auto-login on tty1 via a systemd override
- Drops a minimal Hyprland config with known keybindings (Super+j/k for focus, Super+Return for terminal)
- Drops a zsh config that sources the wtfrc preexec/precmd hooks
- Drops a neovim config that loads the wtfrc coaching plugin
- Configures dunst with default settings
- Sets Hyprland as the login shell's session launcher (starts on tty1 login)
- Loads the uinput kernel module and sets permissions on /dev/uinput for the test user
- Installs an SSH authorized key for the host test runner

### 3.3. wtfrc Binary Injection

The wtfrc binary is not baked into the image. Instead, it is cross-compiled on the host (GOOS=linux GOARCH=amd64) and copied into the VM at test time via the 9p shared mount or SCP. This means the image is built once and reused across code changes.

### 3.4. Image Caching

The provisioned image (after first cloud-init boot) is snapshotted as a qcow2 overlay. Subsequent test runs use the snapshot, skipping the provisioning boot entirely. In CI, the base image and snapshot are cached via GitHub Actions cache.

---

## 4. Test Runner Architecture

### 4.1. Host-Side Orchestrator

A Go test binary (build-tagged `e2e`) runs on the host. It is responsible for:
- Building the wtfrc binary for linux/amd64
- Starting the QEMU VM (or detecting local Hyprland)
- Waiting for SSH readiness (VM) or IPC socket (local)
- Copying the wtfrc binary into the guest
- Running `wtfrc index` and `wtfrc coach start` inside the guest
- Executing each test case as a Go subtest
- Collecting results and tearing down

### 4.2. Guest-Side Test Agent

A lightweight Go binary runs inside the VM as the test executor. It receives commands from the host over SSH (or is invoked directly in local mode) and performs:
- uinput device creation and input injection
- D-Bus session bus connection for notification capture
- File reads for inline coach messages
- SQLite queries against the wtfrc database
- Tool-specific APIs (tmux send-keys, nvim RPC feedkeys, hyprctl dispatch)

The agent exposes a simple JSON-over-stdin/stdout protocol. The host sends a test action, the agent executes it, and returns a structured result.

### 4.3. Communication Flow

Host orchestrator connects to guest via SSH. Each test step is a command sent to the agent:
1. Host: "inject keyboard input: Super+Return" (agent uses uinput)
2. Host: "wait for condition: hyprland active window title contains 'kitty'" (agent polls hyprctl)
3. Host: "inject keyboard input: g i t space s t a t u s Enter" (agent types into terminal)
4. Host: "wait for condition: D-Bus notification with body containing 'gs'" (agent listens on D-Bus)
5. Host: "query DB: SELECT count(*) FROM coaching_log" (agent reads SQLite)

---

## 5. Input Simulation

### 5.1. uinput Virtual Devices

The agent creates two virtual devices via the Linux uinput subsystem (using the bendahl/uinput Go library):
- A virtual keyboard registered with all standard keycodes including modifiers
- A virtual mouse with relative movement and button capabilities

These devices appear as real evdev inputs. Hyprland's libinput backend processes them identically to physical hardware. There is no API mocking involved.

### 5.2. Keyboard Simulation

Key combos are simulated by holding modifier keys (KeyDown), pressing the target key (KeyPress), then releasing modifiers (KeyUp). A small delay (20ms) between events prevents races in the compositor's input processing.

Typing text (for shell commands) is simulated as a sequence of KeyPress calls with appropriate shift handling for uppercase and symbols.

### 5.3. Mouse Simulation

Mouse movement is injected as relative axis events. Click-to-focus is simulated by moving the virtual mouse to a known screen coordinate (computed from Hyprland's layout via hyprctl clients) and issuing a left-click.

### 5.4. Tool-Native APIs

Where uinput is not the right tool, native APIs are used:
- tmux: `tmux send-keys` for typing into panes, `tmux select-pane` for pane switching
- neovim: msgpack-RPC `nvim_feedkeys` for keystroke injection, `nvim_command` for ex commands
- zsh: commands are typed via uinput into the terminal emulator (kitty) where zsh is running

The choice depends on what the test is validating. To test that Hyprland processes a keybind correctly, uinput is required. To test that neovim arrow key coaching works, nvim_feedkeys is sufficient since the neovim plugin is the detection layer.

---

## 6. Output Assertion

### 6.1. D-Bus Notification Capture

The agent subscribes to the session D-Bus at the org.freedesktop.Notifications interface using godbus. When the coach daemon sends a notification via the dunst delivery channel, the agent captures the method call (Notify) and records the summary, body, and hints. Tests assert on notification content using substring or regex matches.

An alternative for verification: the agent can also become the notification server itself (claiming org.freedesktop.Notifications on the bus before dunst starts), giving it direct control over what notifications are received. This is more reliable than racing with dunst but changes the test fidelity. The design supports both modes via a configuration flag.

### 6.2. Inline Shell Messages

For shell coaching, the coach daemon writes to a file at `$XDG_RUNTIME_DIR/wtfrc/coach-msg`. The agent reads this file after each shell command and asserts on its content.

### 6.3. Database State

The agent opens the wtfrc SQLite database (same file the daemon uses, via a read-only connection) and runs queries against coaching_state, coaching_log, and usage_events tables.

### 6.4. Hyprland State

The agent queries `hyprctl activewindow`, `hyprctl clients`, and `hyprctl workspaces` in JSON mode to assert on compositor state (which window is focused, how many clients exist, etc.).

### 6.5. tmux State

The agent queries `tmux list-panes`, `tmux display-message`, and `tmux capture-pane` to assert on tmux state and pane contents.

---

## 7. Test Cases

All ten required test cases, organized by detection layer.

### 7.1. TC01: Shell Alias Coaching

**Layer:** L1 (shell preexec)
**Setup:** zsh with wtfrc hooks active, KB indexed with alias `gs` = `git status`
**Input:** Type `git status` followed by Enter into the terminal via uinput
**Expected:** Inline coach-msg file is written containing the alias `gs`. coaching_log has one entry with optimal_action = "gs".

### 7.2. TC02: Keybind Used, No Coaching (Correlator)

**Layer:** L3 (interceptor + correlator)
**Setup:** Hyprland running with wtfrc interceptor config. Keybind Super+j bound to focus down, with interceptor FIFO write.
**Input:** Press Super+j via uinput
**Expected:** Hyprland dispatches the focus action. The FIFO receives a keybind event. The correlator pairs it with the Hyprland socket2 activewindow event. No coaching message is generated. coaching_log remains empty for this action.
**Assertion:** hyprctl confirms focus changed. No D-Bus notification was sent. No new coaching_log row.

### 7.3. TC03: Mouse Click to Focus Window, Coaching via Dunst

**Layer:** L3 (correlator detects unpaired result event) or L4 (evdev mouse click)
**Setup:** Two windows open on the same workspace. Active focus on window A.
**Input:** Move virtual mouse to window B's coordinates and left-click via uinput
**Expected:** Hyprland socket2 emits activewindow event. No preceding keybind event in the correlation window. The correlator classifies this as mouse/manual. A dunst notification is delivered coaching the user to use the focus keybind.
**Assertion:** D-Bus captures a Notify call with body referencing the focus keybind. coaching_log has an entry.

### 7.4. TC04: Neovim Arrow Keys, Coaching

**Layer:** L2 (neovim plugin)
**Setup:** neovim running with wtfrc Lua plugin loaded
**Input:** Send arrow key presses via nvim_feedkeys (or uinput if neovim is in the terminal)
**Expected:** The neovim plugin detects arrow key usage in normal mode and writes an event to the FIFO. The daemon matches it against hjkl motions and delivers a coaching message via vim.notify (or dunst).
**Assertion:** neovim received a vim.notify call (verifiable via nvim RPC), or a D-Bus notification was captured. coaching_log has an entry with source "nvim".

### 7.5. TC05: tmux Mouse Pane Switch, Coaching

**Layer:** L3 (tmux event source)
**Setup:** tmux session with two panes, wtfrc subscribed via tmux control mode
**Input:** Click on the inactive pane via tmux send-keys with mouse event, or via uinput mouse click at the pane's screen coordinates
**Expected:** tmux emits a pane-changed event. No keybind event preceded it. Coaching message delivered suggesting the select-pane keybind.
**Assertion:** tmux status file or D-Bus notification contains coaching text. coaching_log entry exists.

### 7.6. TC06: Graduation (7 Optimal Uses Silence Coaching)

**Layer:** L1 (shell)
**Setup:** KB with alias `gs` = `git status`, graduation_streak set to 7
**Input:** Type `gs` (the optimal alias) 7 times, then type `git status` (suboptimal)
**Expected:** After 7 consecutive optimal uses, the action transitions to GRADUATED state. The subsequent suboptimal use does NOT trigger coaching because the action is graduated (it enters IMPROVING on relapse, but the first relapse after graduation may or may not coach depending on the interval — the test verifies the graduation state machine).
**Assertion:** coaching_state row for `zsh:gs` has state = "graduated" after 7 optimal uses. After the 8th suboptimal use, state transitions to "improving". The coaching behavior follows the graduation FSM rules.

### 7.7. TC07: Budget Exhaustion

**Layer:** L1 (shell)
**Setup:** budget_per_hour = 3, cooldown_seconds = 0 (to avoid cooldown interference)
**Input:** Type 5 different suboptimal commands that each have a matching alias
**Expected:** The first 3 trigger coaching messages. The 4th and 5th are suppressed by the budget gate.
**Assertion:** coaching_log has exactly 3 entries. usage_events has 5 entries.

### 7.8. TC08: Strict Mode Shell Blocking

**Layer:** L1 (shell strict mode)
**Setup:** coach started with --strict, socat available, preexec hook configured for strict mode socket
**Input:** Type a suboptimal command into zsh
**Expected:** The preexec hook sends the command to the coach-strict.sock. The daemon responds "reject". The shell cancels command execution. The command is NOT executed.
**Assertion:** The command's side effect did not occur (e.g., a file that would have been created does not exist). An inline coaching message was delivered. coaching_log records the blocked event.

### 7.9. TC09: Config Reload (Mode Change)

**Layer:** Cross-cutting
**Setup:** Coach running in "chill" mode
**Input:** Modify the config file to set mode = "moderate". Send SIGHUP to the daemon.
**Expected:** The daemon reloads the config. Subsequent coaching messages use the moderate template style.
**Assertion:** Send a shell command that triggers coaching. The coaching_log entry has mode = "moderate". The message text differs from the chill template (e.g., includes keystroke count).

### 7.10. TC10: Interceptor Round-Trip

**Layer:** L3 (interceptor)
**Setup:** Hyprland with wtfrc-intercept.conf sourced. A keybind that wraps a FIFO write before the real action.
**Input:** Press the intercepted keybind via uinput
**Expected:** The FIFO receives a `kb:` prefixed event from the interceptor. The original action executes normally (e.g., a window moves to a new workspace). The correlator pairs the keybind event with the Hyprland socket2 result event. No coaching is generated.
**Assertion:** The action took effect (hyprctl confirms workspace change). The keybind event was logged in usage_events. No coaching_log entry.

---

## 8. CI Integration

### 8.1. GitHub Actions Workflow

A dedicated `e2e.yml` workflow runs on pushes to `develop` and on pull requests. It uses `ubuntu-latest` runners with KVM enabled (via /dev/kvm permission setup).

Steps:
1. Checkout code
2. Set up Go from go.mod
3. Enable KVM access (chmod /dev/kvm)
4. Install QEMU and cloud-image-utils
5. Download and cache the Arch cloud image
6. Build the E2E VM image (or restore from cache)
7. Build the wtfrc binary for linux/amd64
8. Run `make e2e`
9. Upload test results and VM screen recording as artifacts on failure

### 8.2. KVM on GitHub Actions

GitHub Actions ubuntu-latest runners have /dev/kvm available. The workflow ensures the runner user has access by adding the user to the kvm group and setting permissions on /dev/kvm. No nested virtualization is needed because the VM runs directly under KVM, not inside another VM.

### 8.3. Timeouts

Each test case has a 30-second timeout. The entire E2E suite has a 10-minute timeout. VM boot (from cached snapshot) takes under 10 seconds with KVM.

### 8.4. Caching Strategy

The base Arch cloud image is cached by its download URL hash. The provisioned snapshot is cached by a hash of the provisioning scripts (cloud-init user-data + package list). If provisioning scripts change, the snapshot is rebuilt.

---

## 9. Makefile Interface

Three targets exposed to the developer:

- `make e2e-image`: Downloads the base Arch cloud image, creates the cloud-init seed ISO, boots the VM for provisioning, and snapshots the result. Idempotent if the snapshot already exists and provisioning scripts have not changed.

- `make e2e`: Builds the wtfrc binary, detects local-vs-VM mode, and runs all E2E tests. In VM mode, boots the snapshot, waits for SSH, copies the binary, and executes the test suite inside the guest. Returns standard Go test exit codes.

- `make e2e-shell`: Boots the VM and opens an interactive SSH session for debugging. The developer can inspect Hyprland, run wtfrc commands, and manually test scenarios.

---

## 10. Directory Structure

All E2E infrastructure lives under `e2e/` at the project root:

- `e2e/harness/` — Host-side orchestrator: VM lifecycle, SSH management, binary deployment, test dispatch
- `e2e/agent/` — Guest-side agent: uinput helpers, D-Bus capture, DB queries, tool API wrappers
- `e2e/testcases/` — Go test files (build-tagged `e2e`), one file per test case or logical group
- `e2e/image/` — VM image build: cloud-init user-data, provisioning scripts, Hyprland/zsh/nvim test configs
- `e2e/image/configs/` — Tool configs for the test VM (hyprland.conf, zshrc, init.lua, dunstrc)
- `e2e/scripts/` — Shell scripts for image build, VM boot, SSH wait

---

## 11. Failure Modes and Mitigations

| Failure | Mitigation |
|---------|------------|
| VM boot hangs | SSH readiness check with 60s timeout; kill QEMU and fail fast |
| Hyprland crash inside VM | Agent checks `hyprctl version` before each test; skip with clear error |
| uinput permission denied | cloud-init provisions /dev/uinput access; agent checks on startup |
| D-Bus notification race | Agent subscribes to D-Bus before triggering the action; 5s poll with backoff |
| Flaky timing (coach message not yet written) | Retry with exponential backoff up to 5s per assertion |
| KVM not available in CI | Workflow checks /dev/kvm; fails immediately with actionable error |
| Stale VM snapshot | Hash-based cache key invalidation on provisioning script changes |

---

## 12. Constraints and Non-Goals

- The E2E harness does NOT test Layer 4 (evdev monitor) in CI because creating uinput devices from within the guest while also reading evdev from the same guest creates a feedback loop. Layer 4 is tested separately with its existing unit tests using mock evdev devices.
- The E2E harness does NOT test waybar signal delivery because waybar requires a running compositor bar and asserting on a status bar pixel change is brittle. Waybar integration is validated by the unit test for WaybarDeliverer.
- The E2E harness does NOT test LLM-generated messages (Tier 2/3). All E2E tests use template-only mode (Tier 1) for deterministic assertions.
- The VM image is x86_64 only. ARM testing is out of scope.
- Tests are NOT designed to run in parallel within a single VM (shared compositor state). They run sequentially.
