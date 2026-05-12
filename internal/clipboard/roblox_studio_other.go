//go:build !windows

package clipboard

import "fmt"

// SetRobloxModel is a no-op stub on non-Windows builds. joxblox ships
// Windows-first; if the app is ever ported to another platform, this is
// the integration point.
func SetRobloxModel(payload []byte) error {
	_ = payload
	return fmt.Errorf("clipboard: Roblox Studio paste is only supported on Windows")
}
