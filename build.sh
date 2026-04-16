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
  all)
    build_go
    build_rust
    build_mesh_renderer
    ;;
  *)
    echo "Usage: $0 [go|rust|mesh-renderer|all]"
    exit 1
    ;;
esac
