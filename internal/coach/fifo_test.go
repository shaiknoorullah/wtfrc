package coach

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestFIFOReaderBasic verifies that valid events written to the FIFO appear on the channel.
func TestFIFOReaderBasic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.fifo")

	r := NewFIFOReader(path)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events := make(chan Event, 10)

	// Start Run in a goroutine.
	runErr := make(chan error, 1)
	go func() {
		runErr <- r.Run(ctx, events)
	}()

	// Wait for the FIFO to be created before opening it for writing.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for FIFO to be created")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Open FIFO for writing.
	wf, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("failed to open FIFO for writing: %v", err)
	}
	defer wf.Close()

	// Write valid events.
	lines := []string{
		"shell\tls -la\t/home/user\n",
		"nvim\t:w\n",
		"hyprland\tkb:hyprland:movefocus l\tworkspace1\n",
	}
	for _, line := range lines {
		if _, err := wf.WriteString(line); err != nil {
			t.Fatalf("write error: %v", err)
		}
	}

	// Collect 3 events.
	collected := make([]Event, 0, 3)
	timeout := time.After(3 * time.Second)
	for len(collected) < 3 {
		select {
		case e := <-events:
			collected = append(collected, e)
		case <-timeout:
			t.Fatalf("timed out waiting for events; got %d of 3", len(collected))
		}
	}

	// Verify parsing.
	if collected[0].Source != "shell" || collected[0].Action != "ls -la" || collected[0].Context != "/home/user" {
		t.Errorf("event 0 mismatch: %+v", collected[0])
	}
	if collected[1].Source != "nvim" || collected[1].Action != ":w" {
		t.Errorf("event 1 mismatch: %+v", collected[1])
	}
	if collected[2].Source != "hyprland" || collected[2].Action != "hyprland:movefocus l" || !collected[2].IsKeybind {
		t.Errorf("event 2 mismatch: %+v", collected[2])
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Errorf("Run returned non-nil error on cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Run did not return after context cancellation")
	}
}

// TestFIFOReaderMalformedSkipped verifies malformed lines are skipped and the next valid event arrives.
func TestFIFOReaderMalformedSkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.fifo")

	r := NewFIFOReader(path)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events := make(chan Event, 10)
	runErr := make(chan error, 1)
	go func() {
		runErr <- r.Run(ctx, events)
	}()

	// Wait for FIFO to be created.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for FIFO to be created")
		}
		time.Sleep(10 * time.Millisecond)
	}

	wf, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("failed to open FIFO for writing: %v", err)
	}
	defer wf.Close()

	// Write malformed lines then a valid one.
	lines := []string{
		"\n",         // empty
		"shell\n",    // single field
		"\t\n",       // empty source
		"shell\t\n",  // empty action
		"shell\tgit status\t/repo\n", // valid
	}
	for _, line := range lines {
		if _, err := wf.WriteString(line); err != nil {
			t.Fatalf("write error: %v", err)
		}
	}

	// Expect exactly one event.
	select {
	case e := <-events:
		if e.Source != "shell" || e.Action != "git status" || e.Context != "/repo" {
			t.Errorf("unexpected event: %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for valid event after malformed lines")
	}

	// Verify no additional events arrived.
	select {
	case extra := <-events:
		t.Errorf("unexpected extra event: %+v", extra)
	case <-time.After(100 * time.Millisecond):
		// Good — no extra events.
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Errorf("Run returned non-nil error on cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Run did not return after context cancellation")
	}
}

// TestFIFOReaderContextCancellation verifies Run returns nil when the context is cancelled.
func TestFIFOReaderContextCancellation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.fifo")

	r := NewFIFOReader(path)

	ctx, cancel := context.WithCancel(context.Background())

	events := make(chan Event, 10)
	runErr := make(chan error, 1)
	go func() {
		runErr <- r.Run(ctx, events)
	}()

	// Wait for FIFO to be created then cancel.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for FIFO to be created")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Open a writer so the reader's OpenFile doesn't block indefinitely (O_RDWR
	// means we don't need this, but be safe for the cancellation path).
	cancel()

	select {
	case err := <-runErr:
		if err != nil {
			t.Errorf("Run returned non-nil error on cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("Run did not return after context cancellation")
	}
}

// TestFIFOReaderCleanup verifies Cleanup removes the FIFO file.
func TestFIFOReaderCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.fifo")

	r := NewFIFOReader(path)

	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan Event, 1)
	runErr := make(chan error, 1)
	go func() {
		runErr <- r.Run(ctx, events)
	}()

	// Wait for FIFO creation.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for FIFO to be created")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case <-runErr:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}

	if err := r.Cleanup(); err != nil {
		t.Fatalf("Cleanup error: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected FIFO to be removed, but os.Stat returned: %v", err)
	}
}

// TestFIFOReaderExistingFIFO verifies that Run reuses a pre-existing FIFO without error.
func TestFIFOReaderExistingFIFO(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.fifo")

	// Create the FIFO manually first.
	if err := syscall.Mkfifo(path, 0660); err != nil {
		t.Fatalf("failed to pre-create FIFO: %v", err)
	}

	r := NewFIFOReader(path)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan Event, 10)
	runErr := make(chan error, 1)
	go func() {
		runErr <- r.Run(ctx, events)
	}()

	// Wait for the reader to open the FIFO (it should reuse the existing one).
	// Give it a moment to get past the stat/mkfifo/open phase.
	time.Sleep(100 * time.Millisecond)

	// Write a valid event.
	deadline := time.Now().Add(2 * time.Second)
	var wf *os.File
	var err error
	for {
		wf, err = os.OpenFile(path, os.O_WRONLY, 0)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out opening FIFO for writing: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer wf.Close()

	if _, err := wf.WriteString("shell\techo hello\t/tmp\n"); err != nil {
		t.Fatalf("write error: %v", err)
	}

	select {
	case e := <-events:
		if e.Source != "shell" || e.Action != "echo hello" {
			t.Errorf("unexpected event: %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event from reused FIFO")
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Errorf("Run returned non-nil error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Run did not return after cancellation")
	}
}
