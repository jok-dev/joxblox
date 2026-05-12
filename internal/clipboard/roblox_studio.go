// Package clipboard writes Roblox-specific payloads to the system
// clipboard. Roblox Studio places copied instances onto the clipboard
// as raw rbxm binary bytes under the custom MIME format
// `application/x-roblox-studio`; SetRobloxModel reproduces that exact
// shape so a pasted instance round-trips back into Studio.
package clipboard

// RobloxStudioFormat is the MIME-style clipboard format identifier
// Roblox Studio uses for copied instances. Exported so non-Windows
// stubs can reference the same constant.
const RobloxStudioFormat = "application/x-roblox-studio"
