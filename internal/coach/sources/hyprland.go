// Package sources provides optional event source goroutines for tool integrations.
// Each source is individually failable: if the tool is not running or not present,
// the source logs a warning and either returns immediately or retries with backoff.
package sources

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/shaiknoorullah/wtfrc/internal/coach"
)

// hyprlandCaptured is the set of Hyprland event types we care about for coaching.
var hyprlandCaptured = map[string]bool{
	"activewindow": true,
	"openwindow":   true,
	"movewindow":   true,
	"fullscreen":   true,
}

// parseHyprlandEvent parses a single line from Hyprland socket2.
// Lines have the format: EVENTTYPE>>DATA
// Returns (event, true) if the event type is in our capture list,
// (zero, false) otherwise.
func parseHyprlandEvent(line string) (coach.Event, bool) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return coach.Event{}, false
	}

	idx := strings.Index(line, ">>")
	if idx < 0 {
		return coach.Event{}, false
	}

	eventType := line[:idx]
	data := line[idx+2:]

	if !hyprlandCaptured[eventType] {
		return coach.Event{}, false
	}

	return coach.Event{
		Source:    coach.SourceHyprland,
		Action:    eventType,
		Context:   data,
		Timestamp: time.Now(),
	}, true
}

// RunHyprlandOptional subscribes to Hyprland socket2 and forwards captured events
// to the shared events channel. It runs until ctx is cancelled.
//
// If HYPRLAND_INSTANCE_SIGNATURE is not set, it logs a message and returns immediately.
// On connection failure it backs off and retries rather than propagating errors.
func RunHyprlandOptional(ctx context.Context, events chan<- coach.Event) {
	sig := os.Getenv("HYPRLAND_INSTANCE_SIGNATURE")
	if sig == "" {
		log.Info("Hyprland not detected, source disabled")
		return
	}

	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	socketPath := fmt.Sprintf("%s/hypr/%s/.socket2.sock", runtimeDir, sig)

	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}

		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			log.Warnf("hyprland: cannot connect to %s: %v (retrying in %s)", socketPath, err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		// Reset backoff on successful connection.
		backoff = time.Second
		log.Debugf("hyprland: connected to %s", socketPath)

		scanner := bufio.NewScanner(conn)
		readErr := false
		for scanner.Scan() {
			if ctx.Err() != nil {
				_ = conn.Close()
				return
			}
			line := scanner.Text()
			if ev, ok := parseHyprlandEvent(line); ok {
				select {
				case events <- ev:
				case <-ctx.Done():
					_ = conn.Close()
					return
				}
			}
		}

		_ = conn.Close()
		if ctx.Err() != nil {
			return
		}

		if err := scanner.Err(); err != nil || readErr {
			log.Warnf("hyprland: connection lost (%v), retrying in %s", err, backoff)
		} else {
			log.Warnf("hyprland: connection closed, retrying in %s", backoff)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

