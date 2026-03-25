package sources

import (
	"bufio"
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/shaiknoorullah/wtfrc/internal/coach"
)

// parseTmuxEvent parses a single line from `tmux -C attach` stdout.
// tmux control-mode notifications start with '%'. We capture:
//   - %window-pane-changed @win %pane → action "pane-changed", context "@win %pane"
//   - %session-window-changed $session @win → action "window-changed", context "$session @win"
//
// Returns (event, true) for coaching-relevant events, (zero, false) otherwise.
func parseTmuxEvent(line string) (coach.Event, bool) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" || !strings.HasPrefix(line, "%") {
		return coach.Event{}, false
	}

	// Format: %<notification-type> <data...>
	parts := strings.SplitN(line[1:], " ", 2) // strip leading '%'
	notifType := parts[0]
	data := ""
	if len(parts) == 2 {
		data = parts[1]
	}

	switch notifType {
	case "window-pane-changed":
		return coach.Event{
			Source:    coach.SourceTmux,
			Action:    "pane-changed",
			Context:   data,
			Timestamp: time.Now(),
		}, true
	case "session-window-changed":
		return coach.Event{
			Source:    coach.SourceTmux,
			Action:    "window-changed",
			Context:   data,
			Timestamp: time.Now(),
		}, true
	default:
		return coach.Event{}, false
	}
}

// RunTmuxOptional spawns `tmux -C attach` and reads its stdout for control-mode
// notifications, forwarding coaching-relevant events to the events channel.
// It runs until ctx is cancelled.
//
// If the tmux binary is not found, it logs a warning and returns immediately.
// If the subprocess exits, it retries with exponential backoff.
func RunTmuxOptional(ctx context.Context, events chan<- coach.Event) {
	if _, err := exec.LookPath("tmux"); err != nil {
		log.Warn("tmux: binary not found, source disabled")
		return
	}

	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}

		cmd := exec.CommandContext(ctx, "tmux", "-C", "attach")
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			log.Warnf("tmux: failed to create stdout pipe: %v (retrying in %s)", err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		if err := cmd.Start(); err != nil {
			log.Warnf("tmux: failed to start: %v (retrying in %s)", err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		// Reset backoff on successful start.
		backoff = time.Second
		log.Debug("tmux: subprocess started")

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if ctx.Err() != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return
			}
			line := scanner.Text()
			if ev, ok := parseTmuxEvent(line); ok {
				select {
				case events <- ev:
				case <-ctx.Done():
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					return
				}
			}
		}

		_ = cmd.Wait()

		if ctx.Err() != nil {
			return
		}

		log.Warnf("tmux: subprocess exited, retrying in %s", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}
