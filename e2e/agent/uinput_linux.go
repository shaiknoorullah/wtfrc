//go:build e2e && linux

package agent

import (
	"fmt"

	"github.com/bendahl/uinput"
)

// UinputDevice wraps bendahl/uinput virtual keyboard and mouse devices
// to implement the InputDevice interface with real Linux uinput injection.
type UinputDevice struct {
	keyboard uinput.Keyboard
	mouse    uinput.Mouse
}

// NewUinputDevice creates real uinput virtual devices for keyboard and mouse.
// Requires /dev/uinput to be accessible (the test user must be in the input group).
func NewUinputDevice() (*UinputDevice, error) {
	kb, err := uinput.CreateKeyboard("/dev/uinput", []byte("wtfrc-test-kbd"))
	if err != nil {
		return nil, fmt.Errorf("create virtual keyboard: %w", err)
	}

	mouse, err := uinput.CreateMouse("/dev/uinput", []byte("wtfrc-test-mouse"))
	if err != nil {
		kb.Close()
		return nil, fmt.Errorf("create virtual mouse: %w", err)
	}

	return &UinputDevice{
		keyboard: kb,
		mouse:    mouse,
	}, nil
}

// KeyPress presses and releases a single key.
func (u *UinputDevice) KeyPress(keycode int) error {
	return u.keyboard.KeyPress(keycode)
}

// KeyDown holds a key down.
func (u *UinputDevice) KeyDown(keycode int) error {
	return u.keyboard.KeyDown(keycode)
}

// KeyUp releases a held key.
func (u *UinputDevice) KeyUp(keycode int) error {
	return u.keyboard.KeyUp(keycode)
}

// MouseMove moves the mouse by relative x, y.
func (u *UinputDevice) MouseMove(dx, dy int32) error {
	return u.mouse.Move(dx, dy)
}

// MouseClick performs a left mouse click.
func (u *UinputDevice) MouseClick() error {
	return u.mouse.LeftClick()
}

// MouseRightClick performs a right mouse click.
func (u *UinputDevice) MouseRightClick() error {
	return u.mouse.RightClick()
}

// Close releases the virtual devices.
func (u *UinputDevice) Close() error {
	kbErr := u.keyboard.Close()
	mouseErr := u.mouse.Close()
	if kbErr != nil {
		return kbErr
	}
	return mouseErr
}
