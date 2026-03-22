package monitor

import (
	"testing"
)

// TestCheckInputGroup verifies the function runs without panic.
// In CI the current user is likely not in the input group, so we only
// verify a non-panic return (either nil or a descriptive error).
func TestCheckInputGroup(t *testing.T) {
	err := CheckInputGroup()
	// Either outcome is acceptable in a test environment.
	t.Logf("CheckInputGroup returned: %v", err)
}

// TestFindKeyboard verifies FindKeyboard returns a non-empty string or an error.
func TestFindKeyboard(t *testing.T) {
	path, err := FindKeyboard()
	// In a CI environment without evdev devices we expect an error.
	// What we must NOT have is an empty path paired with a nil error.
	if err == nil && path == "" {
		t.Error("FindKeyboard returned empty path with nil error")
	}
	t.Logf("FindKeyboard: path=%q err=%v", path, err)
}

// TestFindMouse verifies FindMouse returns a non-empty string or an error.
func TestFindMouse(t *testing.T) {
	path, err := FindMouse()
	if err == nil && path == "" {
		t.Error("FindMouse returned empty path with nil error")
	}
	t.Logf("FindMouse: path=%q err=%v", path, err)
}
