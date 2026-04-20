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

build_go() {
  echo "Building Go app..."
  embed_windows_resources
  LDFLAGS=""
  if [ "$(go env GOOS)" = "windows" ]; then
    LDFLAGS="-H windowsgui"
  fi
  (cd "$ROOT_DIR" && go build -ldflags "${LDFLAGS}" ./cmd/joxblox)
}

build_rust() {
  echo "Building Rust project..."
  (cd "$ROOT_DIR/tools/rbxl-id-extractor" && cargo build --release)
}

build_mesh_renderer() {
  echo "Building mesh renderer..."
  (cd "$ROOT_DIR/tools/mesh-renderer" && go build -o "$ROOT_DIR/joxblox-mesh-renderer$(go env GOEXE)" .)
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
  go)
    build_go
    build_mesh_renderer
    ;;
  rust)
    build_rust
    ;;
  mesh-renderer)
    build_mesh_renderer
    ;;
  release)
    build_release
    ;;
  all)
    build_go
    build_rust
    build_mesh_renderer
    ;;
  *)
    echo "Usage: $0 [go|rust|mesh-renderer|release|all]"
    exit 1
    ;;
esac
