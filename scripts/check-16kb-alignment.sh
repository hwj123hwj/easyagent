#!/usr/bin/env bash
#
# check-16kb-alignment.sh — Verify APK .so files are 16KB page-size aligned
#
# Usage: ./scripts/check-16kb-alignment.sh [path/to/app.apk]
#
# RN Best Practice: native-android-16kb-alignment (CRITICAL)
# Google Play requires 16KB page size support for apps targeting Android 15+.
# This script should be run after every release APK build.
#
set -euo pipefail

ZIPALIGN="${ZIPALIGN:-$(command -v zipalign || find /home/q/android-sdk/build-tools -name zipalign 2>/dev/null | sort -V | tail -1)}"

if [ -z "$ZIPALIGN" ]; then
  echo "ERROR: zipalign not found. Set ZIPALIGN env or install Android build-tools."
  exit 1
fi

APK="${1:-desktop/android/app/build/outputs/apk/release/app-release.apk}"

if [ ! -f "$APK" ]; then
  echo "ERROR: APK not found at $APK"
  echo "Build one first: bash scripts/build-android.sh release"
  exit 1
fi

echo "Checking 16KB alignment: $APK"
echo "Using zipalign: $ZIPALIGN"
echo "---"

OUTPUT=$("$ZIPALIGN" -c -P 16 -v 4 "$APK" 2>&1) || true

# Check for failures
FAILURES=$(echo "$OUTPUT" | grep -i "fail\|misalign" || true)
SO_FILES=$(echo "$OUTPUT" | grep "\.so " || true)
SO_FAILED=$(echo "$SO_FILES" | grep -v "OK" || true)

if [ -n "$SO_FAILED" ]; then
  echo "❌ MISALIGNED .so files found:"
  echo "$SO_FAILED"
  echo ""
  echo "These libraries need updating to support 16KB page sizes."
  exit 1
fi

echo "✅ All .so files are 16KB page-size aligned."
SO_COUNT=$(echo "$SO_FILES" | wc -l)
echo "   Checked $SO_COUNT native libraries."
echo "   Verification: $(echo "$OUTPUT" | tail -1)"
