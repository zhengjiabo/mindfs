#!/usr/bin/env bash
set -euo pipefail

REPO="${MINDFS_INSTALL_REPO:-zhengjiabo/mindfs}"
RELEASE_NOTES_URL="https://raw.githubusercontent.com/${REPO}/main/release-notes.md"
VERSION=""
PREFIX="${HOME}/.local"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --prefix) PREFIX="$2"; shift 2 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

detect_os() {
  local raw
  raw="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$raw" in
    darwin) echo "darwin" ;;
    linux) echo "linux" ;;
    *) echo "Unsupported OS: $raw" >&2; exit 1 ;;
  esac
}

detect_arch() {
  local raw
  raw="$(uname -m)"
  case "$raw" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    armv7*|armhf) echo "arm" ;;
    *) echo "Unsupported arch: $raw" >&2; exit 1 ;;
  esac
}

normalize_tag() {
  local value="${1:-}"
  value="${value#v}"
  printf 'v%s' "$value"
}

extract_version() {
  sed -nE '1s/^[[:space:]]*#[[:space:]]+MindFS[[:space:]]+(v?[0-9]+(\.[0-9]+){1,3}[^[:space:]]*).*$/\1/p'
}

if [[ -z "$VERSION" ]]; then
  echo "Fetching latest release version from ${REPO}..."
  if command -v curl &>/dev/null; then
    VERSION="$(curl -fsSL "$RELEASE_NOTES_URL" | extract_version)"
  elif command -v wget &>/dev/null; then
    VERSION="$(wget -qO- "$RELEASE_NOTES_URL" | extract_version)"
  else
    echo "Error: curl or wget is required." >&2
    exit 1
  fi
  if [[ -z "$VERSION" ]]; then
    echo "Error: could not determine latest version. Use --version to specify." >&2
    exit 1
  fi
fi

OS="$(detect_os)"
ARCH="$(detect_arch)"
VERSION="$(normalize_tag "$VERSION")"
FILENAME="mindfs_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

echo "Installing ${REPO} ${VERSION} for ${OS}/${ARCH}"
echo "  Prefix: ${PREFIX}"
echo "  Downloading ${URL}"

if command -v curl &>/dev/null; then
  curl -fsSL "$URL" -o "${TMPDIR}/${FILENAME}"
else
  wget -qO "${TMPDIR}/${FILENAME}" "$URL"
fi

tar -xzf "${TMPDIR}/${FILENAME}" -C "$TMPDIR"
PKG_DIR="${TMPDIR}/mindfs_${VERSION}_${OS}_${ARCH}"
if [[ ! -d "$PKG_DIR" ]]; then
  echo "Error: unexpected archive structure (expected ${PKG_DIR})." >&2
  exit 1
fi

mkdir -p "${PREFIX}/bin" "${PREFIX}/share/mindfs"
install -m 0755 "${PKG_DIR}/mindfs" "${PREFIX}/bin/mindfs"

if [[ -f "${PKG_DIR}/agents.json" ]]; then
  install -m 0644 "${PKG_DIR}/agents.json" "${PREFIX}/share/mindfs/agents.json"
fi

if [[ -f "${PKG_DIR}/task_template.json" ]]; then
  install -m 0644 "${PKG_DIR}/task_template.json" "${PREFIX}/share/mindfs/task_template.json"
fi

if [[ -d "${PKG_DIR}/web" ]]; then
  rm -rf "${PREFIX}/share/mindfs/web"
  cp -r "${PKG_DIR}/web" "${PREFIX}/share/mindfs/web"
fi

echo "Installed to ${PREFIX}/bin/mindfs"
