package coach

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"
)

// FIFOReader creates and reads events from a named pipe (FIFO).
type FIFOReader struct {
	path string
}

// NewFIFOReader returns a FIFOReader that will use the given path.
func NewFIFOReader(path string) *FIFOReader {
	return &FIFOReader{path: path}
}

// Run creates the FIFO if necessary, opens it, and streams parsed Events onto
// the events channel until the context is cancelled. It returns nil when the
// context is done, or a non-nil error for setup/IO failures.
func (r *FIFOReader) Run(ctx context.Context, events chan<- Event) error {
	// 1. Ensure the parent directory exists.
	if err := os.MkdirAll(filepath.Dir(r.path), 0755); err != nil {
		return fmt.Errorf("fifo: mkdir %s: %w", filepath.Dir(r.path), err)
	}

	// 2. Create or reuse the FIFO.
	if fi, err := os.Stat(r.path); err == nil {
		// Path already exists — verify it is a FIFO.
		if fi.Mode()&os.ModeNamedPipe == 0 {
			return fmt.Errorf("fifo: %s exists but is not a named pipe", r.path)
		}
		// Reuse existing FIFO.
	} else if os.IsNotExist(err) {
		if mkErr := syscall.Mkfifo(r.path, 0660); mkErr != nil {
			return fmt.Errorf("fifo: mkfifo %s: %w", r.path, mkErr)
		}
	} else {
		return fmt.Errorf("fifo: stat %s: %w", r.path, err)
	}

	// 3. Open the FIFO with O_RDWR to avoid blocking and to prevent EOF when
	//    no writer is attached (standard Unix FIFO idiom).
	f, err := os.OpenFile(r.path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("fifo: open %s: %w", r.path, err)
	}
	defer f.Close()

	// 4. Read lines with a bufio.Scanner.
	scanner := bufio.NewScanner(f)

	// Use a goroutine to push scan results into a channel so we can also
	// select on ctx.Done().
	type scanResult struct {
		text string
		ok   bool
	}
	scanCh := make(chan scanResult, 1)

	go func() {
		for scanner.Scan() {
			select {
			case scanCh <- scanResult{text: scanner.Text(), ok: true}:
			case <-ctx.Done():
				return
			}
		}
		// Scanner stopped (EOF or error) — signal the main loop.
		scanCh <- scanResult{ok: false}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case res := <-scanCh:
			if !res.ok {
				// EOF or scanner error; wait for context.
				select {
				case <-ctx.Done():
					return nil
				}
			}
			// 5. Parse line; skip malformed input.
			e, err := ParseEvent(res.text)
			if err != nil {
				log.Printf("fifo: malformed event skipped: %v (line: %q)", err, res.text)
				continue
			}
			// 6. Send event, respecting cancellation.
			select {
			case events <- e:
			case <-ctx.Done():
				return nil
			}
		}
	}
}

// Cleanup removes the FIFO file from the filesystem.
func (r *FIFOReader) Cleanup() error {
	return os.Remove(r.path)
}
