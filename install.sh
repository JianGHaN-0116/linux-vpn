#!/usr/bin/env bash
#===========================================================================
# vpn — mihomo-based CLI proxy tool — one-click installer
#===========================================================================
set -euo pipefail

# ----- user-configurable ------------------------------------------------
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
DATA_DIR="${VPN_DATA_DIR:-$HOME/.vpn}"
MIHOMO_VER="${MIHOMO_VER:-1.19.4}"
YQ_VER="${YQ_VER:-4.44.3}"
REPO_URL="https://github.com/nelvko/vpn"
# ------------------------------------------------------------------------

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
info()  { printf "${GREEN}[+]${NC} %s\n" "$*"; }
warn()  { printf "${YELLOW}[!]${NC} %s\n" "$*"; }
err()   { printf "${RED}[x]${NC} %s\n" "$*"; exit 1; }
step()  { printf "${CYAN}[*]${NC} %s\n" "$*"; }

# ----- helpers ----------------------------------------------------------
arch() {
    case "$(uname -m)" in
        x86_64|amd64) echo "amd64";;
        aarch64|arm64) echo "arm64";;
        armv7l) echo "armv7";;
        *) echo "amd64";;
    esac
}

os() {
    case "$(uname -s)" in
        Linux)  echo "linux";;
        Darwin) echo "darwin";;
        *) echo "linux";;
    esac
}

detect_shell() {
    basename "${SHELL:-/bin/bash}"
}

# ----- dependency installers -------------------------------------------
install_go() {
    command -v go >/dev/null 2>&1 && { info "go $(go version | awk '{print $3}')"; return 0; }
    step "Installing Go..."
    local go_tar="go1.23.4.$(os)-$(arch).tar.gz"
    local go_url="https://go.dev/dl/${go_tar}"
    curl -fsSL "$go_url" -o "/tmp/${go_tar}" || wget -q "$go_url" -O "/tmp/${go_tar}"
    sudo tar -C /usr/local -xzf "/tmp/${go_tar}"
    rm -f "/tmp/${go_tar}"
    export PATH="/usr/local/go/bin:$PATH"
    info "Go installed: $(go version)"
}

install_mihomo() {
    local dest="${DATA_DIR}/bin/mihomo"
    [ -x "$dest" ] && { info "mihomo v${MIHOMO_VER} (cached)"; return 0; }

    # Check system PATH
    command -v mihomo >/dev/null 2>&1 && { info "mihomo $(mihomo --version 2>/dev/null || echo ok)"; return 0; }

    step "Downloading mihomo v${MIHOMO_VER}..."
    mkdir -p "${DATA_DIR}/bin"
    local tar_name="mihomo-linux-$(arch)-v${MIHOMO_VER}.gz"
    local url="https://github.com/MetaCubeX/mihomo/releases/download/v${MIHOMO_VER}/${tar_name}"

    curl -fsSL "$url" -o "/tmp/${tar_name}" || wget -q "$url" -O "/tmp/${tar_name}"
    gzip -d -c "/tmp/${tar_name}" > "$dest" 2>/dev/null || {
        # Try uncompressed
        curl -fsSL "${url%.gz}" -o "$dest" || wget -q "${url%.gz}" -O "$dest"
    }
    chmod +x "$dest"
    rm -f "/tmp/${tar_name}"
    ln -sf "$dest" "${INSTALL_DIR}/mihomo" 2>/dev/null || true
    info "mihomo v${MIHOMO_VER} installed"
}

install_yq() {
    command -v yq >/dev/null 2>&1 && { info "yq $(yq --version)"; return 0; }
    local dest="${DATA_DIR}/bin/yq"
    [ -x "$dest" ] && { info "yq v${YQ_VER} (cached)"; return 0; }

    step "Downloading yq v${YQ_VER}..."
    mkdir -p "${DATA_DIR}/bin"
    local bin_name="yq_$(os)_$(arch)"
    local url="https://github.com/mikefarah/yq/releases/download/v${YQ_VER}/${bin_name}"

    curl -fsSL "$url" -o "$dest" || wget -q "$url" -O "$dest"
    chmod +x "$dest"
    ln -sf "$dest" "${INSTALL_DIR}/yq" 2>/dev/null || true
    info "yq v${YQ_VER} installed"
}

install_geodata() {
    [ -f "${DATA_DIR}/Country.mmdb" ] && { info "GeoIP data (cached)"; return 0; }

    step "Downloading GeoIP / GeoSite data..."
    curl -fsSL "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat" \
        -o "${DATA_DIR}/Country.mmdb" 2>/dev/null || true
    curl -fsSL "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat" \
        -o "${DATA_DIR}/geosite.dat" 2>/dev/null || true

    # Fallback: use mihomo's built-in download if files are small/missing
    [ -s "${DATA_DIR}/Country.mmdb" ] || touch "${DATA_DIR}/Country.mmdb"
    [ -s "${DATA_DIR}/geosite.dat" ] || touch "${DATA_DIR}/geosite.dat"
    info "Geo data installed"
}

# ----- build & install -------------------------------------------------
build_vpn() {
    step "Building vpn..."
    cd "$SCRIPT_DIR"

    # If binary already built and up-to-date, skip
    if [ -f "./vpn" ] && [ "./vpn" -nt "./main.go" ] 2>/dev/null; then
        info "vpn binary is up to date"
        return 0
    fi

    export PATH="/usr/local/go/bin:$PATH"
    command -v go >/dev/null 2>&1 || err "Go not found; install Go first"

    go mod tidy 2>/dev/null || true
    go build -ldflags="-s -w" -o vpn . || err "Build failed"
    info "Build complete: $(file vpn | cut -d, -f1)"
}

install_binary() {
    step "Installing vpn to ${INSTALL_DIR}..."
    if [ ! -w "${INSTALL_DIR}" ]; then
        sudo install -m 755 "$SCRIPT_DIR/vpn" "${INSTALL_DIR}/vpn"
    else
        install -m 755 "$SCRIPT_DIR/vpn" "${INSTALL_DIR}/vpn"
    fi
    info "Installed: ${INSTALL_DIR}/vpn"
}

# ----- shell integration -----------------------------------------------
setup_shell() {
    local sh="$(detect_shell)"
    local rc=""
    case "$sh" in
        bash) rc="$HOME/.bashrc";;
        zsh)  rc="$HOME/.zshrc";;
        fish) rc="$HOME/.config/fish/config.fish"; mkdir -p "$(dirname "$rc")";;
        *)    rc="$HOME/.profile";;
    esac

    if grep -q "# vpn-proxy-helper" "$rc" 2>/dev/null; then
        info "Shell helper already in $rc"
        return 0
    fi

    step "Adding shell helper to $rc..."
    cat >> "$rc" << 'SHELLFN'

# vpn-proxy-helper — wraps vpn for automatic proxy env sourcing
vpn() {
    case "${1:-}" in
        on)
            if [ "$#" -eq 1 ]; then
                command vpn on -s >&2
                eval "$(command vpn on -e 2>/dev/null)"
                echo "✓ Proxy enabled — http_proxy=$http_proxy"
            else
                command vpn "$@"
                if [[ "$*" == *"-e"* ]] || [[ "$*" == *"--env-only"* ]]; then
                    eval "$(command vpn "$@" 2>/dev/null)"
                fi
            fi
            ;;
        off)
            command vpn off "$@"
            unset http_proxy HTTP_PROXY https_proxy HTTPS_PROXY all_proxy ALL_PROXY no_proxy NO_PROXY
            ;;
        *)
            command vpn "$@"
            ;;
    esac
}
SHELLFN
    info "Shell helper installed — restart your shell or run: source $rc"
}

# ----- main -------------------------------------------------------------
main() {
    echo ""
    echo "  ╔══════════════════════════════════╗"
    echo "  ║   vpn — mihomo proxy installer   ║"
    echo "  ╚══════════════════════════════════╝"
    echo ""

    SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

    install_go
    install_mihomo
    install_yq
    install_geodata
    build_vpn
    install_binary

    # Initialize data directory
    mkdir -p "${DATA_DIR}/profiles" "${DATA_DIR}/bin"
    [ -f "${DATA_DIR}/mixin.yaml" ] || touch "${DATA_DIR}/mixin.yaml"

    setup_shell

    echo ""
    info "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    info "  Installation complete!"
    info "  Binary : ${INSTALL_DIR}/vpn"
    info "  Data   : ${DATA_DIR}"
    info "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "  Quick start:"
    echo "    source ~/.bashrc           # load shell helper"
    echo "    vpn sub add <url>          # add subscription"
    echo "    vpn on                     # start proxy"
    echo "    vpn status                 # check status"
    echo "    vpn off                    # stop proxy"
    echo ""
}

main "$@"
