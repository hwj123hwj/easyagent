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
node -e 'if(Number(process.versions.node.split(".")[0])<22)throw Error("Node.js 22+ is required to build workflow runtime")'
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
OUTPUT="$PROJECT_ROOT/desktop/release/$VERSION/$ARCH"
mkdir -p "$BUNDLE/licenses"
CGO_ENABLED=0 GOOS=darwin GOARCH="$GO_ARCH" go build -trimpath \
  -ldflags "-s -w -X main.version=v$VERSION" -o "$BUNDLE/easyagent" ./cmd/easyagent
(cd workflow-runtime && npm ci --ignore-scripts && npm run build && npm test)
cp workflow-runtime/output/workflow-runtime.mjs "$BUNDLE/workflow-runtime.mjs"
cp workflow-runtime/vendor/ZCODE-LICENSE workflow-runtime/vendor/ZCODE-NOTICE.md \
  workflow-runtime/vendor/ZCODE-THIRD-PARTY-NOTICES.md workflow-runtime/vendor/SOURCE.md "$BUNDLE/licenses/"
# Homebrew node links local dylibs; verify an official portable distribution.
STAGE=$(mktemp -d "${TMPDIR:-/tmp}/easyagent-desktop-node.XXXXXX")
trap 'rm -rf "$STAGE"' EXIT
curl --fail --silent --show-error --location --proto '=https' https://nodejs.org/dist/index.json -o "$STAGE/index.json"
NODE_VERSION=${EA_DESKTOP_NODE_VERSION:-$(node -e 'const rows=require(process.argv[1]);const v=rows.find(r=>r.lts&&Number(r.version.slice(1).split(".")[0])>=22);if(!v)throw Error("No supported Node LTS found");process.stdout.write(v.version)' "$STAGE/index.json")}
[[ "$NODE_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Invalid Node version' >&2; exit 1; }
NODE_ARCHIVE="node-$NODE_VERSION-darwin-$ARCH.tar.gz"
NODE_BASE="https://nodejs.org/dist/$NODE_VERSION"
NODE_ARCHIVE_BASE=${EA_DESKTOP_NODE_ARCHIVE_BASE:-$NODE_BASE}
[[ "$NODE_ARCHIVE_BASE" == https://* ]] || { echo 'Node archive source must use HTTPS' >&2; exit 1; }
curl --fail --silent --show-error --location --proto '=https' "$NODE_ARCHIVE_BASE/$NODE_ARCHIVE" -o "$STAGE/$NODE_ARCHIVE"
# Always verify against the official distribution, including an optional mirror.
curl --fail --silent --show-error --location --proto '=https' "$NODE_BASE/SHASUMS256.txt" -o "$STAGE/SHASUMS256.txt"
EXPECTED=$(awk -v archive="$NODE_ARCHIVE" '$2==archive {print $1}' "$STAGE/SHASUMS256.txt")
ACTUAL=$(shasum -a 256 "$STAGE/$NODE_ARCHIVE" | awk '{print $1}')
[[ -n "$EXPECTED" && "$ACTUAL" == "$EXPECTED" ]] || { echo 'Node archive checksum mismatch' >&2; exit 1; }
tar -xzf "$STAGE/$NODE_ARCHIVE" -C "$STAGE"
cp "$STAGE/node-$NODE_VERSION-darwin-$ARCH/bin/node" "$BUNDLE/node"
cp "$STAGE/node-$NODE_VERSION-darwin-$ARCH/LICENSE" "$BUNDLE/licenses/Node-LICENSE"
chmod 755 "$BUNDLE/easyagent" "$BUNDLE/node"
if [[ $(node -p process.arch) == "$ARCH" ]]; then "$BUNDLE/node" "$BUNDLE/workflow-runtime.mjs" --check; fi
cd "$PROJECT_ROOT/desktop"
npm ci --ignore-scripts
npm run typecheck
npm test
if [[ ! -f node_modules/electron/path.txt ]]; then node node_modules/electron/install.js; fi
npm run "electron:build:$ARCH" -- --config.directories.output="$OUTPUT"
cd "$PROJECT_ROOT"
bash scripts/check-desktop-package.sh "$ARCH" "$TAG" "$OUTPUT"
echo "Desktop package: $OUTPUT (Node $NODE_VERSION, $ARCH)"
