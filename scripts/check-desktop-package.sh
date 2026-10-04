#!/usr/bin/env bash
# Inspect the installer itself without launching the desktop app or a service.
# Usage: scripts/check-desktop-package.sh <arm64|x64> [release-tag] [output-dir]
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"
case "${1:-}" in
  arm64) ARCH=arm64; MACH_ARCH=arm64; GO_ARCH=arm64 ;;
  x64) ARCH=x64; MACH_ARCH=x86_64; GO_ARCH=amd64 ;;
  *) echo 'Expected arm64 or x64' >&2; exit 1 ;;
esac
[[ $(uname -s) == Darwin ]] || { echo 'Installer inspection requires macOS' >&2; exit 1; }
VERSION=$(node -p 'require("./desktop/package.json").version')
TAG=${2:-}
OUTPUT=${3:-"$PROJECT_ROOT/desktop/release.noindex/$VERSION/$ARCH"}
REVISION=$(git rev-parse HEAD)
if [[ -n "$TAG" ]]; then
  [[ "$TAG" == "v$VERSION" ]] || { echo 'Release tag must match desktop package version' >&2; exit 1; }
  [[ $(node -p process.arch) == "$ARCH" ]] || { echo 'Release verification requires its native architecture' >&2; exit 1; }
  python3 scripts/release.py validate "$TAG"
fi
DMG="$OUTPUT/EasyAgent-$VERSION-$ARCH.dmg"
[[ -f "$DMG" && ! -L "$DMG" && -s "$DMG" ]] || { echo "Missing installer: $DMG" >&2; exit 1; }
hdiutil verify "$DMG"
STAGE=$(python3 -c 'import tempfile; print(tempfile.mkdtemp(prefix="easyagent-installer-check.", suffix=".noindex"))')
MOUNTED=0
cleanup() {
  local status=0
  if [[ "$MOUNTED" == 1 ]]; then
    python3 "$SCRIPT_DIR/unregister-desktop-apps.py" "$STAGE/mount/EasyAgent.app" || status=$?
    hdiutil detach "$STAGE/mount" >/dev/null || return 1
  fi
  rm -rf "$STAGE"
  return "$status"
}
trap cleanup EXIT
mkdir "$STAGE/mount"
hdiutil attach -readonly -nobrowse -mountpoint "$STAGE/mount" "$DMG" >/dev/null
MOUNTED=1
APP="$STAGE/mount/EasyAgent.app"
RESOURCES="$APP/Contents/Resources"
[[ $(plutil -extract CFBundleIdentifier raw -o - "$APP/Contents/Info.plist") == com.easyagent.desktop ]]
codesign --verify --deep --strict --verbose=2 "$APP"
signature=$(codesign --display --verbose=4 "$APP" 2>&1)
[[ "$signature" == *"Identifier=com.easyagent.desktop"* && "$signature" == *"Signature=adhoc"* \
   && "$signature" == *"Sealed Resources version=2"* && "$signature" != *"Info.plist=not bound"* ]] \
  || { echo 'Installer lacks a complete ad hoc app signature and resource seal' >&2; exit 1; }
for binary in "$APP/Contents/MacOS/EasyAgent" "$RESOURCES/easyagent" "$RESOURCES/runtime/bin/node"; do
  [[ -f "$binary" && -x "$binary" && $(lipo -archs "$binary") == "$MACH_ARCH" ]] \
    || { echo "Installer has an invalid $ARCH executable: $binary" >&2; exit 1; }
done
metadata=$(go version -m "$RESOURCES/easyagent")
[[ "$metadata" == *"GOOS=darwin"* && "$metadata" == *"GOARCH=$GO_ARCH"* && "$metadata" == *"vcs.revision=$REVISION"* ]] \
  || { echo 'Bundled core does not match this source revision and architecture' >&2; exit 1; }
if [[ -n "$TAG" ]]; then
  [[ "$metadata" == *"vcs.modified=false"* ]] || { echo 'Release core was built from modified sources' >&2; exit 1; }
fi
node - "$RESOURCES/app.asar" "$VERSION" <<'JS'
const {extractFile}=require('./desktop/node_modules/@electron/asar');
const manifest=JSON.parse(extractFile(process.argv[2],'package.json').toString());
if(manifest.version!==process.argv[3])throw Error('Installed app version does not match the release');
JS
for license in Node-LICENSE ZCODE-LICENSE ZCODE-NOTICE.md ZCODE-THIRD-PARTY-NOTICES.md SOURCE.md; do
  [[ -s "$RESOURCES/licenses/$license" ]] || { echo "Missing runtime license: $license" >&2; exit 1; }
done
[[ -s "$RESOURCES/workflow-runtime.mjs" ]]
if [[ $(node -p process.arch) == "$ARCH" ]]; then
  [[ $("$RESOURCES/easyagent" --version) == "easyagent v$VERSION" ]]
  "$RESOURCES/runtime/bin/node" -e 'if(Number(process.versions.node.split(".")[0])<22)throw Error("Unsupported bundled Node version")'
  "$RESOURCES/runtime/bin/node" "$RESOURCES/workflow-runtime.mjs" --check
fi
echo "Verified installer: EasyAgent-$VERSION-$ARCH.dmg at $REVISION"
