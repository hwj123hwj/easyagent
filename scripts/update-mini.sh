#!/usr/bin/env bash
# Run as the service owner. Build in isolation; never pull/reset the development checkout.
set -Eeuo pipefail
umask 077
DEPLOY_ROOT=${EA_DEPLOY_ROOT:-"$HOME/.local/share/easyagent-deploy"}
API_URL=${EA_DEPLOY_API:-https://api.github.com/repos/hwj123hwj/easyagent}
DEPLOY_REF=${EA_DEPLOY_REF:-refs/heads/main}
BIN_PATH=${EA_DEPLOY_BIN:-"$HOME/.easyagent/bin/easyagent"}
SERVICE=${EA_DEPLOY_SERVICE:-easyagent-core.service}
BRIDGE_BIN=${EA_DEPLOY_BRIDGE_BIN:-}
BRIDGE_SERVICE=${EA_DEPLOY_BRIDGE_SERVICE:-easyagent-bridge.service}
services=("$SERVICE")
if [[ -n "$BRIDGE_BIN" ]]; then services+=("$BRIDGE_SERVICE"); fi
HEALTH_URL=${EA_DEPLOY_HEALTH:-http://192.168.5.16:8080/health}
GO_BIN=${EA_DEPLOY_GO:-go}
mkdir -p "$DEPLOY_ROOT/releases" "$(dirname "$BIN_PATH")"
exec 9>"$DEPLOY_ROOT/update.lock"
flock -n 9 || exit 0
branch=${DEPLOY_REF#refs/heads/}
encoded_branch=$(python3 -c 'import sys,urllib.parse; print(urllib.parse.quote(sys.argv[1],safe=""))' "$branch")
revision=$(curl --fail --silent --show-error --retry 2 --connect-timeout 10 --max-time 45 "$API_URL/commits/$encoded_branch" | python3 -c 'import json,sys; print(json.load(sys.stdin)["sha"])')
[[ "$revision" =~ ^[0-9a-f]{40}$ ]] || { echo 'Invalid revision from GitHub' >&2; exit 1; }
if [[ -f "$DEPLOY_ROOT/current-revision" ]] && [[ $(cat "$DEPLOY_ROOT/current-revision") == "$revision" ]]; then
  echo "Already deployed: $revision"
  exit 0
fi
if [[ -f "$DEPLOY_ROOT/failed-revision" ]] && [[ $(cat "$DEPLOY_ROOT/failed-revision") == "$revision" ]]; then
  echo "Revision previously failed health check: $revision. Remove failed-revision to retry." >&2
  exit 1
fi
build_dir=$(mktemp -d "$DEPLOY_ROOT/build.XXXXXX")
trap 'rm -rf "$build_dir"' EXIT
curl --fail --silent --show-error --location --retry 2 --connect-timeout 10 --max-time 600 "$API_URL/tarball/$revision" -o "$build_dir/source.tar.gz"
tar -xzf "$build_dir/source.tar.gz" --strip-components=1 -C "$build_dir"
rm "$build_dir/source.tar.gz"
# Ignore inherited GOROOT; Go resolves the root belonging to the selected executable.
unset GOROOT
export CGO_ENABLED=0
export GOMAXPROCS=${GOMAXPROCS:-2}
export EA_PROVIDER=mock
export EA_ENV_FILE="$build_dir/no-runtime-config"
export EA_HOME="$build_dir/test-home"
export EA_DATA_DIR="$build_dir/test-data"
mkdir -p "$EA_HOME" "$EA_DATA_DIR"
cd "$build_dir"
workflow_runtime=false
runtime_bin="$(dirname "$BIN_PATH")/workflow-runtime.mjs"
if [[ -f workflow-runtime/package-lock.json ]]; then
  node -e 'if (Number(process.versions.node.split(".")[0]) < 22) process.exit(1)'
  (cd workflow-runtime && npm ci --ignore-scripts && npm run build && npm test)
  node workflow-runtime/output/workflow-runtime.mjs --check
  workflow_runtime=true
fi
"$GO_BIN" test -p 2 ./...
(cd third_party/bubbletea && "$GO_BIN" test -p 2 ./...)
"$GO_BIN" vet -p 2 ./...
"$GO_BIN" build -trimpath -ldflags "-X main.version=$revision" -o easyagent ./cmd/easyagent
"$GO_BIN" build -trimpath -o easyagent-bridge ./cmd/easyagent-bridge
release="$DEPLOY_ROOT/releases/$revision"
mkdir -p "$release"
install -m 755 easyagent "$release/easyagent"
if [[ "$workflow_runtime" == true ]]; then
  install -m 644 workflow-runtime/output/workflow-runtime.mjs "$release/workflow-runtime.mjs"
  cp -R workflow-runtime/vendor/ZCODE-* workflow-runtime/vendor/SOURCE.md "$release/"
fi
if [[ -n "$BRIDGE_BIN" ]]; then install -m 755 easyagent-bridge "$release/easyagent-bridge"; fi
if [[ -f scripts/web-test.mjs ]]; then node --test scripts/web-test.mjs; fi
if [[ -f scripts/test_update_mini.py ]]; then python3 scripts/test_update_mini.py; fi
# Prevent an older branch from silently removing capabilities enabled on this host.
if [[ ${EA_DEPLOY_REQUIRE_PATH_POLICY:-false} == true ]]; then
  help_text=$("$release/easyagent" --help 2>&1)
  [[ "$help_text" == *"-allow-outside-workspace"* ]] || { echo 'Release lacks required path policy switch' >&2; exit 1; }
fi
if [[ -n "$BRIDGE_BIN" && ${EA_DEPLOY_REQUIRE_OWNER_ACCESS:-false} == true ]]; then
  "$GO_BIN" tool nm "$release/easyagent-bridge" > "$build_dir/bridge-symbols"
  grep -Fq 'internal/feishu.OpenOwnerAccess' "$build_dir/bridge-symbols" &&
    grep -Fq 'internal/feishu.(*OwnerAccess).Allowed' "$build_dir/bridge-symbols" || {
      echo 'Release lacks required bridge owner access checks' >&2; exit 1;
    }
fi
# Admit deployment only while idle, and block new work until restart. An old
# server without this protocol fails closed; bootstrap upgrades are explicit.
DEPLOY_URL=${EA_DEPLOY_CONTROL:-"${HEALTH_URL%/health}/admin/deploy"}
CORE_ENV=${EA_DEPLOY_CORE_ENV:-"$HOME/.config/easyagent/core.env"}
lease_file="$build_dir/deploy.lease"
guard="$build_dir/scripts/deploy-guard.py"
if python3 "$guard" prepare "$DEPLOY_URL" "$CORE_ENV" "$lease_file"; then
  :
else
  guard_status=$?
  if [[ "$guard_status" == 75 ]]; then exit 0; fi
  exit "$guard_status"
fi
# Release our own lease on any pre-restart failure. After restart the old lease
# is gone; the best-effort release is harmless. Expiry also recovers killed updaters.
cleanup() {
  python3 "$guard" release "$DEPLOY_URL" "$CORE_ENV" "$lease_file" >/dev/null 2>&1 || true
  rm -rf "$build_dir"
}
trap cleanup EXIT
# Keep an exact copy of the previous binary even if it predates this updater.
previous="$DEPLOY_ROOT/previous-binary"
if [[ -f "$BIN_PATH" ]]; then cp -p "$BIN_PATH" "$previous"; else echo 'Existing service binary is required for rollback' >&2; exit 1; fi
if [[ -n "$BRIDGE_BIN" ]]; then
  [[ -f "$BRIDGE_BIN" ]] || { echo 'Existing bridge binary is required for rollback' >&2; exit 1; }
  cp -p "$BRIDGE_BIN" "$DEPLOY_ROOT/previous-bridge-binary"
fi
previous_runtime=false
if [[ "$workflow_runtime" == true && -f "$runtime_bin" ]]; then
  cp -p "$runtime_bin" "$DEPLOY_ROOT/previous-workflow-runtime.mjs"
  previous_runtime=true
fi
rollback() {
  trap - ERR TERM INT HUP
  echo "Deployment failed; restoring previous binary" >&2
  install -m 755 "$previous" "$BIN_PATH.rollback"
  mv -f "$BIN_PATH.rollback" "$BIN_PATH"
  if [[ -n "$BRIDGE_BIN" ]]; then
    install -m 755 "$DEPLOY_ROOT/previous-bridge-binary" "$BRIDGE_BIN.rollback"
    mv -f "$BRIDGE_BIN.rollback" "$BRIDGE_BIN"
  fi
  if [[ "$workflow_runtime" == true ]]; then
    if [[ "$previous_runtime" == true ]]; then
      cp -p "$DEPLOY_ROOT/previous-workflow-runtime.mjs" "$runtime_bin.rollback"
      mv -f "$runtime_bin.rollback" "$runtime_bin"
    else rm -f "$runtime_bin"; fi
  fi
  systemctl --user restart "${services[@]}" || echo "Rollback restart failed; inspect service journal" >&2
  printf '%s\n' "$revision" > "$DEPLOY_ROOT/failed-revision"
}
install -m 755 "$release/easyagent" "$BIN_PATH.next"
# Any failure from this point must restore the known working executable.
trap 'rollback' ERR
trap 'rollback; exit 1' TERM INT HUP
if [[ "$workflow_runtime" == true ]]; then
  install -m 644 "$release/workflow-runtime.mjs" "$runtime_bin.next"
  mv -f "$runtime_bin.next" "$runtime_bin"
fi
mv -f "$BIN_PATH.next" "$BIN_PATH"
if [[ -n "$BRIDGE_BIN" ]]; then
  install -m 755 "$release/easyagent-bridge" "$BRIDGE_BIN.next"
  mv -f "$BRIDGE_BIN.next" "$BRIDGE_BIN"
fi
systemctl --user restart "${services[@]}"
healthy=false
for attempt in $(seq 1 20); do
  if systemctl --user is-active --quiet "$SERVICE" && curl --fail --silent --max-time 3 "$HEALTH_URL" | python3 -c 'import json,sys; d=json.load(sys.stdin); sys.exit(0 if d.get("status")=="ok" and d.get("version")==sys.argv[1] else 1)' "$revision" 2>/dev/null; then
    if [[ -z "$BRIDGE_BIN" ]] || systemctl --user is-active --quiet "$BRIDGE_SERVICE"; then healthy=true; break; fi
  fi
  sleep 1
done
if [[ "$healthy" != true ]]; then false; fi
trap - ERR TERM INT HUP
printf '%s\n' "$revision" > "$DEPLOY_ROOT/current-revision.next"
mv -f "$DEPLOY_ROOT/current-revision.next" "$DEPLOY_ROOT/current-revision"
rm -f "$DEPLOY_ROOT/failed-revision"
echo "Deployed and healthy: $revision"
