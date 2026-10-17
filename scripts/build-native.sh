#!/usr/bin/env bash
# Build the MyGo native desktop client and stage its installer artifact.
# Usage: scripts/build-native.sh darwin-arm64 [dist-dir]
# Only darwin-arm64 produces a release artifact; Linux packaging still ships
# through the native-preview tag channel until its packaging lands in-repo.
set -euo pipefail
TARGET=${1:?Usage: build-native.sh darwin-arm64 [dist-dir]}
DIST=${2:-}
[[ "$TARGET" == "darwin-arm64" ]] || { echo "Unsupported native target: $TARGET (only darwin-arm64)" >&2; exit 1; }
[[ $(uname -s) == Darwin && $(uname -m) == arm64 ]] || { echo 'Native darwin-arm64 packaging requires an Apple Silicon macOS host' >&2; exit 1; }

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT/desktop-native"
go run ./tools/build

APP=$(find build.noindex/darwin-arm64 -maxdepth 1 -name '*.app' | head -1)
[[ -n "$APP" && -d "$APP" ]] || { echo 'MyGo build did not produce an .app bundle' >&2; exit 1; }
DMG=$(find build.noindex/darwin-arm64 -maxdepth 1 -name '*.dmg' | head -1)
[[ -n "$DMG" && -s "$DMG" ]] || { echo 'MyGo build did not produce a DMG' >&2; exit 1; }

# Built bundles must carry the promoted identity before they can ship.
plutil -extract CFBundleIdentifier raw -o - "$APP/Contents/Info.plist" | grep -qx 'com.hwj.easyagent' \
  || { echo 'Native app bundle identifier is not com.hwj.easyagent' >&2; exit 1; }

# LaunchServices hygiene: the staged app is a build artifact, never registered.
python3 "$ROOT/scripts/unregister-desktop-apps.py" "$APP" || true

if [[ -n "$DIST" ]]; then
  mkdir -p "$DIST"
  cp "$DMG" "$DIST/easyagent-native-darwin-arm64.dmg"
  echo "Native artifact: $DIST/easyagent-native-darwin-arm64.dmg"
else
  echo "Native app: $APP"
  echo "Native DMG: $DMG"
fi
