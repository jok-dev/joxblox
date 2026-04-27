//go:build windows

package renderdoc

import (
	"fmt"
	"syscall"
	"unsafe"
)

// TriggerCapture asks RenderDoc's in-process hook to grab a frame on the
// next swap. Implemented as a system-level F12 keystroke via SendInput,
// which is what RenderDoc's low-level keyboard hook listens for. The
// Studio window does not need to be focused — low-level hooks fire
// regardless of focus.
//
// Side note: any other process with an LL keyboard hook will also see
// the F12. In practice browsers and similar only act on F12 when focused,
// so this is harmless background.
func TriggerCapture() error {
	const (
		inputKeyboard      = 1
		vkF12              = 0x7B
		keyEventfKeyup     = 0x0002
		keyEventfScancode  = 0x0008
	)

	// INPUT struct for SendInput. KEYBDINPUT is the largest variant of
	// the union; we lay out the full union as raw bytes here so it
	// matches the C union layout exactly.
	type keyboardInput struct {
		Type uint32
		_    uint32 // padding to 64-bit boundary on x64
		// KEYBDINPUT fields:
		WVk         uint16
		WScan       uint16
		DwFlags     uint32
		Time        uint32
		DwExtraInfo uintptr
		// MOUSEINPUT is larger; pad to its size so SendInput sees a
		// fully-formed INPUT struct of correct cbSize.
		_ [8]byte
	}

	user32 := syscall.NewLazyDLL("user32.dll")
	sendInput := user32.NewProc("SendInput")

	keyDown := keyboardInput{Type: inputKeyboard, WVk: vkF12}
	keyUp := keyboardInput{Type: inputKeyboard, WVk: vkF12, DwFlags: keyEventfKeyup}
	inputs := [2]keyboardInput{keyDown, keyUp}
	cbSize := unsafe.Sizeof(keyboardInput{})

	r1, _, callErr := sendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		cbSize,
	)
	if r1 != uintptr(len(inputs)) {
		return fmt.Errorf("SendInput injected %d/%d events: %v", r1, len(inputs), callErr)
	}
	return nil
}
