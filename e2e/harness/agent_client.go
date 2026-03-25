//go:build e2e

package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// AgentClient communicates with the guest-side wtfrc-agent binary
// over its JSON-over-stdin/stdout protocol via SSH (VM mode) or
// direct invocation (local mode).
type AgentClient struct {
	harness *Harness
}

// AgentRequest is the JSON payload sent to the agent binary.
type AgentRequest struct {
	Action string         `json:"action"`
	Params map[string]any `json:"params"`
}

// AgentResponse is the JSON payload returned by the agent binary.
type AgentResponse struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// NewAgentClient creates an AgentClient bound to the given Harness.
func NewAgentClient(h *Harness) *AgentClient {
	return &AgentClient{harness: h}
}

// Call sends a JSON request to the guest-side agent and reads the JSON response.
// In VM mode the agent is invoked via SSH; in local mode it is executed directly.
func (ac *AgentClient) Call(ctx context.Context, action string, params map[string]any) (*AgentResponse, error) {
	req := AgentRequest{
		Action: action,
		Params: params,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal agent request: %w", err)
	}

	var cmd *exec.Cmd
	if ac.harness.isLocal {
		cmd = exec.CommandContext(ctx, "wtfrc-agent")
	} else {
		args := ac.harness.sshArgs("wtfrc-agent")
		cmd = exec.CommandContext(ctx, "ssh", args...)
	}

	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("agent call %q: %w\nstderr: %s", action, err, stderr.String())
	}

	var resp AgentResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("unmarshal agent response for %q: %w\nraw: %s", action, err, stdout.String())
	}

	if !resp.OK {
		return &resp, fmt.Errorf("agent error for %q: %s", action, resp.Error)
	}
	return &resp, nil
}

// --- Convenience methods ---

// TypeText asks the agent to type text via uinput keyboard simulation.
func (ac *AgentClient) TypeText(ctx context.Context, text string) (*AgentResponse, error) {
	return ac.Call(ctx, "type_text", map[string]any{
		"text": text,
	})
}

// PressCombo asks the agent to press a key combination (e.g. "Super+Return").
func (ac *AgentClient) PressCombo(ctx context.Context, combo string) (*AgentResponse, error) {
	return ac.Call(ctx, "press_combo", map[string]any{
		"combo": combo,
	})
}

// MouseClick asks the agent to move the virtual mouse to (x, y) and click.
func (ac *AgentClient) MouseClick(ctx context.Context, x, y int) (*AgentResponse, error) {
	return ac.Call(ctx, "mouse_click", map[string]any{
		"x": x,
		"y": y,
	})
}

// QueryDB asks the agent to execute a read-only SQL query against the wtfrc database.
func (ac *AgentClient) QueryDB(ctx context.Context, sql string) (*AgentResponse, error) {
	return ac.Call(ctx, "query_db", map[string]any{
		"sql": sql,
	})
}

// WriteFIFO asks the agent to write a message to the coach FIFO.
func (ac *AgentClient) WriteFIFO(ctx context.Context, msg string) (*AgentResponse, error) {
	return ac.Call(ctx, "write_fifo", map[string]any{
		"message": msg,
	})
}

// ReadFile asks the agent to read and return the contents of a file on the guest.
func (ac *AgentClient) ReadFile(ctx context.Context, path string) (*AgentResponse, error) {
	return ac.Call(ctx, "read_file", map[string]any{
		"path": path,
	})
}

// WaitNotification asks the agent to wait for a D-Bus notification whose body
// contains the given substring, up to the specified timeout.
func (ac *AgentClient) WaitNotification(ctx context.Context, contains string, timeout time.Duration) (*AgentResponse, error) {
	return ac.Call(ctx, "wait_notification", map[string]any{
		"contains":   contains,
		"timeout_ms": timeout.Milliseconds(),
	})
}

// Hyprctl asks the agent to run hyprctl with the given arguments and return the output.
func (ac *AgentClient) Hyprctl(ctx context.Context, args ...string) (*AgentResponse, error) {
	return ac.Call(ctx, "hyprctl", map[string]any{
		"args": args,
	})
}

// StartDBusCapture asks the agent to begin capturing D-Bus notifications.
func (ac *AgentClient) StartDBusCapture(ctx context.Context) (*AgentResponse, error) {
	return ac.Call(ctx, "start_dbus_capture", nil)
}

// GetNotifications asks the agent to return all captured D-Bus notifications
// since the last StartDBusCapture call.
func (ac *AgentClient) GetNotifications(ctx context.Context) (*AgentResponse, error) {
	return ac.Call(ctx, "get_notifications", nil)
}

// --- Helper on Harness to build SSH args ---

// sshArgs returns the SSH command-line arguments for connecting to the guest,
// with the given remote command appended.
func (h *Harness) sshArgs(remoteCmd string) []string {
	return []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=5",
		"-o", "LogLevel=ERROR",
		"-i", h.cfg.SSHKey,
		"-p", fmt.Sprintf("%d", h.cfg.SSHPort),
		"test@localhost",
		remoteCmd,
	}
}

// Agent returns an AgentClient for this harness. It can be called multiple
// times; each call returns a new client (they are stateless on the host side).
func (h *Harness) Agent() *AgentClient {
	return NewAgentClient(h)
}

// sshBaseArgs returns the common SSH options without a remote command.
// Useful for building custom SSH invocations (e.g. with stdin piping).
func (h *Harness) sshBaseArgs() []string {
	return []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=5",
		"-o", "LogLevel=ERROR",
		"-i", h.cfg.SSHKey,
		"-p", fmt.Sprintf("%d", h.cfg.SSHPort),
		"test@localhost",
	}
}

// refactored runSSH to use the shared sshArgs helper.
func (h *Harness) runSSHViaAgent(ctx context.Context, command string) (string, string, error) {
	args := h.sshArgs(command)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}
