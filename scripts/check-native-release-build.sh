#!/usr/bin/env bash
# Exercise the native release/tag contract in an isolated checkout; never publish.
set -euo pipefail
root=$(git rev-parse --show-toplevel)
fixture=$(python3 -c 'import tempfile; print(tempfile.mkdtemp(prefix="easyagent-native-release.", suffix=".noindex"))')
trap 'rm -rf "$fixture"' EXIT
git clone --quiet --no-local "$root" "$fixture/repo"
cd "$fixture/repo"
git config user.name 'Release fixture'
git config user.email 'release-test@example.invalid'
version=v999.0.0-rc.1
(cd desktop-native && go run github.com/egoist/mygo/cmd/mygo keygen -o "$fixture/keys")
export MYGO_UPDATER_PRIVATE_KEY=$(cat "$fixture/keys/mygo-update.key")
export EASYAGENT_TEST_PUBLIC_KEY=$(cat "$fixture/keys/mygo-update.pub")
python3 - "$version" <<'PY'
import json, pathlib, sys, os
version = sys.argv[1]
p = pathlib.Path('desktop-native/mygo.json')
config = json.loads(p.read_text())
config['version'] = version[1:]
config['updates']['publicKey'] = os.environ['EASYAGENT_TEST_PUBLIC_KEY']
p.write_text(json.dumps(config, indent=2) + '\n')
p = pathlib.Path('CHANGELOG.md')
p.write_text(p.read_text() + f'\n## [{version}]\n\nDisposable native release verification.\n')
PY
git add -- CHANGELOG.md desktop-native/mygo.json
git -c commit.gpgsign=false commit --quiet -m 'test: local native release fixture'
git update-ref refs/remotes/origin/main HEAD
git -c tag.gpgsign=false tag -a "$version" -m 'Local test only; never publish'
bash scripts/build-native.sh darwin-arm64 dist "$version"
test -s dist/easyagent-native-darwin-arm64.dmg
"desktop-native/build.noindex/darwin-arm64/EasyAgent.app/Contents/Resources/easyagent" --version | grep -Fx "easyagent $version"
