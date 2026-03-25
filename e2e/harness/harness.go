//go:build e2e

// Package harness provides the host-side orchestrator for E2E tests.
// It manages the QEMU VM lifecycle, SSH connections, and test coordination.
package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// VMConfig holds configuration for the QEMU virtual machine.
type VMConfig struct {
	CacheDir string
	SSHPort  int
	SSHKey   string
}

// Harness manages the E2E test environment.
type Harness struct {
	cfg               VMConfig
	qemuPID           int
	isLocal           bool
	cacheDir          string
	originalWorkspace string // saved workspace name to restore on teardown (local mode)
}

// New creates a new Harness. It auto-detects whether to use local Hyprland
// or boot a QEMU VM based on the HYPRLAND_INSTANCE_SIGNATURE env var.
func New() (*Harness, error) {
	e2eDir := findE2EDir()
	cacheDir := filepath.Join(e2eDir, ".cache")

	h := &Harness{
		cfg: VMConfig{
			CacheDir: cacheDir,
			SSHPort:  2222,
			SSHKey:   filepath.Join(cacheDir, "e2e_key"),
		},
		cacheDir: cacheDir,
	}

	// Detect local Hyprland
	if sig := os.Getenv("HYPRLAND_INSTANCE_SIGNATURE"); sig != "" {
		socketPath := filepath.Join(
			os.Getenv("XDG_RUNTIME_DIR"),
			"hypr", sig, ".socket2.sock",
		)
		if _, err := os.Stat(socketPath); err == nil {
			h.isLocal = true
		}
	}

	return h, nil
}

// IsLocal returns true if running directly on a Hyprland desktop.
func (h *Harness) IsLocal() bool {
	return h.isLocal
}

// Setup prepares the test environment. In VM mode, it boots the VM and
// waits for SSH readiness. In local mode, it verifies the Hyprland socket.
func (h *Harness) Setup(ctx context.Context) error {
	if h.isLocal {
		return h.setupLocal(ctx)
	}
	return h.setupVM(ctx)
}

// Teardown cleans up the test environment.
func (h *Harness) Teardown() error {
	if h.isLocal {
		return h.teardownLocal()
	}
	return h.stopVM()
}

// RunOnGuest executes a command on the guest (VM via SSH, or local shell).
// Returns stdout, stderr, and any error.
func (h *Harness) RunOnGuest(ctx context.Context, command string) (string, string, error) {
	if h.isLocal {
		return h.runLocal(ctx, command)
	}
	return h.runSSH(ctx, command)
}

// CopyToGuest copies a file from the host to the guest.
func (h *Harness) CopyToGuest(ctx context.Context, src, dst string) error {
	if h.isLocal {
		// In local mode, just copy the file directly.
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("read %s: %w", src, err)
		}
		return os.WriteFile(dst, data, 0755)
	}
	return h.scpToGuest(ctx, src, dst)
}

// WaitForCondition polls a command on the guest until it returns the expected
// output or the timeout is reached.
func (h *Harness) WaitForCondition(ctx context.Context, command, expected string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastOutput string
	for time.Now().Before(deadline) {
		stdout, _, err := h.RunOnGuest(ctx, command)
		if err == nil && strings.Contains(stdout, expected) {
			return nil
		}
		lastOutput = stdout
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("condition not met within %v: command=%q expected=%q last_output=%q",
		timeout, command, expected, lastOutput)
}

// setupLocal verifies the local Hyprland environment is usable for testing
// and creates a dedicated workspace to avoid interfering with the developer's session.
func (h *Harness) setupLocal(_ context.Context) error {
	// Verify hyprctl is available
	if _, err := exec.LookPath("hyprctl"); err != nil {
		return fmt.Errorf("hyprctl not found: %w", err)
	}

	// Query the current active workspace so we can restore it on teardown.
	out, err := exec.Command("hyprctl", "-j", "activeworkspace").Output()
	if err != nil {
		return fmt.Errorf("query active workspace: %w", err)
	}
	var ws struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &ws); err != nil {
		return fmt.Errorf("parse active workspace: %w", err)
	}
	h.originalWorkspace = ws.Name

	// Create and switch to a dedicated test workspace.
	if err := exec.Command("hyprctl", "dispatch", "workspace", "name:wtfrc-test").Run(); err != nil {
		return fmt.Errorf("create test workspace: %w", err)
	}

	return nil
}

// teardownLocal switches back to the original workspace and closes the test workspace.
func (h *Harness) teardownLocal() error {
	var errs []string

	// Switch back to the original workspace.
	if h.originalWorkspace != "" {
		if err := exec.Command("hyprctl", "dispatch", "workspace", "name:"+h.originalWorkspace).Run(); err != nil {
			errs = append(errs, fmt.Sprintf("restore workspace %q: %v", h.originalWorkspace, err))
		}
	}

	// Close any windows left in the test workspace, then remove it.
	// Moving to a different workspace first (done above) means clients on wtfrc-test
	// can be closed without focus issues. We use focusworkspaceoncurrentmonitor to avoid
	// interfering if the user has multiple monitors.
	// hyprctl does not have a "close workspace" command, but an empty workspace is
	// automatically destroyed by Hyprland when it has no clients. We just need to
	// make sure any straggler windows are closed.
	_ = exec.Command("bash", "-c",
		`hyprctl -j clients | `+
			`jq -r '.[] | select(.workspace.name=="wtfrc-test") | .address' | `+
			`while read addr; do hyprctl dispatch closewindow address:"$addr"; done`,
	).Run()

	if len(errs) > 0 {
		return fmt.Errorf("teardown local: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ErrSkipNoImage is returned when the VM image is not available.
// Callers should treat this as a skip condition, not a failure.
var ErrSkipNoImage = fmt.Errorf("VM image not available (run 'make e2e-image' first)")

// setupVM boots the QEMU VM and waits for SSH.
func (h *Harness) setupVM(ctx context.Context) error {
	// Check that the VM image and SSH key exist before trying to boot
	snapshot := filepath.Join(h.cacheDir, "arch-e2e.qcow2")
	if _, err := os.Stat(snapshot); os.IsNotExist(err) {
		return ErrSkipNoImage
	}
	sshKey := filepath.Join(h.cacheDir, "e2e_key")
	if _, err := os.Stat(sshKey); os.IsNotExist(err) {
		return ErrSkipNoImage
	}

	scriptPath := filepath.Join(findE2EDir(), "scripts", "boot-vm.sh")

	cmd := exec.CommandContext(ctx, "bash", scriptPath, h.cacheDir)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("boot VM: %w", err)
	}

	pid := strings.TrimSpace(string(out))
	if pid == "" {
		return fmt.Errorf("boot-vm.sh returned empty PID")
	}

	fmt.Sscanf(pid, "%d", &h.qemuPID)

	// Deploy wtfrc binaries to the guest
	if err := h.deployBinaries(ctx); err != nil {
		return fmt.Errorf("deploy binaries: %w", err)
	}

	return nil
}

// deployBinaries copies the wtfrc binaries from the host bin/ directory
// to /usr/local/bin/ on the guest VM via SCP + sudo mv.
func (h *Harness) deployBinaries(ctx context.Context) error {
	// Find the project root (where bin/ lives)
	projectRoot := filepath.Dir(findE2EDir())
	binaries := []string{"wtfrc", "wtfrc-monitor", "wtfrc-agent"}

	for _, bin := range binaries {
		src := filepath.Join(projectRoot, "bin", bin)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			return fmt.Errorf("binary not found: %s (run 'make build' first)", src)
		}

		// SCP to /tmp first (test user can write there)
		tmpDst := fmt.Sprintf("/tmp/%s", bin)
		if err := h.scpToGuest(ctx, src, tmpDst); err != nil {
			return fmt.Errorf("scp %s: %w", bin, err)
		}

		// Move to /usr/local/bin with sudo
		_, stderr, err := h.runSSH(ctx, fmt.Sprintf(
			"sudo mv %s /usr/local/bin/%s && sudo chmod +x /usr/local/bin/%s",
			tmpDst, bin, bin,
		))
		if err != nil {
			return fmt.Errorf("install %s: %s: %w", bin, stderr, err)
		}
	}

	return nil
}

// stopVM shuts down the QEMU VM.
func (h *Harness) stopVM() error {
	scriptPath := filepath.Join(findE2EDir(), "scripts", "stop-vm.sh")
	cmd := exec.Command("bash", scriptPath, h.cacheDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runSSH executes a command on the VM via SSH.
func (h *Harness) runSSH(ctx context.Context, command string) (string, string, error) {
	args := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=5",
		"-i", h.cfg.SSHKey,
		"-p", fmt.Sprintf("%d", h.cfg.SSHPort),
		"test@localhost",
		command,
	}

	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// scpToGuest copies a file to the VM via SCP.
func (h *Harness) scpToGuest(ctx context.Context, src, dst string) error {
	args := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-i", h.cfg.SSHKey,
		"-P", fmt.Sprintf("%d", h.cfg.SSHPort),
		src,
		fmt.Sprintf("test@localhost:%s", dst),
	}

	cmd := exec.CommandContext(ctx, "scp", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runLocal executes a command on the local machine with XDG_RUNTIME_DIR set.
func (h *Harness) runLocal(ctx context.Context, command string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)

	// Inherit the current environment and ensure XDG_RUNTIME_DIR is set.
	// Some test commands reference $XDG_RUNTIME_DIR and it may not be
	// propagated through the exec environment by default.
	cmd.Env = os.Environ()
	xdgDir := os.Getenv("XDG_RUNTIME_DIR")
	if xdgDir == "" {
		// Fallback to the conventional path.
		xdgDir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	cmd.Env = append(cmd.Env, "XDG_RUNTIME_DIR="+xdgDir)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// findE2EDir locates the e2e/ directory relative to the project root.
func findE2EDir() string {
	// Walk up from the current working directory looking for go.mod
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "e2e")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Fallback: assume we're in the project root
			return "e2e"
		}
		dir = parent
	}
}
