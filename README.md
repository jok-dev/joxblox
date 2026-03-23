# Joxblox

`Joxblox` is a cross-platform Fyne desktop app for inspecting Roblox assets, scanning folders and `.rbxl` files for asset references, and exporting/importing scan results.

## What It Does

- Load a single Roblox asset ID with or without the `rbxassetid://` prefix
- Preview image assets and inspect audio/image metadata
- Browse referenced child assets in a hierarchy view
- Scan folders for Roblox asset IDs in files
- Scan `.rbxl` place files using the Rust extractor
- Run diff scans for folders and `.rbxl` files to find only new references
- Filter scan results by asset type, instance type, property name, duplicates, search text, and more
- Show duplicate counts, duplicate size, shown size, dimensions, hashes, source/state, and asset metadata
- Save and load scan results as JSON
- Drag and drop results `.json` files onto the app window to import them
- Generate PNG images in the `Image Generator` tab for manual Roblox upload workflows
- View embedded changelog and about/license information from the Help menu
- Use optional `.ROBLOSECURITY` authentication for gated AssetDelivery requests
- Store `.ROBLOSECURITY` securely in the OS credential store/keychain

## App Layout

The app currently has three main tabs:

1. `Single Asset`
2. `Scan`
3. `Image Generator`

### Single Asset

Use this tab to paste a Roblox asset ID and inspect a single asset in detail.

It currently supports:

- Previewing the fetched asset
- Downloading the original asset or preview variants when available
- Opening the preview in an expanded zoomable view
- Showing dimensions, self size, total size, content type, asset type, source, failure reason, and referenced assets
- Showing reference metadata such as instance type, property name, and instance path
- Browsing the referenced asset hierarchy when the asset links to child assets
- Viewing raw JSON sections for AssetDelivery, thumbnail, economy, and Rust extractor data

### Scan

The `Scan` tab supports two source types and two modes:

- Sources: `RBXL`, `Folders`
- Modes: `Single`, `Diff`

That gives four scan workflows:

1. Single `.rbxl` scan
2. `.rbxl` diff scan
3. Single folder scan
4. Folder diff scan

Current scan-table capabilities include:

- Sortable result table
- Search across ID, type, source, hash, property name, and path-related fields
- Filters for asset type, instance type, and property name
- `Unknown` group support for instance type/property values that are missing
- `Show only duplicates` toggle
- Stats for total rows, shown rows, failed rows, duplicate count, duplicate size, and shown size
- Asset preview/details pane for the selected row
- Recent imported files per scan context
- JSON import/export with progress dialogs
- Drag-and-drop JSON import

### Image Generator

This tab generates `1024x1024` PNG files for manual upload workflows.

It currently supports:

- Configurable image count
- Pattern selection
- Output folder selection
- Stop/cancel during generation
- Listing generated file paths in the UI

## File Menu

The `File` menu currently includes:

- `Save Results (.json)` to save all scan tables across contexts into one workspace JSON file
- `Load Results (.json)` to restore all scan tables from a workspace JSON file
- `Clear All Results` to clear every loaded scan table after confirmation
- `Recent Files` for quick reload of previously imported results

## Help Menu

The `Help` menu currently includes:

- `Changelog` to view the embedded project changelog
- `About` to view the app version, author, source repository, and license summary
- a license viewer accessible from the About dialog

## Authentication

Joxblox supports optional `.ROBLOSECURITY` auth for requests that need a signed-in Roblox session.

- Paste the cookie into the auth field at the bottom of the window
- Click `Apply Auth` to enable it for the current session
- Enable `Save to keychain` to store it securely in the OS credential store
- Use `Clear Auth` to remove it from memory and delete the saved credential

Treat `.ROBLOSECURITY` like a password. Do not share it.

## Requirements

- Go `1.23+`
- Rust toolchain if you want to build the `.rbxl` extractor from source

## License

This project is licensed under the `GNU General Public License v3.0`. The full license text is available in [`LICENSE.md`](LICENSE.md) and is also viewable from the app UI.

## Run

For general app development:

```bash
go mod tidy
go run ./cmd/joxblox
```

## Build

Build the Go app only:

```bash
go build ./cmd/joxblox
```

Build both the Go app and the Rust `.rbxl` extractor from the repository root:

```bash
./build.sh
```

Build one target at a time:

```bash
./build.sh go
./build.sh rust
```

## Releases

Tagged pushes like `v1.0.0` now build per-platform single-binary releases in GitHub Actions and publish them as a GitHub release.

Each release binary includes:

- the `joxblox` app itself
- the embedded changelog text
- the embedded GPL v3 license text
- the embedded RBXL extractor payload used for `.rbxl` scanning at runtime

## Notes

- `.rbxl` scanning depends on the Rust helper under `tools/rbxl-id-extractor`
- release builds extract the bundled RBXL helper to a temporary location when `.rbxl` scanning is used
- Results JSON can represent either a single scan table or the full multi-context scan workspace
- The app writes debug output to `latest.log`

## macOS Dock Icon

When running via `go run`, macOS may still show a generic runtime icon. To get the app icon in Dock/Task Switcher, package it as a `.app` bundle:

```bash
go install fyne.io/tools/cmd/fyne@latest
fyne package -os darwin -src ./cmd/joxblox
open "./cmd/joxblox/Joxblox.app"
```
