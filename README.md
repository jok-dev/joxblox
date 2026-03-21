# Joxblox

Simple cross-platform Go GUI app that:

- Accepts Roblox asset IDs with or without `rbxassetid://`
- Fetches and previews the asset image at a fixed size
- Opens the image in a separate window
- Shows asset ID, dimensions, and image size in MB
- Includes a Folder Scan mode for finding Roblox asset IDs in text files and browsing results in a sortable table
- Includes an RBXL Scan mode for extracting asset IDs directly from `.rbxl` place files
- Supports optional `.ROBLOSECURITY` authentication for gated AssetDelivery requests
- Can securely remember `.ROBLOSECURITY` using the OS keychain/credential store

## Requirements

- Go 1.22+

## Run

```bash
go mod tidy
go run ./cmd/joxblox
```

## Build

```bash
go build ./cmd/joxblox
```

## macOS Dock icon

When running via `go run .`, macOS may still show a generic runtime icon.
To get the app icon in Dock/Task Switcher, package as a `.app` bundle:

```bash
go install fyne.io/tools/cmd/fyne@latest
fyne package -os darwin -src ./cmd/joxblox
open "./cmd/joxblox/Joxblox.app"
```
