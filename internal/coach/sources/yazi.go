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

// yaziCaptured is the set of yazi DDS event types we care about for coaching.
var yaziCaptured = map[string]bool{
	"cd":    true,
	"hover": true,
}

// parseYaziEvent parses a single line from `ya sub cd,hover` stdout.
// The DDS output format is: <eventType> <data...>
// where <data> is the remainder of the line after the first space (may be empty).
// Returns (event, true) for captured event types, (zero, false) otherwise.
func parseYaziEvent(line string) (coach.Event, bool) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return coach.Event{}, false
	}

	parts := strings.SplitN(line, " ", 2)
	eventType := parts[0]

	if !yaziCaptured[eventType] {
		return coach.Event{}, false
	}

	data := ""
	if len(parts) == 2 {
		data = parts[1]
	}

	return coach.Event{
		Source:    coach.SourceYazi,
		Action:    eventType,
		Context:   data,
		Timestamp: time.Now(),
	}, true
}

// RunYaziOptional spawns `ya sub cd,hover` and reads its stdout for DDS events,
// forwarding coaching-relevant events to the events channel.
// It runs until ctx is cancelled.
//
// If the ya binary is not found, it logs a warning and returns immediately.
// If the subprocess exits, it retries with exponential backoff.
func RunYaziOptional(ctx context.Context, events chan<- coach.Event) {
	if _, err := exec.LookPath("ya"); err != nil {
		log.Warn("yazi: ya binary not found, source disabled")
		return
	}

	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}

		cmd := exec.CommandContext(ctx, "ya", "sub", "cd,hover")
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			log.Warnf("yazi: failed to create stdout pipe: %v (retrying in %s)", err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		if err := cmd.Start(); err != nil {
			log.Warnf("yazi: failed to start ya: %v (retrying in %s)", err, backoff)
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
		log.Debug("yazi: ya subprocess started")

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if ctx.Err() != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return
			}
			line := scanner.Text()
			if ev, ok := parseYaziEvent(line); ok {
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

		log.Warnf("yazi: ya subprocess exited, retrying in %s", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}
