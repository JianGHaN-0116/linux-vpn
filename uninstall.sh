#!/usr/bin/env bash
#===========================================================================
# vpn — mihomo-based CLI proxy tool — uninstaller
#===========================================================================
set -euo pipefail

INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
DATA_DIR="${VPN_DATA_DIR:-$HOME/.vpn}"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
info()  { printf "${GREEN}[+]${NC} %s\n" "$*"; }
warn()  { printf "${YELLOW}[!]${NC} %s\n" "$*"; }
step()  { printf "${CYAN}[*]${NC} %s\n" "$*"; }

confirm() {
    printf "${YELLOW}WARNING: This will remove vpn and all its data.${NC}\n"
    printf "Are you sure? [y/N] "
    read -r ans
    case "$ans" in
        y|Y|yes|Yes) return 0;;
        *) echo "Aborted."; exit 1;;
    esac
}

remove_binary() {
    if [ -f "${INSTALL_DIR}/vpn" ]; then
        step "Removing vpn binary from ${INSTALL_DIR}..."
        if [ ! -w "${INSTALL_DIR}" ]; then
            sudo rm -f "${INSTALL_DIR}/vpn"
        else
            rm -f "${INSTALL_DIR}/vpn"
        fi
        info "Removed ${INSTALL_DIR}/vpn"
    else
        warn "vpn binary not found in ${INSTALL_DIR}"
    fi
}

remove_symlinks() {
    for link in mihomo yq; do
        if [ -L "${INSTALL_DIR}/${link}" ]; then
            step "Removing symlink ${INSTALL_DIR}/${link}..."
            if [ ! -w "${INSTALL_DIR}" ]; then
                sudo rm -f "${INSTALL_DIR}/${link}"
            else
                rm -f "${INSTALL_DIR}/${link}"
            fi
            info "Removed ${INSTALL_DIR}/${link}"
        fi
    done
}

remove_data() {
    if [ -d "$DATA_DIR" ]; then
        step "Removing data directory ${DATA_DIR}..."
        rm -rf "$DATA_DIR"
        info "Removed ${DATA_DIR}"
    else
        warn "Data directory not found: ${DATA_DIR}"
    fi
}

clean_shell_helper() {
    local marker="# vpn-proxy-helper"
    local files=()

    files+=("$HOME/.bashrc")
    files+=("$HOME/.zshrc")
    files+=("$HOME/.config/fish/config.fish")
    files+=("$HOME/.profile")

    for rc in "${files[@]}"; do
        if [ -f "$rc" ] && grep -q "$marker" "$rc" 2>/dev/null; then
            step "Removing shell helper from $rc..."
            tmp=$(mktemp)
            sed "/$marker/,/^$/d" "$rc" > "$tmp" && mv "$tmp" "$rc"
            info "Cleaned $rc"
        fi
    done
}

clean_git_proxy() {
    if command -v git &>/dev/null; then
        step "Removing git proxy settings..."
        git config --global --unset http.proxy 2>/dev/null || true
        git config --global --unset https.proxy 2>/dev/null || true
        info "Git proxy unset"
    fi
}

clean_ssh_proxy() {
    local ssh_config="$HOME/.ssh/config"
    if [ -f "$ssh_config" ] && grep -q "# vpn-proxy:github" "$ssh_config" 2>/dev/null; then
        step "Removing SSH proxy config..."
        tmp=$(mktemp)
        sed '/# vpn-proxy:github/,/# vpn-proxy:end/d' "$ssh_config" > "$tmp" && mv "$tmp" "$ssh_config"
        info "SSH proxy removed from $ssh_config"
    fi
}

stop_mihomo() {
    if command -v vpn &>/dev/null; then
        step "Stopping mihomo..."
        vpn off 2>/dev/null || true
        info "mihomo stopped"
    fi
}

main() {
    echo ""
    echo "  ╔══════════════════════════════════╗"
    echo "  ║      vpn — uninstall script      ║"
    echo "  ╚══════════════════════════════════╝"
    echo ""

    confirm

    stop_mihomo
    clean_git_proxy
    clean_ssh_proxy
    clean_shell_helper
    remove_binary
    remove_symlinks
    remove_data

    echo ""
    info "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    info "  vpn has been uninstalled."
    info "  Binary  : removed from ${INSTALL_DIR}/vpn"
    info "  Data    : removed ${DATA_DIR}"
    info "  Shell   : helper function removed"
    info "  Git     : proxy config cleaned"
    info "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "  To complete cleanup, restart your shell or run:"
    echo "    exec \"\$SHELL\""
    echo ""
}

main "$@"
