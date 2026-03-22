#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TARGET="${1:-all}"

build_go() {
  echo "Building Go project..."
  (cd "$ROOT_DIR" && go build ./cmd/joxblox)
}

build_rust() {
  echo "Building Rust project..."
  (cd "$ROOT_DIR/tools/rbxl-id-extractor" && cargo build --release)
}

case "$TARGET" in
  go)
    build_go
    ;;
  rust)
    build_rust
    ;;
  all)
    build_go
    build_rust
    ;;
  *)
    echo "Usage: $0 [go|rust|all]"
    exit 1
    ;;
esac
