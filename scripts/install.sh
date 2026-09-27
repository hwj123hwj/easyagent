#!/usr/bin/env bash
#
# EasyAgent 一键安装脚本
#
# 用法:
#   curl -fsSL https://raw.githubusercontent.com/hwj123hwj/easyagent/main/scripts/install.sh | bash
#
# 脚本会自动完成:
#   1. 检测系统 → 下载并校验指定 Release（默认最新正式版）
#   2. 安装到 ~/.easyagent/bin 并配置 PATH
#   3. 创建 ~/.easyagent/.env 配置文件
#   4. 引导用户填入 API Key
#   5. 创建 easyagent 全局命令别名
#
set -euo pipefail
umask 077

REPO="hwj123hwj/easyagent"
LEGACY_ROOT="${PI_GO_HOME:-$HOME/.pi-go}"
INSTALL_ROOT="${EA_HOME:-$HOME/.easyagent}"
if [ -n "${PI_GO_HOME:-}" ] && [ "${PI_GO_HOME}" != "$HOME/.pi-go" ] && [ -z "${EA_HOME:-}" ]; then
    INSTALL_ROOT="${PI_GO_HOME}"
fi
INSTALL_DIR="${INSTALL_ROOT}/bin"
BINARY_NAME="easyagent"
CONFIG_FILE="${INSTALL_ROOT}/.env"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'

info()  { echo -e "${CYAN}ℹ${NC}  $*"; }
ok()    { echo -e "${GREEN}✓${NC}  $*"; }
warn()  { echo -e "${YELLOW}⚠${NC}  $*"; }
fail()  { echo -e "${RED}✗${NC}  $*"; exit 1; }

# ── Banner ──
echo ""
echo -e "${BOLD}  ╔══════════════════════════════════════╗${NC}"
echo -e "${BOLD}  ║        EasyAgent Installer                ║${NC}"
echo -e "${BOLD}  ║   Interactive AI Agent (Bubble Tea)  ║${NC}"
echo -e "${BOLD}  ╚══════════════════════════════════════╝${NC}"
echo ""

# ── 1. 检测平台 ──
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$OS" in
    linux)  OS="linux"  ;;
    darwin) OS="darwin" ;;
    *)      fail "Unsupported OS: $OS (only linux/macOS supported)" ;;
esac
case "$ARCH" in
    x86_64|amd64)  ARCH="amd64"  ;;
    aarch64|arm64) ARCH="arm64" ;;
    *)             fail "Unsupported architecture: $ARCH" ;;
esac
info "Platform: ${OS}/${ARCH}"

# ── 2. 安装二进制 ──
if [ -d "${LEGACY_ROOT}" ] && [ "${LEGACY_ROOT}" != "${INSTALL_ROOT}" ]; then
    if [ -e "${INSTALL_ROOT}" ]; then
        fail "Both legacy and EasyAgent data directories exist. Resolve ${LEGACY_ROOT} and ${INSTALL_ROOT} before installing."
    fi
    info "Migrating existing data: ${LEGACY_ROOT} → ${INSTALL_ROOT}"
    mv "${LEGACY_ROOT}" "${INSTALL_ROOT}"
fi
mkdir -p "${INSTALL_DIR}"

BINARY_PATH="${INSTALL_DIR}/${BINARY_NAME}"
LEGACY_BINARY_NAME="pi-agent"
NEEDS_DOWNLOAD=true

# 如果已有相同版本，跳过
if [ -f "${BINARY_PATH}" ]; then
    info "Found existing EasyAgent installation: ${BINARY_PATH}"
    read -rp "$(echo -e ${CYAN}ℹ${NC}  Reinstall/upgrade? [Y/n] )" REPLY < /dev/tty 2>/dev/null || REPLY="y"
    if [[ "$REPLY" == "n" || "$REPLY" == "N" ]]; then
        ok "Keeping existing installation"
        NEEDS_DOWNLOAD=false
    fi
fi

# Downloads and verifies everything before replacing an installed file.
# Run in a subshell so temporary files and rollback traps cannot leak to setup.
download_release() (
    set -euo pipefail
    tag=${EA_VERSION:-}
    if [ -z "$tag" ]; then
        info "Fetching latest stable release..."
        tag=$(curl -fsSL --connect-timeout 10 --max-time 60 "https://api.github.com/repos/${REPO}/releases/latest" \
            | sed -nE 's/^[[:space:]]*"tag_name":[[:space:]]*"([^"]+)".*/\1/p')
    fi
    [[ "$tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(alpha|beta|rc)\.[1-9][0-9]*)?$ ]] \
        || fail "Invalid release version: $tag"
    stage=$(mktemp -d "${INSTALL_DIR}/.download.XXXXXX")
    publishing=false
    destinations=()
    rollback() {
        status=$?
        if [ "$publishing" = true ] && [ "$status" -ne 0 ]; then
            for dest in "${destinations[@]}"; do
                if [ -e "$stage/backup/$dest" ] || [ -L "$stage/backup/$dest" ]; then
                    mv -f "$stage/backup/$dest" "$INSTALL_DIR/$dest"
                else
                    rm -f "$INSTALL_DIR/$dest"
                fi
            done
        fi
        rm -rf "$stage"
        exit "$status"
    }
    trap rollback EXIT
    base="https://github.com/${REPO}/releases/download/$tag"
    curl -fsSL --connect-timeout 10 --max-time 60 "$base/checksums.txt" -o "$stage/checksums.txt" \
        || fail "Cannot retrieve release checksums; installation unchanged"
    checksum_for() {
        awk -v name="$1" '$2 == name || $2 == "*" name {value=$1; count++} END {if (count == 1) print value; else exit 1}' "$stage/checksums.txt"
    }
    fetch_verified() {
        asset=$1
        digest=$(checksum_for "$asset") || fail "Missing or duplicate checksum: $asset"
        [[ "$digest" =~ ^[0-9a-fA-F]{64}$ ]] || fail "Invalid SHA-256: $asset"
        curl -fSL --connect-timeout 10 --max-time 300 "$base/$asset" -o "$stage/$asset" \
            || fail "Download failed: $asset; installation unchanged"
        if command -v sha256sum >/dev/null 2>&1; then
            actual=$(sha256sum "$stage/$asset" | awk '{print $1}')
        else
            actual=$(shasum -a 256 "$stage/$asset" | awk '{print $1}')
        fi
        [ "$actual" = "$digest" ] || fail "SHA-256 mismatch: $asset; installation unchanged"
    }
    core="easyagent-$OS-$ARCH"
    if ! checksum_for "$core" >/dev/null; then
        core="pi-agent-$OS-$ARCH" # Historical assets remain supported, with verification.
    fi
    fetch_verified "$core"
    chmod +x "$stage/$core"
    if [[ "$core" == easyagent-* ]]; then
        reported=$("$stage/$core" --version)
        [ "$reported" = "easyagent $tag" ] || fail "Downloaded binary reports a different version: $reported"
    fi
    # Historical pi-agent --version runs past flag parsing into app startup.
    # Verify its release checksum, but never execute that binary during installation.
    mv "$stage/$core" "$stage/easyagent"
    destinations=(easyagent)
    if [[ "$core" == easyagent-* ]] || checksum_for release.json >/dev/null; then
        fetch_verified release.json
        fetch_verified "easyagent-bridge-$OS-$ARCH"
        chmod +x "$stage/easyagent-bridge-$OS-$ARCH"
        [ "$("$stage/easyagent-bridge-$OS-$ARCH" --version)" = "easyagent-bridge $tag" ] \
            || fail 'Bridge version differs from release'
        mv "$stage/easyagent-bridge-$OS-$ARCH" "$stage/easyagent-bridge"
        fetch_verified workflow-runtime.mjs
        fetch_verified workflow-runtime-licenses.tar.gz
        destinations+=(easyagent-bridge workflow-runtime.mjs workflow-runtime-licenses.tar.gz release.json)
    elif [ -e "$INSTALL_DIR/workflow-runtime.mjs" ]; then
        fail 'This historical release has no workflow bundle; use a separate EA_HOME for downgrades'
    fi
    mkdir "$stage/backup"
    for dest in "${destinations[@]}"; do
        [ ! -d "$INSTALL_DIR/$dest" ] || fail "Install destination is a directory: $dest"
        if [ -e "$INSTALL_DIR/$dest" ] || [ -L "$INSTALL_DIR/$dest" ]; then
            cp -pP "$INSTALL_DIR/$dest" "$stage/backup/$dest"
        fi
    done
    publishing=true
    for dest in "${destinations[@]}"; do
        mv -f "$stage/$dest" "$INSTALL_DIR/$dest"
    done
    publishing=false
    ok "Installed verified release $tag"
)

if [ "$NEEDS_DOWNLOAD" = true ]; then
    download_release
fi

chmod +x "${BINARY_PATH}" 2>/dev/null || true
ok "Binary: ${BINARY_PATH}"

# ── 3. 配置 PATH ──
PATH_CONFIGURED=false
case ":$PATH:" in
    *":${INSTALL_DIR}:"*)
        PATH_CONFIGURED=true
        ;;
esac

if [ "$PATH_CONFIGURED" = false ]; then
    # 检测用户的 shell rc 文件
    SHELL_NAME=$(basename "$SHELL")
    case "$SHELL_NAME" in
        zsh)  RC_FILE="$HOME/.zshrc" ;;
        bash) RC_FILE="$HOME/.bashrc" ;;
        fish) RC_FILE="$HOME/.config/fish/config.fish" ;;
        *)    RC_FILE="$HOME/.profile" ;;
    esac

    # 写入 PATH 配置
    touch "$RC_FILE"
    if ! grep -q "${INSTALL_DIR}" "$RC_FILE" 2>/dev/null; then
        echo "" >> "$RC_FILE"
        echo "# EasyAgent" >> "$RC_FILE"
        if [ "$SHELL_NAME" = "fish" ]; then
            echo "set -gx PATH \$PATH ${INSTALL_DIR}" >> "$RC_FILE"
        else
            echo "export PATH=\"\$PATH:${INSTALL_DIR}\"" >> "$RC_FILE"
        fi
        ok "PATH configured in ${RC_FILE}"
    fi

    # 当前 session 也生效
    export PATH="$PATH:${INSTALL_DIR}"
fi

# ── 4. 创建快捷封装脚本 ──
# Keep the previous command as a compatibility alias.
ln -sfn "${BINARY_NAME}" "${INSTALL_DIR}/pi-go"

# ── 5. 配置文件 ──
if [ ! -f "${CONFIG_FILE}" ]; then
    info "Creating config: ${CONFIG_FILE}"

    # 引导用户配置
    echo ""
    echo -e "${BOLD}  ── 配置 API Key ──${NC}"
    echo ""
    echo -e "  选择你的 LLM Provider:"
    echo -e "  ${CYAN}1${NC}) OpenAI / 本地网关 (默认)"
    echo -e "  ${CYAN}2${NC}) Anthropic Claude"
    echo -e "  ${CYAN}3${NC}) 跳过，稍后手动配置"
    echo ""
    read -rp "$(echo -e ${CYAN}ℹ${NC}  选择 [1/2/3]: )" CHOICE < /dev/tty 2>/dev/null || CHOICE="3"

    case "${CHOICE:-1}" in
        1)
            read -rp "$(echo -e ${CYAN}ℹ${NC}  API Key: )" API_KEY < /dev/tty 2>/dev/null || API_KEY=""
            read -rp "$(echo -e ${CYAN}ℹ${NC}  Base URL [http://localhost:4001]: )" BASE_URL < /dev/tty 2>/dev/null || BASE_URL=""
            read -rp "$(echo -e ${CYAN}ℹ${NC}  Model [longcat-opus]: )" MODEL < /dev/tty 2>/dev/null || MODEL=""

            cat > "${CONFIG_FILE}" << EOF
EA_PROVIDER=openai
EA_API_KEY=${API_KEY}
EA_BASE_URL=${BASE_URL:-http://localhost:4001}
EA_MODEL=${MODEL:-longcat-opus}
EA_HOST=127.0.0.1
EA_PORT=8080

# Enable bash tool (allows running shell commands like free, top, ps, etc.)
EA_ENABLE_BASH=true
EOF
            ok "Config saved"
            ;;
        2)
            read -rp "$(echo -e ${CYAN}ℹ${NC}  Anthropic API Key: )" API_KEY < /dev/tty 2>/dev/null || API_KEY=""
            cat > "${CONFIG_FILE}" << EOF
EA_PROVIDER=anthropic
ANTHROPIC_API_KEY=${API_KEY}
ANTHROPIC_MODEL=claude-sonnet-4-20250514
EA_HOST=127.0.0.1
EA_PORT=8080
EOF
            ok "Config saved"
            ;;
        3)
            cat > "${CONFIG_FILE}" << 'EOF'
# EasyAgent 配置文件
# 请填入你的 API Key 和 Provider 信息

EA_PROVIDER=openai
EA_API_KEY=sk-your-key-here
EA_BASE_URL=http://localhost:4001
EA_MODEL=longcat-opus

# Anthropic (可选)
# EA_PROVIDER=anthropic
# ANTHROPIC_API_KEY=your-key
# ANTHROPIC_MODEL=claude-sonnet-4-20250514
EOF
            warn "Config template created at ${CONFIG_FILE}"
            info "Edit it later: nano ${CONFIG_FILE}"
            ;;
    esac
else
    ok "Config exists: ${CONFIG_FILE}"
fi

# ── 6. 完成 ──
echo ""
echo -e "${GREEN}${BOLD}  🎉 安装完成！${NC}"
echo ""
echo -e "  ${BOLD}下一步：${NC}"
echo ""
if [ "$PATH_CONFIGURED" = false ]; then
    echo -e "  1. 重新加载终端配置:"
    echo -e "     ${CYAN}source ${RC_FILE}${NC}"
    echo ""
fi
echo -e "  ${BOLD}启动交互式 TUI:${NC}"
echo -e "     ${GREEN}easyagent chat${NC}"
echo ""
echo -e "  ${BOLD}单次提问:${NC}"
echo -e "     ${GREEN}easyagent run -p \"你好\"${NC}"
echo ""
echo -e "  ${BOLD}启动 HTTP 服务:${NC}"
echo -e "     ${GREEN}easyagent serve${NC}"
echo ""
echo -e "  安装路径: ${CYAN}${INSTALL_DIR}${NC}"
echo -e "  配置文件: ${CYAN}${CONFIG_FILE}${NC}"
echo ""
