#!/usr/bin/env bash
# Exercise the actual release builder in a disposable repository. Never pushes a tag.
set -euo pipefail
root=$(git rev-parse --show-toplevel)
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
git clone --quiet --no-local "$root" "$fixture/repo"
cd "$fixture/repo"
git config user.name 'Release fixture'
git config user.email 'release-test@example.invalid'
# Pick a deliberately separate local-only version for the fixture.
version=v999.0.0-rc.1
printf '\n## [%s]\n\nDisposable release build verification.\n' "$version" >> CHANGELOG.md
git add CHANGELOG.md
git -c commit.gpgsign=false commit --quiet -m 'test: local release build fixture'
git update-ref refs/remotes/origin/main HEAD
git -c tag.gpgsign=false tag -a "$version" -m 'Local test only; never publish'
bash scripts/build-release.sh "$version"
