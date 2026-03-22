package coach

import (
	"context"
	"database/sql"
	"log"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/shaiknoorullah/wtfrc/internal/config"
	"github.com/shaiknoorullah/wtfrc/internal/kb"
	"github.com/shaiknoorullah/wtfrc/internal/llm"
)

// Daemon is the top-level coach process.  It wires together the FIFO reader,
// event pipeline (correlator → matcher → throttle → roaster → dispatcher), and
// graduation manager.  Hot-reload is supported via SIGHUP.
type Daemon struct {
	cfg        *config.Config
	cfgPtr     atomic.Pointer[config.Config] // for hot reload
	cfgPath    string                        // path to config file (for reload)
	db         *kb.DB
	fastLLM    llm.Provider
	fifo       *FIFOReader
	matcher    *Matcher
	correlator *Correlator
	throttle   *Throttle
	graduation *GraduationManager
	roaster    *Roaster
	dispatcher *Dispatcher
}

// NewDaemon constructs and wires all coach subsystems.
//
//   - cfg:        initial configuration
//   - db:         knowledge-base database (provides Conn() *sql.DB and GetEntriesByTypes)
//   - fastLLM:    LLM provider for Roaster (may be nil for offline/template-only mode)
//   - fifoPath:   path to the named pipe used by shell hooks
//   - runtimeDir: XDG_RUNTIME_DIR used by Dispatcher for delivery paths
func NewDaemon(cfg *config.Config, db *kb.DB, fastLLM llm.Provider, fifoPath string, runtimeDir string) (*Daemon, error) {
	// 1. Build matcher from DB alias/function/keybind entries.
	entries, err := db.GetEntriesByTypes([]string{"alias", "function", "keybind"})
	if err != nil {
		return nil, err
	}
	matcher := NewMatcher(entries)

	// 2. Graduation manager.
	graduation := NewGraduationManager(db.Conn(), &cfg.Coach, nil)

	// 3. Throttle.
	throttle := NewThrottle(&cfg.Coach, graduation, nil)

	// 4. Correlator (150 ms window).
	correlator := NewCorrelator(150*time.Millisecond, nil)

	// 5. Roaster.
	roaster := NewRoaster(db.Conn(), fastLLM, &cfg.Coach)

	// 6. Dispatcher.
	dispatcher := NewDispatcher(&cfg.Coach.Delivery, runtimeDir)

	// 7. FIFO reader.
	fifo := NewFIFOReader(fifoPath)

	d := &Daemon{
		cfg:        cfg,
		db:         db,
		fastLLM:    fastLLM,
		fifo:       fifo,
		matcher:    matcher,
		correlator: correlator,
		throttle:   throttle,
		graduation: graduation,
		roaster:    roaster,
		dispatcher: dispatcher,
	}

	// 8. Store config pointer atomically.
	d.cfgPtr.Store(cfg)

	return d, nil
}

// Run starts all goroutines under an errgroup and blocks until they all return.
// It returns nil when the context is cancelled (normal shutdown).
func (d *Daemon) Run(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)

	events := make(chan Event, 256)

	// Mandatory: FIFO reader.
	eg.Go(func() error {
		return d.fifo.Run(ctx, events)
	})

	// Event processing pipeline.
	eg.Go(func() error {
		return d.processEvents(ctx, events)
	})

	// Signal handler.
	eg.Go(func() error {
		return d.handleSignals(ctx)
	})

	return eg.Wait()
}

// processEvents consumes events from the channel and runs the coaching pipeline.
func (d *Daemon) processEvents(ctx context.Context, events <-chan Event) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			d.handleEvent(ctx, ev)
		}
	}
}

// handleEvent executes the full coaching pipeline for a single event.
func (d *Daemon) handleEvent(ctx context.Context, ev Event) {
	cfg := d.cfgPtr.Load()

	// 1. Record every event to usage_events.
	_ = recordUsageEvent(d.db.Conn(), ev.Source, ev.Action, "", false)

	// 2. Correlate: suppress if triggered by a keybind.
	correlated := d.correlator.Process(ev)
	if correlated == nil {
		// Keybind event buffered, or result was keybind-triggered — no coaching.
		return
	}

	// 3. Match: look for a better alternative.
	suggestion := d.matcher.Match(correlated.Source, correlated.Action)
	if suggestion == nil {
		// No better alternative; action is already optimal.
		return
	}

	// 4. Throttle: check anti-annoyance gates.
	if !d.throttle.Allow(suggestion.ActionID, correlated.Source) {
		return
	}

	// 5. Generate message.
	msg := d.roaster.Generate(*suggestion, cfg.Coach.Mode)

	// 6. Dispatch.
	if err := d.dispatcher.Send(ctx, msg, correlated.Source); err != nil {
		log.Printf("wtfrc/coach: dispatch: %v", err)
	}

	// 7. Record coached in graduation manager (creates coaching_state row if needed).
	d.graduation.RecordCoached(suggestion.ActionID)

	// 8. Record to coaching_log (FK requires coaching_state row to exist first).
	if err := recordCoachingLog(
		d.db.Conn(),
		correlated.Source,
		suggestion.ActionID,
		correlated.Action,
		suggestion.Optimal,
		msg,
		cfg.Coach.Mode,
		cfg.Coach.Delivery.Shell, // delivery channel
	); err != nil {
		log.Printf("wtfrc/coach: recordCoachingLog: %v", err)
	}
}

// handleSignals listens for OS signals and reacts accordingly.
//   - SIGHUP  → hot-reload config
//   - SIGTERM, SIGINT → return nil (errgroup cancels the context)
func (d *Daemon) handleSignals(ctx context.Context) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	for {
		select {
		case <-ctx.Done():
			return nil
		case sig := <-sigCh:
			switch sig {
			case syscall.SIGHUP:
				if err := d.reload(); err != nil {
					log.Printf("wtfrc/coach: reload: %v", err)
				}
			case syscall.SIGTERM, syscall.SIGINT:
				// Return nil; the errgroup will cancel the context, stopping all goroutines.
				return nil
			}
		}
	}
}

// reload re-reads the config from disk, rebuilds the matcher, and updates the
// atomic config pointer.
func (d *Daemon) reload() error {
	newCfg, err := config.Load(d.cfgPath)
	if err != nil {
		return err
	}

	// Rebuild matcher from DB.
	entries, err := d.db.GetEntriesByTypes([]string{"alias", "function", "keybind"})
	if err != nil {
		return err
	}
	d.matcher = NewMatcher(entries)

	// Atomically update config pointer.
	d.cfgPtr.Store(newCfg)

	log.Printf("wtfrc/coach: config reloaded from %s (mode=%s)", d.cfgPath, newCfg.Coach.Mode)
	return nil
}

// ---------------------------------------------------------------------------
// DB helpers
// ---------------------------------------------------------------------------

// recordUsageEvent inserts a row into the usage_events table.
// optimalAction may be empty when there is no known alternative.
func recordUsageEvent(db *sql.DB, tool, action, optimalAction string, wasOptimal bool) error {
	wasOptimalInt := 0
	if wasOptimal {
		wasOptimalInt = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO usage_events (tool, action, optimal_action, timestamp, was_optimal, coached)
		 VALUES (?, ?, ?, ?, ?, 0)`,
		tool, action, optimalAction, now, wasOptimalInt,
	)
	return err
}

// recordCoachingLog inserts a row into the coaching_log table.
func recordCoachingLog(db *sql.DB, source, actionID, userAction, optimalAction, message, mode, delivery string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO coaching_log
		 (timestamp, source, action_id, user_action, optimal_action, message, mode, delivery)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		now, source, actionID, userAction, optimalAction, message, mode, delivery,
	)
	return err
}
