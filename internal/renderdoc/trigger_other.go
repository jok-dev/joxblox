//go:build !windows

package renderdoc

import "errors"

// TriggerCapture is Windows-only for now. RenderDoc's capture hook
// listens for the configured key (default F12); on Linux/macOS we'd
// need a platform-specific input-injection path that hasn't been
// implemented yet. Use the Studio process directly until then.
func TriggerCapture() error {
	return errors.New("trigger capture is not implemented on this platform — focus Studio and press F12")
}
