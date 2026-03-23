//go:build e2e

package agent

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// InputDevice abstracts a virtual keyboard and mouse for input simulation.
// In a real E2E run, this wraps bendahl/uinput. In test compilation without
// uinput available, callers can use the Noop implementation.
type InputDevice interface {
	// KeyPress presses and releases a single key.
	KeyPress(keycode int) error
	// KeyDown holds a key down.
	KeyDown(keycode int) error
	// KeyUp releases a held key.
	KeyUp(keycode int) error
	// MouseMove moves the mouse by relative x, y.
	MouseMove(dx, dy int32) error
	// MouseClick performs a left mouse click.
	MouseClick() error
	// MouseRightClick performs a right mouse click.
	MouseRightClick() error
	// Close releases the virtual devices.
	Close() error
}

// Well-known Linux input keycodes (from linux/input-event-codes.h).
// These match the values used by evdev and uinput.
const (
	KeyEsc       = 1
	Key1         = 2
	Key2         = 3
	Key3         = 4
	Key4         = 5
	Key5         = 6
	Key6         = 7
	Key7         = 8
	Key8         = 9
	Key9         = 10
	Key0         = 11
	KeyMinus     = 12
	KeyEqual     = 13
	KeyBackspace = 14
	KeyTab       = 15
	KeyQ         = 16
	KeyW         = 17
	KeyE         = 18
	KeyR         = 19
	KeyT         = 20
	KeyU         = 22
	KeyI         = 23
	KeyO         = 24
	KeyP         = 25
	KeyLeftBrace = 26
	KeyRightBrace = 27
	KeyEnter     = 28
	KeyLeftCtrl  = 29
	KeyA         = 30
	KeyS         = 31
	KeyD         = 32
	KeyF         = 33
	KeyG         = 34
	KeyH         = 35
	KeyJ         = 36
	KeyK         = 37
	KeyL         = 38
	KeySemicolon = 39
	KeyApostrophe = 40
	KeyGrave     = 41
	KeyLeftShift = 42
	KeyBackslash = 43
	KeyZ         = 44
	KeyX         = 45
	KeyC         = 46
	KeyV         = 47
	KeyB         = 48
	KeyN         = 49
	KeyM         = 50
	KeyComma     = 51
	KeyDot       = 52
	KeySlash     = 53
	KeyRightShift = 54
	KeyLeftAlt   = 56
	KeySpace     = 57
	KeyCapsLock  = 58
	KeyF1        = 59
	KeyUp        = 103
	KeyLeft      = 105
	KeyRight     = 106
	KeyDown      = 108
	KeyLeftMeta  = 125 // Super key
	KeyRightMeta = 126
	KeyY         = 21
)

// charToKeycode maps ASCII characters to their keycodes and whether shift is needed.
var charToKeycode = map[rune]struct {
	code  int
	shift bool
}{
	'a': {KeyA, false}, 'b': {KeyB, false}, 'c': {KeyC, false},
	'd': {KeyD, false}, 'e': {KeyE, false}, 'f': {KeyF, false},
	'g': {KeyG, false}, 'h': {KeyH, false}, 'i': {KeyI, false},
	'j': {KeyJ, false}, 'k': {KeyK, false}, 'l': {KeyL, false},
	'm': {KeyM, false}, 'n': {KeyN, false}, 'o': {KeyO, false},
	'p': {KeyP, false}, 'q': {KeyQ, false}, 'r': {KeyR, false},
	's': {KeyS, false}, 't': {KeyT, false}, 'u': {KeyU, false},
	'v': {KeyV, false}, 'w': {KeyW, false}, 'x': {KeyX, false},
	'y': {KeyY, false}, 'z': {KeyZ, false},
	'1': {Key1, false}, '2': {Key2, false}, '3': {Key3, false},
	'4': {Key4, false}, '5': {Key5, false}, '6': {Key6, false},
	'7': {Key7, false}, '8': {Key8, false}, '9': {Key9, false},
	'0': {Key0, false},
	' ': {KeySpace, false},
	'-': {KeyMinus, false}, '=': {KeyEqual, false},
	'[': {KeyLeftBrace, false}, ']': {KeyRightBrace, false},
	';': {KeySemicolon, false}, '\'': {KeyApostrophe, false},
	'`': {KeyGrave, false}, '\\': {KeyBackslash, false},
	',': {KeyComma, false}, '.': {KeyDot, false},
	'/': {KeySlash, false}, '\t': {KeyTab, false},
	'\n': {KeyEnter, false},
	// Shifted characters
	'!': {Key1, true}, '@': {Key2, true}, '#': {Key3, true},
	'$': {Key4, true}, '%': {Key5, true}, '^': {Key6, true},
	'&': {Key7, true}, '*': {Key8, true}, '(': {Key9, true},
	')': {Key0, true}, '_': {KeyMinus, true}, '+': {KeyEqual, true},
	'{': {KeyLeftBrace, true}, '}': {KeyRightBrace, true},
	':': {KeySemicolon, true}, '"': {KeyApostrophe, true},
	'~': {KeyGrave, true}, '|': {KeyBackslash, true},
	'<': {KeyComma, true}, '>': {KeyDot, true},
	'?': {KeySlash, true},
}

// KeyCombo represents a key combination like "Super+j" or "Ctrl+Shift+t".
type KeyCombo struct {
	Modifiers []int
	Key       int
}

// ParseKeyCombo parses a human-readable key combo string.
// Examples: "Super+j", "Ctrl+Shift+t", "Return", "Up"
func ParseKeyCombo(s string) (KeyCombo, error) {
	parts := strings.Split(s, "+")
	combo := KeyCombo{}

	for i, part := range parts {
		part = strings.TrimSpace(part)
		isLast := i == len(parts)-1

		switch strings.ToLower(part) {
		case "super", "mod", "$mod":
			combo.Modifiers = append(combo.Modifiers, KeyLeftMeta)
		case "ctrl", "control":
			combo.Modifiers = append(combo.Modifiers, KeyLeftCtrl)
		case "shift":
			combo.Modifiers = append(combo.Modifiers, KeyLeftShift)
		case "alt":
			combo.Modifiers = append(combo.Modifiers, KeyLeftAlt)
		case "return", "enter":
			if isLast {
				combo.Key = KeyEnter
			}
		case "space":
			if isLast {
				combo.Key = KeySpace
			}
		case "escape", "esc":
			if isLast {
				combo.Key = KeyEsc
			}
		case "tab":
			if isLast {
				combo.Key = KeyTab
			}
		case "up":
			if isLast {
				combo.Key = KeyUp
			}
		case "down":
			if isLast {
				combo.Key = KeyDown
			}
		case "left":
			if isLast {
				combo.Key = KeyLeft
			}
		case "right":
			if isLast {
				combo.Key = KeyRight
			}
		default:
			if isLast && len(part) == 1 {
				r := rune(part[0])
				if mapping, ok := charToKeycode[unicode.ToLower(r)]; ok {
					combo.Key = mapping.code
					if unicode.IsUpper(r) {
						combo.Modifiers = append(combo.Modifiers, KeyLeftShift)
					}
				} else {
					return KeyCombo{}, fmt.Errorf("unknown key: %q", part)
				}
			} else if !isLast {
				return KeyCombo{}, fmt.Errorf("unknown modifier: %q", part)
			} else {
				return KeyCombo{}, fmt.Errorf("unknown key: %q", part)
			}
		}
	}

	if combo.Key == 0 && len(combo.Modifiers) > 0 {
		return KeyCombo{}, fmt.Errorf("key combo has modifiers but no key: %q", s)
	}

	return combo, nil
}

// InputDelay is the delay between key events to prevent input races.
const InputDelay = 20 * time.Millisecond

// PressCombo presses a key combination on the given InputDevice.
func PressCombo(dev InputDevice, combo KeyCombo) error {
	// Press modifiers
	for _, mod := range combo.Modifiers {
		if err := dev.KeyDown(mod); err != nil {
			return fmt.Errorf("keydown modifier %d: %w", mod, err)
		}
		time.Sleep(InputDelay)
	}

	// Press and release the key
	if err := dev.KeyPress(combo.Key); err != nil {
		return fmt.Errorf("keypress %d: %w", combo.Key, err)
	}
	time.Sleep(InputDelay)

	// Release modifiers (reverse order)
	for i := len(combo.Modifiers) - 1; i >= 0; i-- {
		if err := dev.KeyUp(combo.Modifiers[i]); err != nil {
			return fmt.Errorf("keyup modifier %d: %w", combo.Modifiers[i], err)
		}
		time.Sleep(InputDelay)
	}

	return nil
}

// TypeText types a string of text character by character.
func TypeText(dev InputDevice, text string) error {
	for _, r := range text {
		mapping, ok := charToKeycode[r]
		if !ok {
			// Try lowercase
			mapping, ok = charToKeycode[unicode.ToLower(r)]
			if !ok {
				return fmt.Errorf("no keycode mapping for character %q", string(r))
			}
			// If the original was uppercase, we need shift
			if unicode.IsUpper(r) {
				mapping.shift = true
			}
		}

		if mapping.shift {
			if err := dev.KeyDown(KeyLeftShift); err != nil {
				return err
			}
			time.Sleep(InputDelay)
		}

		if err := dev.KeyPress(mapping.code); err != nil {
			return err
		}
		time.Sleep(InputDelay)

		if mapping.shift {
			if err := dev.KeyUp(KeyLeftShift); err != nil {
				return err
			}
			time.Sleep(InputDelay)
		}
	}
	return nil
}

// NoopInputDevice is a no-op implementation for compilation without uinput.
type NoopInputDevice struct{}

func (n *NoopInputDevice) KeyPress(_ int) error        { return nil }
func (n *NoopInputDevice) KeyDown(_ int) error         { return nil }
func (n *NoopInputDevice) KeyUp(_ int) error           { return nil }
func (n *NoopInputDevice) MouseMove(_, _ int32) error  { return nil }
func (n *NoopInputDevice) MouseClick() error           { return nil }
func (n *NoopInputDevice) MouseRightClick() error      { return nil }
func (n *NoopInputDevice) Close() error                { return nil }
