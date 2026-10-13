#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
ARCH=${1:-arm64}
case "$ARCH" in arm64) MACH=arm64 ;; x64) MACH=x86_64 ;; *) echo 'Expected arm64 or x64' >&2; exit 1 ;; esac
VERSION=$(node -p "require('$ROOT/desktop/package.json').version")
APP="$ROOT/desktop/.bundle/cua-helper/EasyAgent Computer Use.app"
mkdir -p "$APP/Contents/MacOS"
xcrun swiftc -O -target "$MACH-apple-macosx14.0" -sdk "$(xcrun --sdk macosx --show-sdk-path)" \
  "$ROOT/native/computer-use/main.swift" -o "$APP/Contents/MacOS/easyagent-cua-helper"
python3 - "$APP" "$VERSION" <<'PY'
import plistlib, pathlib, sys
app, version = pathlib.Path(sys.argv[1]), sys.argv[2]
(app/'Contents/Info.plist').write_bytes(plistlib.dumps({
 'CFBundleIdentifier':'com.easyagent.cua-helper', 'CFBundleName':'EasyAgent Computer Use',
 'CFBundleDisplayName':'EasyAgent Computer Use', 'CFBundleExecutable':'easyagent-cua-helper',
 'CFBundlePackageType':'APPL', 'CFBundleShortVersionString':version, 'CFBundleVersion':version,
 'LSMinimumSystemVersion':'14.0', 'LSUIElement':True,
}))
PY
codesign --force --sign - --identifier com.easyagent.cua-helper "$APP"
python3 "$ROOT/scripts/test_cua_helper.py" "$APP/Contents/MacOS/easyagent-cua-helper"
