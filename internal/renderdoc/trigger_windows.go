//go:build windows

package renderdoc

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Windows VK / SendInput constants.
const (
	inputKeyboard     = 1
	vkF12             = 0x7B
	keyEventfKeyup    = 0x0002
	keyEventfScancode = 0x0008
	scanF12           = 0x58 // PS/2 set 1 scancode for F12
)

// keyboardInput mirrors the Windows INPUT struct (union variant =
// KEYBDINPUT) byte-for-byte on x64. Total size 40 bytes:
//
//	offset 0  type    (uint32)
//	offset 4  pad     (4 bytes)            ← union starts on 8-byte boundary
//	offset 8  wVk     (uint16)
//	offset 10 wScan   (uint16)
//	offset 12 dwFlags (uint32)
//	offset 16 time    (uint32)
//	offset 24 dwExtraInfo (uintptr, 8B)    ← Go auto-pads from 20 to 24 (8-align)
//	offset 32 pad     (8 bytes)            ← fill the union to MOUSEINPUT's 32B
type keyboardInput struct {
	Type        uint32
	_           uint32
	WVk         uint16
	WScan       uint16
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
	_           [8]byte
}

// TriggerCapture asks RenderDoc's in-process hook to grab a frame on the
// next swap. Default capture key is F12, listened for via a low-level
// Windows keyboard hook installed by RenderDoc inside the target
// process — fires regardless of which window is focused.
//
// Strategy: try SendInput (modern, preferred). If it returns 0 (e.g.
// blocked by UIPI when Studio is elevated), fall back to keybd_event
// which uses a different injection path.
func TriggerCapture() error {
	if err := triggerCaptureSendInput(); err == nil {
		return nil
	} else {
		// SendInput failed — try keybd_event before reporting.
		if err2 := triggerCaptureKeybdEvent(); err2 == nil {
			return nil
		} else {
			return fmt.Errorf("SendInput failed (%v) and keybd_event fallback failed (%v)", err, err2)
		}
	}
}

func triggerCaptureSendInput() error {
	user32 := syscall.NewLazyDLL("user32.dll")
	sendInput := user32.NewProc("SendInput")

	keyDown := keyboardInput{Type: inputKeyboard, WVk: vkF12, WScan: scanF12}
	keyUp := keyboardInput{Type: inputKeyboard, WVk: vkF12, WScan: scanF12, DwFlags: keyEventfKeyup}
	inputs := [2]keyboardInput{keyDown, keyUp}
	cbSize := unsafe.Sizeof(keyboardInput{})

	r1, _, callErr := sendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		cbSize,
	)
	if r1 != uintptr(len(inputs)) {
		return fmt.Errorf("SendInput injected %d/%d events (cbSize=%d): %v", r1, len(inputs), cbSize, callErr)
	}
	return nil
}

func triggerCaptureKeybdEvent() error {
	user32 := syscall.NewLazyDLL("user32.dll")
	keybdEvent := user32.NewProc("keybd_event")

	// keybd_event(BYTE bVk, BYTE bScan, DWORD dwFlags, ULONG_PTR dwExtraInfo)
	_, _, _ = keybdEvent.Call(uintptr(vkF12), uintptr(scanF12), 0, 0)
	_, _, _ = keybdEvent.Call(uintptr(vkF12), uintptr(scanF12), uintptr(keyEventfKeyup), 0)
	// keybd_event has no return code we can check; assume success.
	return nil
}
