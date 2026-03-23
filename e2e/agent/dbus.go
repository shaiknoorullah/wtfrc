//go:build e2e

package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// CapturedNotification holds a notification intercepted from D-Bus.
type CapturedNotification struct {
	AppName string
	Summary string
	Body    string
	Time    time.Time
}

// DBusCapture monitors the session D-Bus for org.freedesktop.Notifications
// method calls and records them for test assertions.
type DBusCapture struct {
	conn          *dbus.Conn
	notifications []CapturedNotification
	mu            sync.Mutex
	cancel        context.CancelFunc
}

// NewDBusCapture creates a D-Bus capture that monitors notifications.
// It connects to the session bus and subscribes to Notify method calls.
func NewDBusCapture() (*DBusCapture, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("connect session bus: %w", err)
	}

	c := &DBusCapture{
		conn: conn,
	}

	return c, nil
}

// Start begins capturing notifications. Call Stop to clean up.
func (c *DBusCapture) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	// Add a match rule for Notify method calls on the notifications interface.
	// We match on the interface rather than acting as the server, so dunst
	// still handles the actual display.
	matchRule := "type='method_call',interface='org.freedesktop.Notifications',member='Notify'"
	if err := c.conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.AddMatch", 0, matchRule).Err; err != nil {
		cancel()
		return fmt.Errorf("add match rule: %w", err)
	}

	ch := make(chan *dbus.Message, 32)
	c.conn.Eavesdrop(ch)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				c.handleMessage(msg)
			}
		}
	}()

	return nil
}

// handleMessage processes a D-Bus message and extracts notification details.
func (c *DBusCapture) handleMessage(msg *dbus.Message) {
	if msg == nil {
		return
	}

	// Notify method signature: (STRING app_name, UINT32 replaces_id, STRING app_icon,
	//                           STRING summary, STRING body, ARRAY actions,
	//                           DICT hints, INT32 expire_timeout)
	if msg.Headers[dbus.FieldInterface].String() != `"org.freedesktop.Notifications"` {
		return
	}
	if msg.Headers[dbus.FieldMember].String() != `"Notify"` {
		return
	}

	if len(msg.Body) < 5 {
		return
	}

	appName, _ := msg.Body[0].(string)
	summary, _ := msg.Body[3].(string)
	body, _ := msg.Body[4].(string)

	c.mu.Lock()
	c.notifications = append(c.notifications, CapturedNotification{
		AppName: appName,
		Summary: summary,
		Body:    body,
		Time:    time.Now(),
	})
	c.mu.Unlock()
}

// Stop stops capturing and closes the D-Bus connection.
func (c *DBusCapture) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
	if c.conn != nil {
		c.conn.Close()
	}
}

// Notifications returns all captured notifications.
func (c *DBusCapture) Notifications() []CapturedNotification {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]CapturedNotification, len(c.notifications))
	copy(result, c.notifications)
	return result
}

// Clear removes all captured notifications.
func (c *DBusCapture) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notifications = c.notifications[:0]
}

// WaitForNotification waits until a notification matching the predicate is captured,
// or the timeout expires.
func (c *DBusCapture) WaitForNotification(timeout time.Duration, match func(CapturedNotification) bool) (*CapturedNotification, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for _, n := range c.notifications {
			if match(n) {
				c.mu.Unlock()
				return &n, nil
			}
		}
		c.mu.Unlock()
		time.Sleep(100 * time.Millisecond)
	}
	return nil, fmt.Errorf("no matching notification within %v", timeout)
}

// WaitForNotificationContaining waits for a notification whose body contains
// the given substring.
func (c *DBusCapture) WaitForNotificationContaining(timeout time.Duration, substr string) (*CapturedNotification, error) {
	return c.WaitForNotification(timeout, func(n CapturedNotification) bool {
		return strings.Contains(n.Body, substr) || strings.Contains(n.Summary, substr)
	})
}
