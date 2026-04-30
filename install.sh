#!/bin/sh
# install.sh — download and install the latest localsend-cli release.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/tingkai-c/localsend-cli/main/install.sh | sh
#
# Environment overrides:
#   VERSION       Tag to install (e.g. v1.2.6). Default: latest release.
#   INSTALL_DIR   Where to put the binary. Default: /usr/local/bin if writable,
#                 else $HOME/.local/bin.
#   BIN_NAME      Installed binary name. Default: localsend-cli.

set -eu

REPO="tingkai-c/localsend-cli"
BIN_NAME="${BIN_NAME:-localsend-cli}"

err() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }
info() { printf '==> %s\n' "$*"; }

need() {
    command -v "$1" >/dev/null 2>&1 || err "missing required tool: $1"
}

detect_os() {
    uname_s="$(uname -s)"
    case "$uname_s" in
        Linux)  echo linux ;;
        Darwin) echo darwin ;;
        MINGW*|MSYS*|CYGWIN*)
            err "Windows detected — use install.ps1 instead:
  irm https://raw.githubusercontent.com/${REPO}/main/install.ps1 | iex"
            ;;
        *) err "unsupported OS: $uname_s" ;;
    esac
}

detect_arch() {
    uname_m="$(uname -m)"
    case "$uname_m" in
        x86_64|amd64)   echo amd64 ;;
        aarch64|arm64)  echo arm64 ;;
        riscv64)        echo riscv64 ;;
        *) err "unsupported architecture: $uname_m (supported: amd64, arm64, riscv64)" ;;
    esac
}

resolve_version() {
    if [ -n "${VERSION:-}" ] && [ "${VERSION}" != "latest" ]; then
        printf '%s' "${VERSION#v}"
        return
    fi
    api_url="https://api.github.com/repos/${REPO}/releases/latest"
    if command -v curl >/dev/null 2>&1; then
        body="$(curl -fsSL "$api_url")"
    elif command -v wget >/dev/null 2>&1; then
        body="$(wget -qO- "$api_url")"
    else
        err "need curl or wget to look up the latest release"
    fi
    tag="$(printf '%s' "$body" | grep -m1 '"tag_name"' | cut -d '"' -f4)"
    [ -n "$tag" ] || err "could not resolve latest release tag"
    printf '%s' "${tag#v}"
}

fetch() {
    url="$1"
    out="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fSL --retry 3 -o "$out" "$url"
    else
        wget -qO "$out" "$url"
    fi
}

verify_sha256() {
    archive="$1"
    sums="$2"
    # Use awk with $2 ==/$NF == comparison so dots in the version string are
    # not treated as regex metacharacters.
    expected="$(awk -v f="$archive" '$NF == f { print $1; exit }' "$sums")"
    [ -n "$expected" ] || err "checksum for ${archive} not found in checksums.txt"
    if command -v sha256sum >/dev/null 2>&1; then
        actual="$(sha256sum "$archive" | awk '{print $1}')"
    elif command -v shasum >/dev/null 2>&1; then
        actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
    else
        err "need sha256sum or shasum to verify the download"
    fi
    [ "$expected" = "$actual" ] || err "checksum mismatch for ${archive}
  expected: $expected
  actual:   $actual"
}

pick_install_dir() {
    if [ -n "${INSTALL_DIR:-}" ]; then
        mkdir -p "$INSTALL_DIR"
        printf '%s' "$INSTALL_DIR"
        return
    fi
    if [ -w /usr/local/bin ] 2>/dev/null; then
        printf '%s' /usr/local/bin
    else
        mkdir -p "$HOME/.local/bin"
        printf '%s' "$HOME/.local/bin"
    fi
}

main() {
    need uname
    need tar
    need mkdir
    need mv

    os="$(detect_os)"
    arch="$(detect_arch)"
    ver="$(resolve_version)"

    info "Installing localsend-cli ${ver} for ${os}/${arch}"

    base="localsend-cli_${ver}_${os}_${arch}"
    archive="${base}.tar.gz"
    base_url="https://github.com/${REPO}/releases/download/v${ver}"

    tmp="$(mktemp -d 2>/dev/null || mktemp -d -t localsend-cli)"
    trap 'rm -rf "$tmp"' EXIT

    info "Downloading ${archive}"
    fetch "${base_url}/${archive}"     "${tmp}/${archive}"
    fetch "${base_url}/checksums.txt"  "${tmp}/checksums.txt"

    info "Verifying SHA-256"
    ( cd "$tmp" && verify_sha256 "$archive" checksums.txt )

    info "Extracting"
    tar -C "$tmp" -xzf "${tmp}/${archive}"

    dir="$(pick_install_dir)"
    target="${dir}/${BIN_NAME}"
    info "Installing to ${target}"
    if command -v install >/dev/null 2>&1; then
        install -m 0755 "${tmp}/localsend-cli" "$target"
    else
        cp "${tmp}/localsend-cli" "$target"
        chmod 0755 "$target"
    fi

    case ":$PATH:" in
        *":$dir:"*) ;;
        *)
            printf '\nNote: %s is not on your PATH. Add it with:\n  export PATH="%s:$PATH"\n' \
                "$dir" "$dir"
            ;;
    esac

    if [ "$os" = "linux" ]; then
        printf '\nLinux: if device discovery (ping) fails as a non-root user, run:\n'
        printf '  sudo setcap cap_net_raw=+ep %s\n' "$target"
    fi

    printf '\nDone. Run: %s --help\n' "$BIN_NAME"
}

main "$@"
