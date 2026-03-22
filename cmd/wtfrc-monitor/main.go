// wtfrc-monitor is a small daemon that reads raw evdev keyboard/mouse events,
// classifies them into coaching-relevant combos, and writes formatted lines to
// a FIFO so that the wtfrc coaching daemon can consume them.
//
// Usage:
//
//	wtfrc-monitor [--fifo PATH] [--emit-shift-chars]
//
// Environment:
//
//	WTFRC_FIFO  — FIFO path (overridden by --fifo flag)
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/shaiknoorullah/wtfrc/internal/monitor"
)

func main() {
	os.Exit(run())
}

func run() int {
	fifo := flag.String("fifo", "", "path to the FIFO (defaults to $WTFRC_FIFO)")
	emitShiftChars := flag.Bool("emit-shift-chars", false,
		"emit Shift+<key> combos (disabled by default for privacy)")
	flag.Parse()

	// Resolve FIFO path: flag takes precedence over env.
	fifoPath := *fifo
	if fifoPath == "" {
		fifoPath = os.Getenv("WTFRC_FIFO")
	}
	if fifoPath == "" {
		fmt.Fprintln(os.Stderr, "wtfrc-monitor: FIFO path is required (--fifo or $WTFRC_FIFO)")
		return 1
	}

	// Context cancelled on SIGINT or SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	m := monitor.NewMonitor(fifoPath, *emitShiftChars)
	if err := m.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "wtfrc-monitor: %v\n", err)
		return 1
	}
	return 0
}
