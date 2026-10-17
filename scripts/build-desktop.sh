#!/usr/bin/env bash
# Build a self-contained macOS desktop app without replacing any installed core.
# Usage: scripts/build-desktop.sh [arm64|x64|--x64] [release-tag]
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
case "${1:-$(node -p process.arch)}" in
  arm64|--arm64) ARCH=arm64; GO_ARCH=arm64 ;;
  x64|--x64|amd64) ARCH=x64; GO_ARCH=amd64 ;;
  *) echo 'Expected arm64 or x64' >&2; exit 1 ;;
esac
[[ $(uname -s) == Darwin ]] || { echo 'macOS packaging requires a macOS build host' >&2; exit 1; }
cd "$PROJECT_ROOT"
BUNDLE="$PROJECT_ROOT/desktop/.bundle"
VERSION=$(node -e 'const p=require("./desktop/package.json"),l=require("./desktop/package-lock.json");if(p.version!==l.version||p.version!==l.packages[""].version)throw Error("Desktop package and lock versions differ");process.stdout.write(p.version)')
TAG=${2:-}
python3 - "$VERSION" <<'PY'
import sys
sys.path.insert(0, "scripts")
from release import version_key
version_key("v" + sys.argv[1])
PY
if [[ -n "$TAG" ]]; then
  [[ "$TAG" == "v$VERSION" ]] || { echo 'Release tag must match desktop package version' >&2; exit 1; }
  [[ $(node -p process.arch) == "$ARCH" ]] || { echo 'Release installers must be built and verified on their native architecture' >&2; exit 1; }
  python3 scripts/release.py validate "$TAG"
fi
OUTPUT="$PROJECT_ROOT/desktop/release.noindex/$VERSION/$ARCH"
mkdir -p "$BUNDLE"
bash "$SCRIPT_DIR/build-cua-helper.sh" "$ARCH"
CGO_ENABLED=0 GOOS=darwin GOARCH="$GO_ARCH" go build -trimpath \
  -ldflags "-s -w -X main.version=v$VERSION" -o "$BUNDLE/easyagent" ./cmd/easyagent
cleanup() {
  local status=0
  python3 "$SCRIPT_DIR/unregister-desktop-apps.py" \
    "$OUTPUT"/*/EasyAgent.app \
    "$PROJECT_ROOT/desktop/node_modules/electron/dist/Electron.app" \
    "$BUNDLE/cua-helper/EasyAgent Computer Use.app" || status=$?
  return "$status"
}
trap cleanup EXIT
chmod 755 "$BUNDLE/easyagent"
cd "$PROJECT_ROOT/desktop"
npm ci --ignore-scripts
npm run typecheck
npm test
if [[ ! -f node_modules/electron/path.txt ]]; then node node_modules/electron/install.js; fi
npm run "electron:build:$ARCH" -- --config.directories.output="$OUTPUT"
cd "$PROJECT_ROOT"
bash scripts/check-desktop-package.sh "$ARCH" "$TAG" "$OUTPUT"
echo "Desktop package: $OUTPUT ($ARCH)"
