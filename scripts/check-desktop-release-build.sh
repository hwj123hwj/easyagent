#!/usr/bin/env bash
# Build a real release-mode desktop fixture without pushing or publishing its tag.
set -euo pipefail
arch=${1:?Usage: check-desktop-release-build.sh arm64|x64}
root=$(git rev-parse --show-toplevel)
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
git clone --quiet --no-local "$root" "$fixture/repo"
cd "$fixture/repo"
git config user.name 'Release fixture'
git config user.email 'release-test@example.invalid'
version=v999.0.0-rc.1
python3 - <<'PY'
import json
from pathlib import Path
for filename in ('desktop/package.json', 'desktop/package-lock.json'):
    path = Path(filename)
    data = json.loads(path.read_text())
    data['version'] = '999.0.0-rc.1'
    if 'packages' in data:
        data['packages']['']['version'] = '999.0.0-rc.1'
    path.write_text(json.dumps(data, indent=2) + '\n')
PY
printf '\n## [%s]\n\nDisposable desktop release verification.\n' "$version" >> CHANGELOG.md
git add -- CHANGELOG.md desktop/package.json desktop/package-lock.json
git -c commit.gpgsign=false commit --quiet -m 'test: local desktop release fixture'
git update-ref refs/remotes/origin/main HEAD
git -c tag.gpgsign=false tag -a "$version" -m 'Local test only; never publish'
# The custom signer uses only ad hoc identity "-", with no certificates or secrets.
# Exercise it in PR builds too, rather than inheriting Electron's unsigned binary.
CSC_FOR_PULL_REQUEST=true bash scripts/build-desktop.sh "$arch" "$version"
