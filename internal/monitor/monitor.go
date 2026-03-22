package monitor

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sync"
)

// inputEvent mirrors the kernel's struct input_event (linux/input.h).
// On 64-bit Linux the layout is:
//
//	struct timeval { long tv_sec; long tv_usec; }  — 16 bytes
//	__u16 type; __u16 code; __s32 value;           —  8 bytes
//
// Total: 24 bytes.
const inputEventSize = 24

// Monitor reads raw evdev events from keyboard and mouse devices, classifies
// them, and writes coaching-relevant lines to a FIFO.
type Monitor struct {
	fifoPath       string
	emitShiftChars bool
	classifier     *Classifier
}

// NewMonitor creates a Monitor that writes to fifoPath.
func NewMonitor(fifoPath string, emitShiftChars bool) *Monitor {
	return &Monitor{
		fifoPath:       fifoPath,
		emitShiftChars: emitShiftChars,
		classifier:     NewClassifier(emitShiftChars),
	}
}

// Run starts the monitor loop. It blocks until ctx is cancelled.
//
// Steps:
//  1. Verify the user is in the "input" group.
//  2. Discover keyboard and mouse device paths.
//  3. Open (or create) the FIFO for writing.
//  4. Spawn a reader goroutine per device.
//  5. Fan classified events into the FIFO writer.
//  6. Return when ctx is cancelled or a fatal error occurs.
func (m *Monitor) Run(ctx context.Context) error {
	if err := CheckInputGroup(); err != nil {
		return err
	}

	kbPath, err := FindKeyboard()
	if err != nil {
		return fmt.Errorf("monitor: %w", err)
	}

	mousePath, err := FindMouse()
	if err != nil {
		// Mouse is optional — warn but continue.
		mousePath = ""
	}

	// Open FIFO (or regular file in tests) for writing.
	// O_WRONLY|O_CREATE covers both named pipes and regular files.
	out, err := os.OpenFile(m.fifoPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("monitor: open fifo %s: %w", m.fifoPath, err)
	}
	defer out.Close()

	// Channel for classified events from all device readers.
	eventCh := make(chan *ClassifiedEvent, 64)

	var wg sync.WaitGroup

	// Spawn readers.
	paths := []string{kbPath}
	if mousePath != "" {
		paths = append(paths, mousePath)
	}

	for _, p := range paths {
		wg.Add(1)
		go func(devPath string) {
			defer wg.Done()
			m.readDevice(ctx, devPath, eventCh)
		}(p)
	}

	// Close channel when all readers exit.
	go func() {
		wg.Wait()
		close(eventCh)
	}()

	// Write loop.
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-eventCh:
			if !ok {
				return nil
			}
			if ev != nil {
				if werr := m.writeEvent(out, ev); werr != nil {
					return fmt.Errorf("monitor: write event: %w", werr)
				}
			}
		}
	}
}

// readDevice reads raw input_event structs from devPath, classifies them, and
// sends non-nil ClassifiedEvents to eventCh.  Exits when ctx is cancelled or
// the device read fails.
func (m *Monitor) readDevice(ctx context.Context, devPath string, eventCh chan<- *ClassifiedEvent) {
	f, err := os.Open(devPath)
	if err != nil {
		// Device not accessible — silently exit this goroutine.
		return
	}
	defer f.Close()

	// Use a local classifier per device so goroutines don't share state.
	clf := NewClassifier(m.emitShiftChars)

	buf := make([]byte, inputEventSize)
	for {
		// Check cancellation before each blocking read.
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, err := io.ReadFull(f, buf)
		if err != nil {
			return
		}

		// Parse input_event:
		//   bytes 0–15:  struct timeval (ignored)
		//   bytes 16–17: type  (uint16, LE)
		//   bytes 18–19: code  (uint16, LE)
		//   bytes 20–23: value (int32,  LE)
		evType := binary.LittleEndian.Uint16(buf[16:18])
		evCode := binary.LittleEndian.Uint16(buf[18:20])
		evValue := int32(binary.LittleEndian.Uint32(buf[20:24]))

		ev := clf.Classify(evType, evCode, evValue)
		if ev != nil {
			select {
			case eventCh <- ev:
			case <-ctx.Done():
				return
			}
		}
	}
}

// writeEvent formats a ClassifiedEvent as a tab-delimited line and writes it
// to the supplied writer.
//
// Format: evdev\t{combo}\t{type}\n
func (m *Monitor) writeEvent(w io.Writer, ev *ClassifiedEvent) error {
	line := fmt.Sprintf("evdev\t%s\t%s\n", ev.Combo, ev.Type)
	_, err := io.WriteString(w, line)
	return err
}
