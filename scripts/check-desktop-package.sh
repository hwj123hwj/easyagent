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
# LaunchServices/TCC may still hold an installer briefly after inspection.
# Retry normal detach only; never force-unmount a volume used by another process.
detach_installer() {
  local attempt
  for attempt in 1 2 3 4 5; do
    if hdiutil detach "$1" >/dev/null; then return 0; fi
    if [[ "$attempt" != 5 ]]; then sleep "$attempt"; fi
  done
  echo "Installer is still busy; retained for inspection: $1" >&2
  return 1
}
cleanup() {
  local status=0
  if [[ "$MOUNTED" == 1 ]]; then
    python3 "$SCRIPT_DIR/unregister-desktop-apps.py" "$STAGE/mount/EasyAgent.app" || status=$?
    detach_installer "$STAGE/mount" || return 1
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
[[ "$signature" == *"Identifier=com.easyagent.desktop"* \
   && "$signature" == *"Sealed Resources version=2"* && "$signature" != *"Info.plist=not bound"* ]] \
  || { echo 'Installer lacks a complete app signature and resource seal' >&2; exit 1; }
if [[ $(node desktop/scripts/macos-signing.cjs mode) == adhoc ]]; then
  [[ "$signature" == *"Signature=adhoc"* ]] || { echo 'Expected an ad hoc signature' >&2; exit 1; }
fi
node desktop/scripts/macos-signing.cjs verify "$APP" com.easyagent.desktop
HELPER_APP="$RESOURCES/cua-helper/EasyAgent Computer Use.app"
HELPER="$HELPER_APP/Contents/MacOS/easyagent-cua-helper"
[[ $(plutil -extract CFBundleIdentifier raw -o - "$HELPER_APP/Contents/Info.plist") == com.easyagent.cua-helper ]]
[[ $(plutil -extract CFBundleShortVersionString raw -o - "$HELPER_APP/Contents/Info.plist") == "$VERSION" ]]
codesign --verify --deep --strict "$HELPER_APP"
node desktop/scripts/macos-signing.cjs verify "$HELPER_APP" com.easyagent.cua-helper
for binary in "$HELPER" "$APP/Contents/MacOS/EasyAgent" "$RESOURCES/easyagent"; do
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
for (const file of ['scripts/macos-signing.cjs','scripts/sign-adhoc.cjs','scripts/desktop-update.cjs',
  'scripts/desktop-update-worker.cjs','scripts/install-desktop-update.cjs','dist/electron/desktop-updater.js',
  'node_modules/@electron/osx-sign/package.json']) {
  if (!extractFile(process.argv[2],file).length) throw Error('Installer lacks the fixed-identity update runtime: '+file);
}
JS
if [[ $(node -p process.arch) == "$ARCH" ]]; then
  python3 scripts/test_cua_helper.py "$HELPER" --no-tcc
  [[ $("$RESOURCES/easyagent" --version) == "easyagent v$VERSION" ]]
fi
echo "Verified installer: EasyAgent-$VERSION-$ARCH.dmg at $REVISION"
