//go:build windows

package clipboard

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClipboardFormatW = user32.NewProc("RegisterClipboardFormatW")
	procOpenClipboard            = user32.NewProc("OpenClipboard")
	procEmptyClipboard           = user32.NewProc("EmptyClipboard")
	procSetClipboardData         = user32.NewProc("SetClipboardData")
	procCloseClipboard           = user32.NewProc("CloseClipboard")

	procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
	procGlobalFree   = kernel32.NewProc("GlobalFree")
)

const (
	// GMEM_MOVEABLE is the only flag supported by SetClipboardData; the
	// clipboard takes ownership of the moveable HGLOBAL and frees it.
	gmemMoveable = 0x0002
)

// SetRobloxModel places the given rbxm binary bytes onto the system
// clipboard under Roblox Studio's custom format. After this call
// returns nil, a Ctrl+V in Studio will paste the instance subtree.
//
// The Win32 contract: register the format name to get a numeric id,
// open the clipboard, empty it, allocate a moveable HGLOBAL, copy the
// payload in, hand the handle to SetClipboardData (which takes
// ownership), and close the clipboard. If SetClipboardData succeeds we
// must NOT free the handle ourselves; the OS now owns it.
func SetRobloxModel(payload []byte) error {
	if len(payload) == 0 {
		return fmt.Errorf("clipboard: empty payload")
	}

	formatName, encodeErr := syscall.UTF16PtrFromString(RobloxStudioFormat)
	if encodeErr != nil {
		return fmt.Errorf("clipboard: encode format name: %w", encodeErr)
	}
	formatID, _, registerErr := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(formatName)))
	if formatID == 0 {
		return fmt.Errorf("clipboard: RegisterClipboardFormatW failed: %v", registerErr)
	}

	if ok, _, openErr := procOpenClipboard.Call(0); ok == 0 {
		return fmt.Errorf("clipboard: OpenClipboard failed: %v", openErr)
	}
	defer procCloseClipboard.Call()

	if ok, _, emptyErr := procEmptyClipboard.Call(); ok == 0 {
		return fmt.Errorf("clipboard: EmptyClipboard failed: %v", emptyErr)
	}

	size := uintptr(len(payload))
	hMem, _, allocErr := procGlobalAlloc.Call(gmemMoveable, size)
	if hMem == 0 {
		return fmt.Errorf("clipboard: GlobalAlloc failed: %v", allocErr)
	}
	// On any error from this point until SetClipboardData succeeds, we
	// own hMem and must free it ourselves.
	freeOnError := true
	defer func() {
		if freeOnError {
			procGlobalFree.Call(hMem)
		}
	}()

	dst, _, lockErr := procGlobalLock.Call(hMem)
	if dst == 0 {
		return fmt.Errorf("clipboard: GlobalLock failed: %v", lockErr)
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(dst)), len(payload)), payload)
	procGlobalUnlock.Call(hMem)

	if ok, _, setErr := procSetClipboardData.Call(formatID, hMem); ok == 0 {
		return fmt.Errorf("clipboard: SetClipboardData failed: %v", setErr)
	}
	// SetClipboardData succeeded — the OS now owns hMem.
	freeOnError = false
	return nil
}
