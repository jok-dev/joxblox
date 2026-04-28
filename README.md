# Joxblox

`Joxblox` is a cross-platform Fyne desktop app for exploring Roblox assets, scanning files and places for asset references, and working with the results in a desktop UI.

## Features

Tabs:

- `Report Generation`: grade an `.rbxl/.rbxm` against a chosen asset type (Map / Vehicle), with per-cell avg/p90/max breakdowns for spatial scenes and drill-in views for mismatched PBR materials, oversized textures, and duplicate uploads
- `Single Asset`: inspect a Roblox asset, preview supported content, browse child references, and view raw response data
- `Scan`: scan folders or `.rbxl/.rbxm` files, diff results, filter/search rows, preview, and save/load JSON result sets
- `Heatmap`: turn `.rbxl/.rbxm` asset references into weighted top-down map views with diff support and per-cell breakdowns
- `3D Heatmap`: heatmap a single model asset by mesh part with full 3D camera controls
- `LOD Viewer`: inspect a mesh's LOD levels side-by-side
- `Optimize Assets`: find large image usage in a place file, filter candidates, resize them, and optionally upload replacements
- `Image Generator`: generate PNG images in bulk and optionally upload them through Roblox Open Cloud
- `RenderDoc`: import RDC captures, aggregate textures across captures, and inspect mesh / material / texture state

Other app features:

- Mesh preview support for model assets, with shaded / lit-clay / normals / unlit modes
- Drag-and-drop file and JSON-result importing
- Optional authenticated Roblox requests using `.ROBLOSECURITY`
- Embedded changelog and license info in release builds

## Requirements

- Go `1.25+`
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
