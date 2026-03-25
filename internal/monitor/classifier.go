// Package monitor provides OS-level input monitoring via the evdev interface.
// It classifies raw kernel input events into coaching-relevant combos while
// respecting a privacy boundary: modifier-less key presses are discarded.
package monitor

import "fmt"

// evdev event type and code constants (from <linux/input-event-codes.h>).
// Using raw integers keeps this package free of external evdev dependencies
// and makes the classifier fully testable without hardware.
const (
	EV_KEY = uint16(0x01)
	EV_REL = uint16(0x02)

	REL_WHEEL = uint16(0x08)

	// Mouse buttons (0x110–0x112).
	BTN_LEFT   = uint16(0x110)
	BTN_RIGHT  = uint16(0x111)
	BTN_MIDDLE = uint16(0x112)

	// Modifier keys.
	KEY_LEFTSHIFT  = uint16(42)
	KEY_RIGHTSHIFT = uint16(54)
	KEY_LEFTCTRL   = uint16(29)
	KEY_RIGHTCTRL  = uint16(97)
	KEY_LEFTALT    = uint16(56)
	KEY_RIGHTALT   = uint16(100)
	KEY_LEFTMETA   = uint16(125)
	KEY_RIGHTMETA  = uint16(126)

	// Alphanumeric keys.
	KEY_A = uint16(30)
	KEY_B = uint16(48)
	KEY_C = uint16(46)
	KEY_D = uint16(32)
	KEY_E = uint16(18)
	KEY_F = uint16(33)
	KEY_G = uint16(34)
	KEY_H = uint16(35)
	KEY_I = uint16(23)
	KEY_J = uint16(36)
	KEY_K = uint16(37)
	KEY_L = uint16(38)
	KEY_M = uint16(50)
	KEY_N = uint16(49)
	KEY_O = uint16(24)
	KEY_P = uint16(25)
	KEY_Q = uint16(16)
	KEY_R = uint16(19)
	KEY_S = uint16(31)
	KEY_T = uint16(20)
	KEY_U = uint16(22)
	KEY_V = uint16(47)
	KEY_W = uint16(17)
	KEY_X = uint16(45)
	KEY_Y = uint16(21)
	KEY_Z = uint16(44)

	KEY_1 = uint16(2)
	KEY_2 = uint16(3)
	KEY_3 = uint16(4)
	KEY_4 = uint16(5)
	KEY_5 = uint16(6)
	KEY_6 = uint16(7)
	KEY_7 = uint16(8)
	KEY_8 = uint16(9)
	KEY_9 = uint16(10)
	KEY_0 = uint16(11)

	// Special keys.
	KEY_ESC   = uint16(1)
	KEY_TAB   = uint16(15)
	KEY_ENTER = uint16(28)
	KEY_SPACE = uint16(57)

	// Function keys.
	KEY_F1  = uint16(59)
	KEY_F2  = uint16(60)
	KEY_F3  = uint16(61)
	KEY_F4  = uint16(62)
	KEY_F5  = uint16(63)
	KEY_F6  = uint16(64)
	KEY_F7  = uint16(65)
	KEY_F8  = uint16(66)
	KEY_F9  = uint16(67)
	KEY_F10 = uint16(68)
	KEY_F11 = uint16(87)
	KEY_F12 = uint16(88)

	// Arrow keys.
	KEY_UP    = uint16(103)
	KEY_LEFT  = uint16(105)
	KEY_RIGHT = uint16(106)
	KEY_DOWN  = uint16(108)

	// Modifier bitmask positions.
	modBitLShift  = uint32(1 << 0) // bit 0
	modBitRShift  = uint32(1 << 1) // bit 1
	modBitLCtrl   = uint32(1 << 2) // bit 2
	modBitRCtrl   = uint32(1 << 3) // bit 3
	modBitLAlt    = uint32(1 << 4) // bit 4
	modBitRAlt    = uint32(1 << 5) // bit 5
	modBitLSuper  = uint32(1 << 6) // bit 6
	modBitRSuper  = uint32(1 << 7) // bit 7

	maskShift = modBitLShift | modBitRShift
	maskCtrl  = modBitLCtrl | modBitRCtrl
	maskAlt   = modBitLAlt | modBitRAlt
	maskSuper = modBitLSuper | modBitRSuper
)

// keyNames maps evdev key codes to human-readable names used in combo strings.
var keyNames = map[uint16]string{
	KEY_ESC:   "Esc",
	KEY_TAB:   "Tab",
	KEY_ENTER: "Return",
	KEY_SPACE: "Space",

	KEY_A: "a", KEY_B: "b", KEY_C: "c", KEY_D: "d", KEY_E: "e",
	KEY_F: "f", KEY_G: "g", KEY_H: "h", KEY_I: "i", KEY_J: "j",
	KEY_K: "k", KEY_L: "l", KEY_M: "m", KEY_N: "n", KEY_O: "o",
	KEY_P: "p", KEY_Q: "q", KEY_R: "r", KEY_S: "s", KEY_T: "t",
	KEY_U: "u", KEY_V: "v", KEY_W: "w", KEY_X: "x", KEY_Y: "y",
	KEY_Z: "z",

	KEY_1: "1", KEY_2: "2", KEY_3: "3", KEY_4: "4", KEY_5: "5",
	KEY_6: "6", KEY_7: "7", KEY_8: "8", KEY_9: "9", KEY_0: "0",

	KEY_F1: "F1", KEY_F2: "F2", KEY_F3: "F3", KEY_F4: "F4",
	KEY_F5: "F5", KEY_F6: "F6", KEY_F7: "F7", KEY_F8: "F8",
	KEY_F9: "F9", KEY_F10: "F10", KEY_F11: "F11", KEY_F12: "F12",

	KEY_UP: "Up", KEY_DOWN: "Down", KEY_LEFT: "Left", KEY_RIGHT: "Right",
}

// modifierKeys maps evdev key codes to their corresponding bitmask bit.
var modifierKeys = map[uint16]uint32{
	KEY_LEFTSHIFT:  modBitLShift,
	KEY_RIGHTSHIFT: modBitRShift,
	KEY_LEFTCTRL:   modBitLCtrl,
	KEY_RIGHTCTRL:  modBitRCtrl,
	KEY_LEFTALT:    modBitLAlt,
	KEY_RIGHTALT:   modBitRAlt,
	KEY_LEFTMETA:   modBitLSuper,
	KEY_RIGHTMETA:  modBitRSuper,
}

// ClassifiedEvent is the output of the Classifier for a single evdev event.
type ClassifiedEvent struct {
	// Type is a coarse category: "key", "mouse:click:left", "mouse:click:right",
	// "mouse:click:middle", "mouse:scroll:up", or "mouse:scroll:down".
	Type string
	// Combo is the human-readable representation, e.g. "$mod+j" or "Ctrl+Shift+t".
	// For mouse events it equals Type.
	Combo string
}

// Classifier tracks modifier state and classifies raw evdev events into
// coaching-relevant combos. It is not safe for concurrent use.
type Classifier struct {
	mods           uint32 // active-modifier bitmask
	emitShiftChars bool   // if false, Shift-only combos are silently discarded
}

// NewClassifier creates a Classifier. When emitShiftChars is true the
// classifier emits events for Shift+<letter> combos (e.g. "Shift+a");
// otherwise those events are discarded to respect the privacy boundary.
func NewClassifier(emitShiftChars bool) *Classifier {
	return &Classifier{emitShiftChars: emitShiftChars}
}

// Classify processes a single raw evdev event and returns a ClassifiedEvent
// when the event is coaching-relevant, or nil otherwise.
func (c *Classifier) Classify(evType uint16, evCode uint16, evValue int32) *ClassifiedEvent {
	switch evType {
	case EV_KEY:
		return c.classifyKey(evCode, evValue)
	case EV_REL:
		return c.classifyRel(evCode, evValue)
	}
	return nil
}

func (c *Classifier) classifyKey(code uint16, value int32) *ClassifiedEvent {
	// 1. Modifier key: update bitmask, return nil.
	if bit, isMod := modifierKeys[code]; isMod {
		if value == 1 {
			c.mods |= bit
		} else if value == 0 {
			c.mods &^= bit
		}
		// Repeat (value==2) intentionally left as-is (no change).
		return nil
	}

	// Only process press events (value == 1) from here on.
	if value != 1 {
		return nil
	}

	// 2. Mouse buttons: emit click event regardless of modifier state.
	switch code {
	case BTN_LEFT:
		return &ClassifiedEvent{Type: "mouse:click:left", Combo: "mouse:click:left"}
	case BTN_RIGHT:
		return &ClassifiedEvent{Type: "mouse:click:right", Combo: "mouse:click:right"}
	case BTN_MIDDLE:
		return &ClassifiedEvent{Type: "mouse:click:middle", Combo: "mouse:click:middle"}
	}

	// 3. Regular key: apply privacy boundary.
	if c.mods == 0 {
		// No modifier held — discard (privacy: would reveal typed characters).
		return nil
	}

	// 4. Shift-only: only emit if configured to do so.
	if c.mods&^maskShift == 0 && !c.emitShiftChars {
		return nil
	}

	// 5. Build combo string.
	combo := c.buildCombo(code)
	if combo == "" {
		// Unknown key code — discard rather than leak raw codes.
		return nil
	}
	return &ClassifiedEvent{Type: "key", Combo: combo}
}

func (c *Classifier) classifyRel(code uint16, value int32) *ClassifiedEvent {
	if code != REL_WHEEL {
		return nil
	}
	if value > 0 {
		return &ClassifiedEvent{Type: "mouse:scroll:up", Combo: "mouse:scroll:up"}
	}
	if value < 0 {
		return &ClassifiedEvent{Type: "mouse:scroll:down", Combo: "mouse:scroll:down"}
	}
	return nil
}

// buildCombo constructs the human-readable modifier+key combo string.
func (c *Classifier) buildCombo(code uint16) string {
	name, ok := keyNames[code]
	if !ok {
		return ""
	}

	prefix := ""
	if c.mods&maskCtrl != 0 {
		prefix += "Ctrl+"
	}
	if c.mods&maskAlt != 0 {
		prefix += "Alt+"
	}
	if c.mods&maskShift != 0 {
		prefix += "Shift+"
	}
	if c.mods&maskSuper != 0 {
		prefix += "$mod+"
	}

	return fmt.Sprintf("%s%s", prefix, name)
}
