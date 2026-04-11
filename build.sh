#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TARGET="${1:-all}"

build_go() {
  echo "Building Go app..."
  (cd "$ROOT_DIR" && go build ./cmd/joxblox)
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
