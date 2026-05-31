#!/usr/bin/env bash
set -euo pipefail

RUN=0
SKIP_TESTS=0
SKIP_DEPS=0

for arg in "$@"; do
  case "$arg" in
    --run|-r)
      RUN=1
      ;;
    --skip-tests)
      SKIP_TESTS=1
      ;;
    --skip-deps)
      SKIP_DEPS=1
      ;;
    --help|-h)
      cat <<'EOF'
Usage: ./build.sh [--run] [--skip-tests]

Build TheMauler for Linux using Wails.

Options:
  --run, -r       Launch build/bin/TheMauler after a successful build.
  --skip-tests    Skip go test ./... and go vet ./...
  --skip-deps     Do not try to install missing Linux/Wails dependencies.
  --help, -h      Show this help.
EOF
      exit 0
      ;;
    *)
      echo "unknown argument: $arg" >&2
      exit 2
      ;;
  esac
done

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

export PATH="$PATH:$(go env GOPATH 2>/dev/null || echo "$HOME/go")/bin"

install_linux_deps() {
  if [ "$SKIP_DEPS" -eq 1 ]; then
    return
  fi

  if ! command -v apt-get >/dev/null 2>&1; then
    echo "apt-get not found; skipping system dependency install." >&2
    return
  fi

  local sudo_cmd=()
  if [ "$(id -u)" -ne 0 ]; then
    if ! command -v sudo >/dev/null 2>&1; then
      echo "sudo was not found; install system packages manually or rerun as root." >&2
      return
    fi
    sudo_cmd=(sudo)
  fi

  echo "Installing Linux build dependencies with apt..."
  "${sudo_cmd[@]}" apt-get update -qq
  "${sudo_cmd[@]}" apt-get install -y \
    build-essential \
    pkg-config \
    libgtk-3-dev

  # Wails v2 pkg-config flags hardcode webkit2gtk-4.0.
  # Ubuntu 22.04 and earlier ship libwebkit2gtk-4.0-dev directly.
  # Ubuntu 24.04+ dropped it — install 4.1 and symlink the .pc file so
  # pkg-config resolves webkit2gtk-4.0 to the 4.1 library (API-compatible).
  if apt-cache show libwebkit2gtk-4.0-dev >/dev/null 2>&1; then
    "${sudo_cmd[@]}" apt-get install -y libwebkit2gtk-4.0-dev
  else
    echo "libwebkit2gtk-4.0-dev not available; installing 4.1 with pkg-config shim..."
    "${sudo_cmd[@]}" apt-get install -y libwebkit2gtk-4.1-dev
    local pc_src
    pc_src="$(dpkg -L libwebkit2gtk-4.1-dev 2>/dev/null | grep 'webkit2gtk-4\.1\.pc$' | head -1)"
    if [ -z "$pc_src" ]; then
      pc_src="$(find /usr/lib /usr/share -name 'webkit2gtk-4.1.pc' 2>/dev/null | head -1)"
    fi
    if [ -n "$pc_src" ]; then
      local pc_dst="${pc_src/webkit2gtk-4.1.pc/webkit2gtk-4.0.pc}"
      if [ ! -f "$pc_dst" ]; then
        "${sudo_cmd[@]}" ln -sf "$pc_src" "$pc_dst"
        echo "Symlinked webkit2gtk-4.1.pc → webkit2gtk-4.0.pc"
      fi
    else
      echo "WARNING: could not locate webkit2gtk-4.1.pc — build may fail." >&2
    fi
  fi
}

if ! command -v go >/dev/null 2>&1; then
  echo "go was not found on PATH. Install Go 1.26+ first." >&2
  exit 1
fi

export PATH="$PATH:$(go env GOPATH)/bin"

if ! command -v npm >/dev/null 2>&1; then
  echo "npm was not found on PATH. Install Node.js/npm first." >&2
  exit 1
fi

install_linux_deps

if ! command -v wails >/dev/null 2>&1; then
  echo "wails CLI was not found on PATH; installing it with go install..."
  go install github.com/wailsapp/wails/v2/cmd/wails@latest
  export PATH="$PATH:$(go env GOPATH)/bin"
fi

if ! command -v wails >/dev/null 2>&1; then
  echo "wails CLI install completed but wails is still not on PATH." >&2
  echo "Try: export PATH=\"\$PATH:$(go env GOPATH)/bin\"" >&2
  exit 1
fi

(cd frontend && npm install)

if [ "$SKIP_TESTS" -eq 0 ]; then
  go test ./...
  go vet ./...
fi

(cd frontend && npm run build)

# Remove plain go-build binaries that are easy to confuse with Wails output.
rm -f "$ROOT/mauler" "$ROOT/mauler.exe"

if command -v pkill >/dev/null 2>&1; then
  pkill -x TheMauler >/dev/null 2>&1 || true
fi

wails build -clean

BIN="$ROOT/build/bin/TheMauler"
echo "Built $BIN"

if [ "$RUN" -eq 1 ]; then
  WEBKIT_DISABLE_COMPOSITING_MODE=1 LIBGL_ALWAYS_SOFTWARE=1 "$BIN" >/dev/null 2>&1 &
fi
