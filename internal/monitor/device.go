package monitor

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// keydVirtualDevice is the virtual keyboard device created by keyd when it is
// running. Prefer this over raw hardware keyboards because keyd already
// aggregates and remaps all physical keyboards.
const keydVirtualDevice = "/dev/input/by-id/keyd-virtual-keyboard"

// CheckInputGroup returns nil when the current process user is a member of the
// "input" group, or a descriptive error explaining how to fix the issue.
func CheckInputGroup() error {
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("monitor: could not determine current user: %w", err)
	}

	gids, err := u.GroupIds()
	if err != nil {
		return fmt.Errorf("monitor: could not list groups for user %s: %w", u.Username, err)
	}

	// Resolve "input" group ID.
	inputGroup, err := user.LookupGroup("input")
	if err != nil {
		// Some minimal environments (Docker, CI) may not have an "input" group.
		return fmt.Errorf("monitor: \"input\" group not found on this system: %w", err)
	}

	for _, gid := range gids {
		if gid == inputGroup.Gid {
			return nil
		}
	}

	return fmt.Errorf(
		"monitor: user %q is not in the \"input\" group.\n"+
			"Add yourself with: sudo usermod -aG input %s\n"+
			"Then log out and back in (or run: newgrp input).",
		u.Username, u.Username,
	)
}

// FindKeyboard returns the path to the preferred keyboard evdev device.
//
// Priority:
//  1. keyd virtual keyboard (aggregates all physical keyboards + remapping)
//  2. First /dev/input/event* device that contains "keyboard" in its /proc name
//  3. First available /dev/input/event* device as a last resort
//
// Returns an error when no readable keyboard device can be found.
func FindKeyboard() (string, error) {
	// Prefer the keyd virtual device.
	if _, err := os.Stat(keydVirtualDevice); err == nil {
		return keydVirtualDevice, nil
	}

	// Walk /dev/input/by-path and /dev/input/by-id for named keyboards.
	for _, dir := range []string{"/dev/input/by-path", "/dev/input/by-id"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := strings.ToLower(e.Name())
			if strings.Contains(name, "kbd") || strings.Contains(name, "keyboard") {
				resolved, err := filepath.EvalSymlinks(filepath.Join(dir, e.Name()))
				if err == nil {
					return resolved, nil
				}
			}
		}
	}

	// Fall back to any readable event device.
	return findFirstEventDevice()
}

// FindMouse returns the path to the mouse evdev device.
//
// Looks for devices containing "mouse" in their by-id/by-path name, then
// falls back to the first available event device.
func FindMouse() (string, error) {
	for _, dir := range []string{"/dev/input/by-id", "/dev/input/by-path"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := strings.ToLower(e.Name())
			if strings.Contains(name, "mouse") {
				resolved, err := filepath.EvalSymlinks(filepath.Join(dir, e.Name()))
				if err == nil {
					return resolved, nil
				}
			}
		}
	}

	return findFirstEventDevice()
}

// findFirstEventDevice returns the path to the first readable /dev/input/eventN
// device, or an error when none can be found.
func findFirstEventDevice() (string, error) {
	entries, err := os.ReadDir("/dev/input")
	if err != nil {
		return "", fmt.Errorf("monitor: cannot read /dev/input: %w", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "event") {
			path := filepath.Join("/dev/input", e.Name())
			f, err := os.Open(path)
			if err == nil {
				f.Close()
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("monitor: no readable evdev device found under /dev/input")
}
