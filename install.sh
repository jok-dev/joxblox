#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GOEXE="$(go env GOEXE 2>/dev/null || echo "")"

is_windows=0
case "$(uname -s 2>/dev/null || echo)" in
  MINGW*|MSYS*|CYGWIN*) is_windows=1 ;;
esac

if [ "$is_windows" -eq 1 ]; then
  win_base="${LOCALAPPDATA:-$USERPROFILE/AppData/Local}"
  if command -v cygpath >/dev/null 2>&1; then
    win_base="$(cygpath -u "$win_base")"
  fi
  DEFAULT_INSTALL_DIR="$win_base/joxblox/bin"
else
  DEFAULT_INSTALL_DIR="$HOME/.local/bin"
fi
INSTALL_DIR="${INSTALL_DIR:-$DEFAULT_INSTALL_DIR}"

mkdir -p "$INSTALL_DIR"

declare -a SOURCES=(
  "$ROOT_DIR/joxblox${GOEXE}"
  "$ROOT_DIR/joxblox-mesh-renderer${GOEXE}"
  "$ROOT_DIR/tools/rbxl-id-extractor/target/release/joxblox-rusty-asset-tool${GOEXE}"
)

missing=0
for src in "${SOURCES[@]}"; do
  if [ ! -f "$src" ]; then
    echo "Missing: $src"
    missing=1
  fi
done
if [ "$missing" -ne 0 ]; then
  echo
  echo "Run ./build.sh first to produce the binaries."
  exit 1
fi

for src in "${SOURCES[@]}"; do
  name="$(basename "$src")"
  dest="$INSTALL_DIR/$name"
  cp "$src" "$dest"
  chmod +x "$dest" 2>/dev/null || true
  echo "Installed: $dest"
done

if [ "$is_windows" -eq 1 ]; then
  install_dir_win="$INSTALL_DIR"
  if command -v cygpath >/dev/null 2>&1; then
    install_dir_win="$(cygpath -w "$INSTALL_DIR")"
  fi

  user_path="$(powershell.exe -NoProfile -Command \
    "[Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment').GetValue('Path','','DoNotExpandEnvironmentNames')" \
    2>/dev/null | tr -d '\r' | tr -d '\n' || true)"

  case ";${user_path};" in
    *";${install_dir_win};"*)
      echo
      echo "$install_dir_win is already on your User PATH."
      ;;
    *)
      echo
      echo "$install_dir_win is NOT on your User PATH."
      if [ -n "$user_path" ]; then
        export JOXBLOX_NEW_PATH="${install_dir_win};${user_path}"
      else
        export JOXBLOX_NEW_PATH="${install_dir_win}"
      fi
      powershell.exe -NoProfile -Command \
        "\$key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', \$true); \
         if (\$null -eq \$key) { \$key = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey('Environment') }; \
         \$kind = try { \$key.GetValueKind('Path') } catch { [Microsoft.Win32.RegistryValueKind]::ExpandString }; \
         \$key.SetValue('Path', \$env:JOXBLOX_NEW_PATH, \$kind); \
         \$key.Close()"
      echo "Added $install_dir_win to your User PATH."
      echo "Open a new terminal (or sign out/in) to pick it up."
      ;;
  esac
else
  case ":$PATH:" in
    *":$INSTALL_DIR:"*)
      echo
      echo "$INSTALL_DIR is already on PATH."
      ;;
    *)
      shell_name="$(basename "${SHELL:-}")"
      case "$shell_name" in
        zsh)  rc_file="$HOME/.zshrc" ;;
        bash) rc_file="$HOME/.bashrc" ;;
        *)    rc_file="" ;;
      esac

      line="export PATH=\"$INSTALL_DIR:\$PATH\""
      echo
      echo "$INSTALL_DIR is NOT on PATH."
      if [ -n "$rc_file" ] && ! grep -Fqs "$line" "$rc_file"; then
        printf '\n# Added by joxblox install.sh\n%s\n' "$line" >> "$rc_file"
        echo "Appended PATH export to $rc_file"
        echo "Run: source $rc_file"
      else
        echo "Add this line to your shell rc file:"
        echo "  $line"
      fi
      ;;
  esac
fi
