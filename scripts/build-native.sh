#!/usr/bin/env bash
# Build the MyGo native desktop client and stage its installer artifact.
# Usage: scripts/build-native.sh darwin-arm64 [dist-dir] [release-tag]
# Only darwin-arm64 produces a release artifact; Linux packaging still ships
# through the native-preview tag channel until its packaging lands in-repo.
set -euo pipefail
TARGET=${1:?Usage: build-native.sh darwin-arm64 [dist-dir] [release-tag]}
DIST=${2:-}
TAG=${3:-}
[[ "$TARGET" == "darwin-arm64" ]] || { echo "Unsupported native target: $TARGET (only darwin-arm64)" >&2; exit 1; }
[[ $(uname -s) == Darwin && $(uname -m) == arm64 ]] || { echo 'Native darwin-arm64 packaging requires an Apple Silicon macOS host' >&2; exit 1; }

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
if [[ -n "$DIST" && "$DIST" != /* ]]; then DIST="$ROOT/$DIST"; fi
if [[ -n "$TAG" ]]; then
  cd "$ROOT"
  python3 scripts/release.py validate "$TAG"
  VERSION=$(python3 -c 'import json; print(json.load(open("desktop-native/mygo.json"))["version"])')
  [[ "$VERSION" == "${TAG#v}" ]] || { echo 'Native version does not match release tag' >&2; exit 1; }
  [[ -n "${MYGO_UPDATER_PRIVATE_KEY:-}" ]] || { echo "Native releases require MYGO_UPDATER_PRIVATE_KEY" >&2; exit 1; }
  export EASYAGENT_BUILD_VERSION="$TAG"
fi
cd "$ROOT/desktop-native"
go run ./tools/build

APP=$(find build.noindex/darwin-arm64 -maxdepth 1 -name '*.app' | head -1)
[[ -n "$APP" && -d "$APP" ]] || { echo 'MyGo build did not produce an .app bundle' >&2; exit 1; }
# Clean up registration even when validation fails; never launch the GUI here.
trap 'python3 "$ROOT/scripts/unregister-desktop-apps.py" "$APP" || true' EXIT

DMG=$(find build.noindex/darwin-arm64 -maxdepth 1 -name '*.dmg' | head -1)
[[ -n "$DMG" && -s "$DMG" ]] || { echo 'MyGo build did not produce a DMG' >&2; exit 1; }

# Built bundles must carry the promoted identity before they can ship.
plutil -extract CFBundleIdentifier raw -o - "$APP/Contents/Info.plist" | grep -qx 'com.hwj.easyagent' \
  || { echo 'Native app bundle identifier is not com.hwj.easyagent' >&2; exit 1; }

ICON=$(plutil -extract CFBundleIconFile raw -o - "$APP/Contents/Info.plist")
[[ -n "$ICON" && -s "$APP/Contents/Resources/$ICON" ]] \
  || { echo 'Native app is missing its Dock icon' >&2; exit 1; }

if [[ -n "$TAG" ]]; then
  CORE="$APP/Contents/Resources/easyagent"
  [[ $("$CORE" --version) == "easyagent $TAG" ]] || { echo 'Bundled core version does not match tag' >&2; exit 1; }
  REVISION=$(git rev-parse HEAD)
  METADATA=$(go version -m "$CORE")
  [[ "$METADATA" == *"vcs.revision=$REVISION"* && "$METADATA" == *"vcs.modified=false"* ]] \
    || { echo 'Bundled core source metadata does not match clean release revision' >&2; exit 1; }
fi

# Exercise bundle discovery AND the actual local-backend launcher with isolated
# configuration. A healthy Resources binary alone does not catch UI recursion.
EASYAGENT_TEST_BUNDLE="$PWD/$APP" go test ./... -count=1

if [[ -n "$DIST" ]]; then
  mkdir -p "$DIST"
  cp "$DMG" "$DIST/easyagent-native-darwin-arm64.dmg"
  if [[ -n "$TAG" ]]; then
    cp "build.noindex/$TARGET/update-$TARGET.json" "$DIST/"
    cp "build.noindex/$TARGET/easyagent-${TAG#v}-$TARGET.tar.gz" "$DIST/"
    go run ./tools/verify-update mygo.json "$DIST" "${TAG#v}"
  fi
  echo "Native artifact: $DIST/easyagent-native-darwin-arm64.dmg"
else
  echo "Native app: $APP"
  echo "Native DMG: $DMG"
fi
