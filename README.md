# Joxblox

`Joxblox` is a cross-platform Fyne desktop app for exploring Roblox assets, scanning files and places for asset references, and working with the results in a desktop UI.

## Features

The app currently includes five main workflows:

- `Single Asset`: inspect a Roblox asset, preview supported content, browse referenced child assets, and view raw response data
- `Scan`: scan folders or `.rbxl/.rbxm` files, diff results, filter/search rows, preview selected assets, and save or load JSON result sets
- `Heatmap`: turn `.rbxl/.rbxm` asset references into weighted map views with diff support and per-cell breakdowns
- `Optimize Assets`: find large image usage in a place file, filter candidates, resize them, and optionally upload optimized replacements
- `Image Generator`: generate PNG images in bulk and optionally upload them through Roblox Open Cloud

Other app features include:

- Mesh preview support for model assets
- Drag-and-drop JSON result importing
- Optional authenticated Roblox requests using `.ROBLOSECURITY`
- Embedded changelog and license info in release builds

## Requirements

- Go `1.23+`
- Rust toolchain if you want to build the bundled RBXL helper from source

## Run

```bash
go mod tidy
go run ./cmd/joxblox
```

## Build

Build the Go app only:

```bash
go build ./cmd/joxblox
```

Build the full app plus the Rust helper from the repository root:

```bash
./build.sh
```

Build one target at a time:

```bash
./build.sh go
./build.sh rust
```
