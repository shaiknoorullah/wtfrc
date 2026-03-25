package cli

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/shaiknoorullah/wtfrc/internal/coach"
)

// ---------------------------------------------------------------------------
// Flag variables for coach start
// ---------------------------------------------------------------------------

// Flag variables removed: local variables in buildCoachStartCmd are used instead.

// ---------------------------------------------------------------------------
// Command tree
// ---------------------------------------------------------------------------

var coachCmd = &cobra.Command{
	Use:   "coach",
	Short: "Real-time coaching daemon and management commands",
	Long: `Manage the wtfrc coaching daemon, which watches your shell and tool
keybindings and nudges you toward more efficient actions.

  wtfrc coach start      Start the coaching daemon
  wtfrc coach stop       Stop the running daemon
  wtfrc coach status     Show current daemon state
  wtfrc coach reload     Send SIGHUP to reload config
  wtfrc coach stats      Show coaching statistics
  wtfrc coach log        Show recent coaching events
  wtfrc coach graduated  List graduated actions`,
}

func init() {
	coachCmd.AddCommand(buildCoachStartCmd())
	coachCmd.AddCommand(coachStopCmd)
	coachCmd.AddCommand(coachStatusCmd)
	coachCmd.AddCommand(coachReloadCmd)
	coachCmd.AddCommand(coachStatsCmd)
	coachCmd.AddCommand(coachLogCmd)
	coachCmd.AddCommand(coachGraduatedCmd)
	rootCmd.AddCommand(coachCmd)
}

// buildCoachStartCmd constructs the start subcommand. Exposed as a function so
// tests can build a fresh instance without touching global flag variables.
func buildCoachStartCmd() *cobra.Command {
	var mode, focus string
	var layer4, strict bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the coaching daemon",
		Long: `Start the wtfrc coaching daemon.

The daemon reads events from the shell hook and neovim plugin via a named pipe
and provides real-time coaching suggestions when a more efficient action is
available.

The process blocks until interrupted (SIGTERM/SIGINT). Run in the background or
via a systemd user service.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCoachStart(cmd, args, mode, focus, layer4, strict)
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "", "coaching mode: chill, moderate, or strict (overrides config)")
	cmd.Flags().StringVar(&focus, "focus", "", "focus coaching on one category (overrides config)")
	cmd.Flags().BoolVar(&layer4, "layer4", false, "enable Layer 4 OS-level monitor")
	cmd.Flags().BoolVar(&strict, "strict", false, "enable strict mode")

	return cmd
}

// ---------------------------------------------------------------------------
// coach start
// ---------------------------------------------------------------------------

func runCoachStart(_ *cobra.Command, _ []string, mode, focus string, layer4, strict bool) error {
	d, err := newDeps()
	if err != nil {
		return err
	}
	defer d.DB.Close()

	// Resolve runtime dir and paths.
	runtimeDir := xdgRuntimeDir()
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}
	fifoPath := filepath.Join(runtimeDir, "coach.fifo")
	pidPath := filepath.Join(runtimeDir, "coach.pid")

	// Apply flag overrides to config.
	if mode != "" {
		d.Cfg.Coach.Mode = mode
	}
	if strict {
		d.Cfg.Coach.Mode = "strict"
	}
	if focus != "" {
		d.Cfg.Coach.FocusCategory = focus
	}
	if layer4 {
		d.Cfg.Coach.Layer4.Enabled = true
	}

	// Install interceptor configs.
	interceptor := coach.NewInterceptor(fifoPath)
	if err := interceptor.Install(d.DB); err != nil {
		return fmt.Errorf("install interceptor: %w", err)
	}

	// Install shell hook.
	if err := installShellHook(); err != nil {
		// Non-fatal: log and continue.
		fmt.Fprintf(os.Stderr, "wtfrc coach: warning: install shell hook: %v\n", err)
	}

	// Install neovim plugin.
	if err := installNeovimPlugin(); err != nil {
		// Non-fatal: log and continue.
		fmt.Fprintf(os.Stderr, "wtfrc coach: warning: install neovim plugin: %v\n", err)
	}

	// Write PID file.
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}

	// Create and run daemon.
	daemon, err := coach.NewDaemon(d.Cfg, d.DB, d.FastLLM, fifoPath, runtimeDir)
	if err != nil {
		os.Remove(pidPath)
		return fmt.Errorf("create daemon: %w", err)
	}

	fmt.Fprintf(os.Stdout, "wtfrc coach: starting (mode=%s, pid=%d)\n", d.Cfg.Coach.Mode, os.Getpid())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := daemon.Run(ctx)

	// Cleanup on exit.
	if err := interceptor.Remove(); err != nil {
		fmt.Fprintf(os.Stderr, "wtfrc coach: warning: remove interceptor: %v\n", err)
	}
	removeShellHook()
	removeNeovimPlugin()
	os.Remove(pidPath)
	os.Remove(fifoPath)

	if runErr != nil {
		return fmt.Errorf("daemon: %w", runErr)
	}
	return nil
}

// ---------------------------------------------------------------------------
// coach stop
// ---------------------------------------------------------------------------

var coachStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running coaching daemon",
	RunE:  runCoachStop,
}

func runCoachStop(_ *cobra.Command, _ []string) error {
	pid, err := findCoachPID()
	if err != nil {
		return fmt.Errorf("find daemon pid: %w", err)
	}
	if pid == 0 {
		fmt.Fprintln(os.Stdout, "Coach is not running.")
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("send SIGTERM to pid %d: %w", pid, err)
	}

	// Wait up to 5 seconds for the process to exit.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			// Process no longer exists.
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	fmt.Fprintln(os.Stdout, "Coach stopped.")
	return nil
}

// ---------------------------------------------------------------------------
// coach status
// ---------------------------------------------------------------------------

var coachStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show coaching daemon status",
	RunE:  runCoachStatus,
}

func runCoachStatus(_ *cobra.Command, _ []string) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	labelStyle := lipgloss.NewStyle().Bold(true).Width(24)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))

	pid, err := findCoachPID()
	if err != nil || pid == 0 {
		fmt.Fprintln(os.Stdout, "Coach is not running.")
		return nil
	}

	d, err := newDeps()
	if err != nil {
		// Print minimal status even without config.
		fmt.Fprintln(os.Stdout, headerStyle.Render("wtfrc coach status"))
		fmt.Fprintf(os.Stdout, "  %s %s\n", labelStyle.Render("Status:"), valueStyle.Render("running"))
		fmt.Fprintf(os.Stdout, "  %s %s\n", labelStyle.Render("PID:"), valueStyle.Render(strconv.Itoa(pid)))
		return nil
	}
	defer d.DB.Close()

	fmt.Fprintln(os.Stdout, headerStyle.Render("wtfrc coach status"))
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "  %s %s\n", labelStyle.Render("Status:"), valueStyle.Render("running"))
	fmt.Fprintf(os.Stdout, "  %s %s\n", labelStyle.Render("PID:"), valueStyle.Render(strconv.Itoa(pid)))
	fmt.Fprintf(os.Stdout, "  %s %s\n", labelStyle.Render("Mode:"), valueStyle.Render(d.Cfg.Coach.Mode))
	fmt.Fprintf(os.Stdout, "  %s %s\n", labelStyle.Render("Budget/hour:"), valueStyle.Render(strconv.Itoa(d.Cfg.Coach.BudgetPerHour)))
	if d.Cfg.Coach.FocusCategory != "" {
		fmt.Fprintf(os.Stdout, "  %s %s\n", labelStyle.Render("Focus:"), valueStyle.Render(d.Cfg.Coach.FocusCategory))
	}
	fmt.Fprintf(os.Stdout, "  %s %s\n", labelStyle.Render("Config source:"), valueStyle.Render("~/.config/wtfrc/config.toml"))
	return nil
}

// ---------------------------------------------------------------------------
// coach reload
// ---------------------------------------------------------------------------

var coachReloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "Reload config in the running daemon (SIGHUP)",
	RunE:  runCoachReload,
}

func runCoachReload(_ *cobra.Command, _ []string) error {
	pid, err := findCoachPID()
	if err != nil {
		return fmt.Errorf("find daemon pid: %w", err)
	}
	if pid == 0 {
		fmt.Fprintln(os.Stdout, "Coach is not running.")
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}

	if err := proc.Signal(syscall.SIGHUP); err != nil {
		return fmt.Errorf("send SIGHUP to pid %d: %w", pid, err)
	}

	fmt.Fprintln(os.Stdout, "Config reloaded.")
	return nil
}

// ---------------------------------------------------------------------------
// coach stats
// ---------------------------------------------------------------------------

var coachStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show coaching statistics",
	RunE:  runCoachStats,
}

func runCoachStats(_ *cobra.Command, _ []string) error {
	d, err := newDeps()
	if err != nil {
		return err
	}
	defer d.DB.Close()

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	labelStyle := lipgloss.NewStyle().Bold(true).Width(28)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFD700")).PaddingTop(1)

	fmt.Fprintln(os.Stdout, headerStyle.Render("wtfrc coach stats"))
	fmt.Fprintln(os.Stdout)

	conn := d.DB.Conn()

	// Coaching log stats.
	fmt.Fprintln(os.Stdout, sectionStyle.Render("Coaching Log"))

	var totalLog int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM coaching_log`).Scan(&totalLog); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("count coaching_log: %w", err)
	}
	fmt.Fprintf(os.Stdout, "  %s %s\n", labelStyle.Render("Total events:"), valueStyle.Render(strconv.Itoa(totalLog)))

	var adoptedLog int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM coaching_log WHERE was_adopted=1`).Scan(&adoptedLog); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("count adopted: %w", err)
	}
	fmt.Fprintf(os.Stdout, "  %s %s\n", labelStyle.Render("Adopted:"), valueStyle.Render(strconv.Itoa(adoptedLog)))

	if totalLog > 0 {
		adoptRate := float64(adoptedLog) / float64(totalLog) * 100
		fmt.Fprintf(os.Stdout, "  %s %s\n", labelStyle.Render("Adoption rate:"), valueStyle.Render(fmt.Sprintf("%.1f%%", adoptRate)))
	}

	// Graduation stats.
	fmt.Fprintln(os.Stdout, sectionStyle.Render("Graduation"))

	var graduated int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM coaching_state WHERE state='graduated'`).Scan(&graduated); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("count graduated: %w", err)
	}
	fmt.Fprintf(os.Stdout, "  %s %s\n", labelStyle.Render("Graduated actions:"), valueStyle.Render(strconv.Itoa(graduated)))

	// Usage events.
	fmt.Fprintln(os.Stdout, sectionStyle.Render("Usage Events"))

	var usageTotal int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM usage_events`).Scan(&usageTotal); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("count usage_events: %w", err)
	}
	fmt.Fprintf(os.Stdout, "  %s %s\n", labelStyle.Render("Total recorded actions:"), valueStyle.Render(strconv.Itoa(usageTotal)))

	return nil
}

// ---------------------------------------------------------------------------
// coach log
// ---------------------------------------------------------------------------

var coachLogCmd = &cobra.Command{
	Use:   "log",
	Short: "Show recent coaching events",
	RunE:  runCoachLog,
}

func runCoachLog(_ *cobra.Command, _ []string) error {
	d, err := newDeps()
	if err != nil {
		return err
	}
	defer d.DB.Close()

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))
	adoptedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))
	skippedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6B6B"))

	fmt.Fprintln(os.Stdout, headerStyle.Render("wtfrc coach log"))
	fmt.Fprintln(os.Stdout)

	rows, err := d.DB.Conn().Query(
		`SELECT timestamp, source, user_action, optimal_action, was_adopted
		 FROM coaching_log ORDER BY timestamp DESC LIMIT 20`,
	)
	if err != nil {
		return fmt.Errorf("query coaching_log: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var ts, source, userAction, optimalAction string
		var wasAdopted int
		if err := rows.Scan(&ts, &source, &userAction, &optimalAction, &wasAdopted); err != nil {
			return fmt.Errorf("scan coaching_log: %w", err)
		}

		adopted := skippedStyle.Render("✗")
		if wasAdopted == 1 {
			adopted = adoptedStyle.Render("✓")
		}

		// Shorten timestamp to datetime only.
		shortTS := ts
		if len(ts) >= 19 {
			shortTS = ts[:19]
		}

		fmt.Fprintf(os.Stdout, "  %s  %s  %s  %s → %s\n",
			dimStyle.Render(shortTS),
			adopted,
			dimStyle.Render(fmt.Sprintf("[%s]", source)),
			userAction,
			optimalAction,
		)
		count++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("coaching_log rows: %w", err)
	}

	if count == 0 {
		fmt.Fprintln(os.Stdout, dimStyle.Render("  No coaching events recorded yet."))
	}
	return nil
}

// ---------------------------------------------------------------------------
// coach graduated
// ---------------------------------------------------------------------------

var coachGraduatedCmd = &cobra.Command{
	Use:   "graduated",
	Short: "List graduated actions",
	RunE:  runCoachGraduated,
}

func runCoachGraduated(_ *cobra.Command, _ []string) error {
	d, err := newDeps()
	if err != nil {
		return err
	}
	defer d.DB.Close()

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))

	fmt.Fprintln(os.Stdout, headerStyle.Render("wtfrc coach graduated"))
	fmt.Fprintln(os.Stdout)

	rows, err := d.DB.Conn().Query(
		`SELECT action_id, graduated_at, total_coached, total_adopted
		 FROM coaching_state WHERE state='graduated'`,
	)
	if err != nil {
		return fmt.Errorf("query graduated: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var actionID string
		var graduatedAt sql.NullString
		var totalCoached, totalAdopted int
		if err := rows.Scan(&actionID, &graduatedAt, &totalCoached, &totalAdopted); err != nil {
			return fmt.Errorf("scan graduated: %w", err)
		}

		gradAt := "unknown"
		if graduatedAt.Valid && len(graduatedAt.String) >= 10 {
			gradAt = graduatedAt.String[:10]
		}

		fmt.Fprintf(os.Stdout, "  %s  %s  %s\n",
			valueStyle.Render(actionID),
			dimStyle.Render(fmt.Sprintf("graduated: %s", gradAt)),
			dimStyle.Render(fmt.Sprintf("coached: %d  adopted: %d", totalCoached, totalAdopted)),
		)
		count++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("graduated rows: %w", err)
	}

	if count == 0 {
		fmt.Fprintln(os.Stdout, dimStyle.Render("  No graduated actions yet. Keep using optimal actions!"))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers: PID management
// ---------------------------------------------------------------------------

// xdgRuntimeDir returns the wtfrc subdir inside $XDG_RUNTIME_DIR, falling
// back to /tmp/wtfrc-<uid> if the variable is not set.
func xdgRuntimeDir() string {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = fmt.Sprintf("/tmp/wtfrc-%d", os.Getuid())
	}
	return filepath.Join(base, "wtfrc")
}

// coachPIDPath returns the path to the coach PID file.
func coachPIDPath() string {
	return filepath.Join(xdgRuntimeDir(), "coach.pid")
}

// findCoachPID returns the PID of the running coach daemon.
// It first checks the PID file; if that fails it falls back to pgrep.
// Returns 0 if no daemon is found.
func findCoachPID() (int, error) {
	// Try PID file first.
	data, err := os.ReadFile(coachPIDPath())
	if err == nil {
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if parseErr == nil && pid > 0 {
			// Verify the process is actually running.
			proc, _ := os.FindProcess(pid)
			if proc != nil {
				if sigErr := proc.Signal(syscall.Signal(0)); sigErr == nil {
					return pid, nil
				}
			}
		}
	}

	// Fall back to pgrep.
	out, pgrepErr := exec.Command("pgrep", "-f", "wtfrc coach start").Output()
	if pgrepErr != nil {
		return 0, nil
	}
	pidStr := strings.TrimSpace(string(out))
	if pidStr == "" {
		return 0, nil
	}
	// pgrep may return multiple PIDs; use the first.
	lines := strings.Split(pidStr, "\n")
	pid, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return 0, nil
	}
	return pid, nil
}

// ---------------------------------------------------------------------------
// Helpers: shell hook and neovim plugin installation
// ---------------------------------------------------------------------------

// scriptSourcePath returns the absolute path to a file in the scripts/ dir
// relative to the binary location. Falls back to searching common paths.
func scriptSourcePath(name string) (string, error) {
	// Try relative to executable.
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "..", "scripts", name)
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Clean(candidate), nil
		}
	}

	// Try relative to working directory (useful in dev).
	cwd, _ := os.Getwd()
	for _, base := range []string{cwd, filepath.Join(cwd, "..")} {
		candidate := filepath.Join(base, "scripts", name)
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Clean(candidate), nil
		}
	}

	return "", fmt.Errorf("script %s not found", name)
}

// installShellHook copies scripts/wtfrc-coach.zsh to ~/.config/zsh/conf.d/wtfrc-coach.zsh.
func installShellHook() error {
	src, err := scriptSourcePath("wtfrc-coach.zsh")
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read shell hook: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dst := filepath.Join(home, ".config", "zsh", "conf.d", "wtfrc-coach.zsh")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create zsh conf.d: %w", err)
	}
	return os.WriteFile(dst, data, 0o644)
}

// removeShellHook removes ~/.config/zsh/conf.d/wtfrc-coach.zsh.
func removeShellHook() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dst := filepath.Join(home, ".config", "zsh", "conf.d", "wtfrc-coach.zsh")
	_ = os.Remove(dst)
}

// installNeovimPlugin copies scripts/wtfrc-coach.lua to ~/.config/nvim/plugin/wtfrc-coach.lua.
func installNeovimPlugin() error {
	src, err := scriptSourcePath("wtfrc-coach.lua")
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read neovim plugin: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dst := filepath.Join(home, ".config", "nvim", "plugin", "wtfrc-coach.lua")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create nvim plugin dir: %w", err)
	}
	return os.WriteFile(dst, data, 0o644)
}

// removeNeovimPlugin removes ~/.config/nvim/plugin/wtfrc-coach.lua.
func removeNeovimPlugin() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dst := filepath.Join(home, ".config", "nvim", "plugin", "wtfrc-coach.lua")
	_ = os.Remove(dst)
}
