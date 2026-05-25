#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TARGET="${1:-all}"

embed_windows_resources() {
  if [ "$(go env GOOS)" != "windows" ]; then
    return
  fi
  if ! command -v go-winres &>/dev/null; then
    echo "Installing go-winres..."
    go install github.com/tc-hib/go-winres@latest
  fi
  echo "Embedding Windows icon and manifest..."
  (cd "$ROOT_DIR/cmd/joxblox" && go-winres make --in winres/winres.json)
}

# build_fyne builds the legacy Fyne desktop binary (./cmd/joxblox). The app is
# being retired in favour of the Wails web app; this target is kept so the old
# UI still compiles/runs during the migration.
build_fyne() {
  echo "Building legacy Fyne app..."
  embed_windows_resources
  LDFLAGS=""
  if [ "$(go env GOOS)" = "windows" ]; then
    LDFLAGS="-H windowsgui"
  fi
  (cd "$ROOT_DIR" && go build -ldflags "${LDFLAGS}" ./cmd/joxblox)
}

# webapp_assets_dir holds the native helper binaries embedded into release-tag
# Wails builds (currently just the Rust extractor; the mesh-renderer side binary
# isn't wired into any web tab yet).
WEBAPP_ASSETS_DIR="$ROOT_DIR/internal/webapp/release-assets"

# populate_webapp_assets builds the Rust extractor and copies it into the webapp
# embed directory so a `-tags release` Wails build bundles it.
populate_webapp_assets() {
  local goexe
  goexe="$(go env GOEXE)"
  mkdir -p "$WEBAPP_ASSETS_DIR"
  echo "Building Rust extractor for webapp embed..."
  (cd "$ROOT_DIR/tools/rbxl-id-extractor" && cargo build --release)
  cp "$ROOT_DIR/tools/rbxl-id-extractor/target/release/joxblox-rusty-asset-tool${goexe}" \
    "$WEBAPP_ASSETS_DIR/rbxl-id-extractor.bin"
}

build_rust() {
  echo "Building Rust project..."
  (cd "$ROOT_DIR/tools/rbxl-id-extractor" && cargo build --release)
}

build_mesh_renderer() {
  echo "Building mesh renderer..."
  (cd "$ROOT_DIR/tools/mesh-renderer" && go build -o "$ROOT_DIR/joxblox-mesh-renderer$(go env GOEXE)" .)
}

resolve_wails_bin() {
  local wails_bin
  wails_bin="$(command -v wails || true)"
  if [ -z "$wails_bin" ]; then
    wails_bin="$(go env GOPATH)/bin/wails"
  fi
  if [ ! -x "$wails_bin" ]; then
    echo "wails CLI not found. Install with:" >&2
    echo "  go install github.com/wailsapp/wails/v2/cmd/wails@latest" >&2
    exit 1
  fi
  printf '%s\n' "$wails_bin"
}

build_wails() {
  echo "Building Wails (web-rendered) app..."
  local wails_bin
  wails_bin="$(resolve_wails_bin)"
  populate_webapp_assets
  # -tags release makes the webapp embed the bundled Rust extractor so the
  # shipped app extracts without a source tree / sidecar binary. wails build
  # runs the frontend build (npm install + npm run build) itself.
  (cd "$ROOT_DIR" && "$wails_bin" build -tags release)
}

dev_wails() {
  echo "Starting Wails dev server (hot-reload)..."
  local wails_bin
  wails_bin="$(resolve_wails_bin)"
  (cd "$ROOT_DIR" && "$wails_bin" dev)
}

build_release() {
  : "${VERSION_NAME:?VERSION_NAME must be set for release builds (e.g. VERSION_NAME=v1.2.3 ./build.sh release)}"

  local goos goarch goexe
  goos="$(go env GOOS)"
  goarch="$(go env GOARCH)"
  goexe="$(go env GOEXE)"

  local release_assets_dir="$ROOT_DIR/internal/app/release-assets"
  local dist_dir="$ROOT_DIR/dist"
  mkdir -p "$release_assets_dir" "$dist_dir"

  echo "Building Rust extractor..."
  (cd "$ROOT_DIR/tools/rbxl-id-extractor" && cargo build --release)
  cp "$ROOT_DIR/tools/rbxl-id-extractor/target/release/joxblox-rusty-asset-tool${goexe}" \
    "$release_assets_dir/rbxl-id-extractor.bin"

  echo "Building mesh renderer into release assets..."
  (cd "$ROOT_DIR/tools/mesh-renderer" && go build -o "$release_assets_dir/joxblox-mesh-renderer.bin" .)

  cp "$ROOT_DIR/CHANGELOG.md" "$release_assets_dir/CHANGELOG.md"
  cp "$ROOT_DIR/LICENSE.md" "$release_assets_dir/LICENSE.md"

  embed_windows_resources

  local safe_version ldflags output_rel
  safe_version="${VERSION_NAME//\//-}"
  output_rel="dist/joxblox-${safe_version}-${goos}-${goarch}${goexe}"
  ldflags="-X joxblox/internal/app.appVersion=${VERSION_NAME}"
  if [ "$goos" = "windows" ]; then
    ldflags="${ldflags} -H windowsgui"
  fi

  echo "Building release binary -> ${output_rel}"
  (cd "$ROOT_DIR" && go build -v -tags release -trimpath -ldflags "${ldflags}" -o "${output_rel}" ./cmd/joxblox)
  printf '%s\n' "${output_rel}" > "$dist_dir/.last-release-path"
}

case "$TARGET" in
  fyne | go)
    # Legacy Fyne desktop app (`go` kept as an alias for muscle memory).
    build_fyne
    build_mesh_renderer
    ;;
  rust)
    build_rust
    ;;
  mesh-renderer)
    build_mesh_renderer
    ;;
  wails)
    build_wails
    ;;
  dev)
    dev_wails
    ;;
  release)
    # NOTE: still builds the legacy Fyne binary (prepare-release.sh + CI expect
    # that artifact shape). Migrating release packaging to the Wails app bundle
    # is a separate follow-up.
    build_release
    ;;
  all)
    # Default: build & bundle the shippable Wails web app.
    build_wails
    ;;
  *)
    echo "Usage: $0 [wails|fyne|rust|mesh-renderer|dev|release|all]"
    exit 1
    ;;
esac
