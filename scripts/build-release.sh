#!/usr/bin/env bash
# Linux release builder. Verification is performed by the required Verify job.
set -euo pipefail
version=${1:?Usage: build-release.sh vMAJOR.MINOR.PATCH}
python3 scripts/release.py validate "$version"
revision=$(git rev-parse HEAD)
# Never mix leftovers from another tag into an upload.
[[ ! -e dist ]] || { echo 'dist already exists; use a clean checkout' >&2; exit 1; }
mkdir dist
(cd workflow-runtime && npm ci --ignore-scripts && npm run build)
node workflow-runtime/output/workflow-runtime.mjs --check
cp workflow-runtime/output/workflow-runtime.mjs dist/
source_epoch=$(git show -s --format=%ct HEAD)
tar --sort=name --mtime="@$source_epoch" --owner=0 --group=0 --numeric-owner \
  -C workflow-runtime/vendor -cf - ZCODE-LICENSE ZCODE-NOTICE.md ZCODE-THIRD-PARTY-NOTICES.md SOURCE.md \
  | gzip -n > dist/workflow-runtime-licenses.tar.gz
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  for binary in easyagent easyagent-bridge; do
    CGO_ENABLED=0 GOOS=${target%/*} GOARCH=${target#*/} go build -trimpath \
      -ldflags "-s -w -X main.version=$version" \
      -o "dist/$binary-${target%/*}-${target#*/}" "./cmd/$binary"
  done
done
# Both binaries must expose the version without starting a service or reading credentials.
[[ $(dist/easyagent-linux-amd64 --version) == "easyagent $version" ]]
[[ $(dist/easyagent-bridge-linux-amd64 --version) == "easyagent-bridge $version" ]]
python3 scripts/release.py manifest "$version" "$revision" dist
(cd dist && sha256sum --check checksums.txt)
