package monitor

import (
	"testing"
)

// TestClassifierModifiers tests modifier-key combos.
func TestClassifierModifiers(t *testing.T) {
	tests := []struct {
		name           string
		emitShiftChars bool
		events         []struct{ evType, evCode uint16; evValue int32 }
		wantLast       *ClassifiedEvent
	}{
		{
			name: "Super_L + KEY_J emits $mod+j",
			events: []struct{ evType, evCode uint16; evValue int32 }{
				{EV_KEY, KEY_LEFTMETA, 1},
				{EV_KEY, KEY_J, 1},
			},
			wantLast: &ClassifiedEvent{Type: "key", Combo: "$mod+j"},
		},
		{
			name: "Ctrl_L + Shift_L + KEY_T emits Ctrl+Shift+t",
			events: []struct{ evType, evCode uint16; evValue int32 }{
				{EV_KEY, KEY_LEFTCTRL, 1},
				{EV_KEY, KEY_LEFTSHIFT, 1},
				{EV_KEY, KEY_T, 1},
			},
			wantLast: &ClassifiedEvent{Type: "key", Combo: "Ctrl+Shift+t"},
		},
		{
			name: "KEY_A alone (no mods) is discarded for privacy",
			events: []struct{ evType, evCode uint16; evValue int32 }{
				{EV_KEY, KEY_A, 1},
			},
			wantLast: nil,
		},
		{
			name:           "Shift_L + KEY_A with emitShiftChars=false returns nil",
			emitShiftChars: false,
			events: []struct{ evType, evCode uint16; evValue int32 }{
				{EV_KEY, KEY_LEFTSHIFT, 1},
				{EV_KEY, KEY_A, 1},
			},
			wantLast: nil,
		},
		{
			name:           "Shift_L + KEY_A with emitShiftChars=true returns Shift+a",
			emitShiftChars: true,
			events: []struct{ evType, evCode uint16; evValue int32 }{
				{EV_KEY, KEY_LEFTSHIFT, 1},
				{EV_KEY, KEY_A, 1},
			},
			wantLast: &ClassifiedEvent{Type: "key", Combo: "Shift+a"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClassifier(tc.emitShiftChars)
			var got *ClassifiedEvent
			for _, ev := range tc.events {
				got = c.Classify(ev.evType, ev.evCode, ev.evValue)
			}
			if tc.wantLast == nil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected %+v, got nil", tc.wantLast)
			}
			if got.Type != tc.wantLast.Type || got.Combo != tc.wantLast.Combo {
				t.Errorf("got {Type:%q Combo:%q}, want {Type:%q Combo:%q}",
					got.Type, got.Combo, tc.wantLast.Type, tc.wantLast.Combo)
			}
		})
	}
}

// TestClassifierMouseButtons verifies mouse button events.
func TestClassifierMouseButtons(t *testing.T) {
	tests := []struct {
		name   string
		events []struct{ evType, evCode uint16; evValue int32 }
		want   *ClassifiedEvent
	}{
		{
			name: "BTN_LEFT press emits mouse:click:left",
			events: []struct{ evType, evCode uint16; evValue int32 }{
				{EV_KEY, BTN_LEFT, 1},
			},
			want: &ClassifiedEvent{Type: "mouse:click:left", Combo: "mouse:click:left"},
		},
		{
			name: "Ctrl held + BTN_LEFT still emits mouse:click:left (not Ctrl+BTN_LEFT)",
			events: []struct{ evType, evCode uint16; evValue int32 }{
				{EV_KEY, KEY_LEFTCTRL, 1},
				{EV_KEY, BTN_LEFT, 1},
			},
			want: &ClassifiedEvent{Type: "mouse:click:left", Combo: "mouse:click:left"},
		},
		{
			name: "BTN_RIGHT press emits mouse:click:right",
			events: []struct{ evType, evCode uint16; evValue int32 }{
				{EV_KEY, BTN_RIGHT, 1},
			},
			want: &ClassifiedEvent{Type: "mouse:click:right", Combo: "mouse:click:right"},
		},
		{
			name: "BTN_MIDDLE press emits mouse:click:middle",
			events: []struct{ evType, evCode uint16; evValue int32 }{
				{EV_KEY, BTN_MIDDLE, 1},
			},
			want: &ClassifiedEvent{Type: "mouse:click:middle", Combo: "mouse:click:middle"},
		},
		{
			name: "BTN_LEFT release (value=0) emits nothing",
			events: []struct{ evType, evCode uint16; evValue int32 }{
				{EV_KEY, BTN_LEFT, 0},
			},
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClassifier(false)
			var got *ClassifiedEvent
			for _, ev := range tc.events {
				got = c.Classify(ev.evType, ev.evCode, ev.evValue)
			}
			if tc.want == nil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected %+v, got nil", tc.want)
			}
			if got.Type != tc.want.Type {
				t.Errorf("got Type=%q, want %q", got.Type, tc.want.Type)
			}
		})
	}
}

// TestClassifierScroll verifies REL_WHEEL events produce scroll events.
func TestClassifierScroll(t *testing.T) {
	tests := []struct {
		name     string
		evValue  int32
		wantType string
	}{
		{"scroll up (positive)", 1, "mouse:scroll:up"},
		{"scroll down (negative)", -1, "mouse:scroll:down"},
		{"scroll up (large)", 3, "mouse:scroll:up"},
		{"scroll down (large)", -3, "mouse:scroll:down"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClassifier(false)
			got := c.Classify(EV_REL, REL_WHEEL, tc.evValue)
			if got == nil {
				t.Fatalf("expected ClassifiedEvent{Type:%q}, got nil", tc.wantType)
			}
			if got.Type != tc.wantType {
				t.Errorf("got Type=%q, want %q", got.Type, tc.wantType)
			}
		})
	}
}

// TestClassifierModifierRelease verifies that releasing a modifier clears it.
func TestClassifierModifierRelease(t *testing.T) {
	c := NewClassifier(false)

	// Super_L down, KEY_J down → combo emitted
	c.Classify(EV_KEY, KEY_LEFTMETA, 1)
	got := c.Classify(EV_KEY, KEY_J, 1)
	if got == nil || got.Combo != "$mod+j" {
		t.Fatalf("step1: expected $mod+j, got %v", got)
	}

	// Super_L up → modifier cleared
	c.Classify(EV_KEY, KEY_LEFTMETA, 0)

	// KEY_K down → nil (no modifier active)
	got = c.Classify(EV_KEY, KEY_K, 1)
	if got != nil {
		t.Errorf("step3: expected nil after modifier release, got %+v", got)
	}
}

// TestClassifierEVSYNIgnored verifies that EV_SYN events produce nil.
func TestClassifierEVSYNIgnored(t *testing.T) {
	c := NewClassifier(false)
	got := c.Classify(0x00, 0x00, 0) // EV_SYN / SYN_REPORT
	if got != nil {
		t.Errorf("expected nil for EV_SYN, got %+v", got)
	}
}

// TestClassifierAltCombo verifies Alt modifier combos.
func TestClassifierAltCombo(t *testing.T) {
	c := NewClassifier(false)
	c.Classify(EV_KEY, KEY_LEFTALT, 1)
	got := c.Classify(EV_KEY, KEY_F4, 1)
	if got == nil {
		t.Fatal("expected Alt+F4, got nil")
	}
	if got.Combo != "Alt+F4" {
		t.Errorf("got Combo=%q, want %q", got.Combo, "Alt+F4")
	}
}
