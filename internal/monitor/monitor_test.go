package monitor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMonitorFIFOWrite verifies that a Monitor writes correctly formatted lines
// to a FIFO when fed a pre-classified event via its internal write path.
// We use a regular temp file here because os.MkdirFifo requires elevated
// permissions in some CI environments. The write path is the same.
func TestMonitorFIFOWrite(t *testing.T) {
	tmpDir := t.TempDir()
	fifoPath := filepath.Join(tmpDir, "test.fifo")

	// Create a regular file to simulate the FIFO destination.
	f, err := os.Create(fifoPath)
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	f.Close()

	m := NewMonitor(fifoPath, false)

	// Open the file for writing as the monitor would.
	out, err := os.OpenFile(fifoPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatalf("open output file: %v", err)
	}

	ev := &ClassifiedEvent{Type: "key", Combo: "$mod+j"}
	if err := m.writeEvent(out, ev); err != nil {
		t.Fatalf("writeEvent: %v", err)
	}
	out.Close()

	data, err := os.ReadFile(fifoPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	line := string(data)
	// Expected format: "evdev\t$mod+j\tkey\n"
	if !strings.HasPrefix(line, "evdev\t") {
		t.Errorf("line does not start with evdev\\t: %q", line)
	}
	if !strings.Contains(line, "$mod+j") {
		t.Errorf("line does not contain combo: %q", line)
	}
	if !strings.HasSuffix(line, "\n") {
		t.Errorf("line does not end with newline: %q", line)
	}
}

// TestMonitorRunCancels verifies that Monitor.Run returns when its context is
// cancelled. We use a short timeout so the test doesn't hang.
func TestMonitorRunCancels(t *testing.T) {
	if os.Getenv("WTFRC_TEST_EVDEV") == "" {
		t.Skip("skipping hardware evdev test (set WTFRC_TEST_EVDEV=1 to enable)")
	}

	tmpDir := t.TempDir()
	fifoPath := filepath.Join(tmpDir, "test.fifo")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	m := NewMonitor(fifoPath, false)
	err := m.Run(ctx)
	// We expect either nil (clean cancel) or a context error.
	if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
		t.Errorf("unexpected error: %v", err)
	}
}
