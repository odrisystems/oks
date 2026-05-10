#!/usr/bin/env bash
set -euo pipefail

# OKS installer
# - Downloads prebuilt binaries from GitHub Releases
# - Installs into /usr/local/bin (if writable) or ~/.local/bin
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/<OWNER>/<REPO>/main/install.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/<OWNER>/<REPO>/main/install.sh | bash -s -- --version v0.1.0
#
# Options:
#   --repo OWNER/REPO        (default: odrisystems/oks)
#   --version vX.Y.Z|latest  (default: latest)
#   --dir PATH               install dir (default: /usr/local/bin or ~/.local/bin)
#   --bin NAME               binary name (default: oks)

REPO="odrisystems/oks"
VERSION="latest"
INSTALL_DIR=""
BIN_NAME="oks"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo) REPO="${2:?}"; shift 2 ;;
    --version) VERSION="${2:?}"; shift 2 ;;
    --dir) INSTALL_DIR="${2:?}"; shift 2 ;;
    --bin) BIN_NAME="${2:?}"; shift 2 ;;
    -h|--help)
      sed -n '1,80p' "$0"
      exit 0
      ;;
    *)
      echo "Unknown arg: $1" >&2
      exit 2
      ;;
  esac
done

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"

case "$os" in
  linux|darwin) ;;
  mingw*|msys*|cygwin*) os="windows" ;;
  *)
    echo "Unsupported OS: $os" >&2
    exit 1
    ;;
esac

case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *)
    echo "Unsupported arch: $arch" >&2
    exit 1
    ;;
esac

if [[ -z "$INSTALL_DIR" ]]; then
  if [[ -w "/usr/local/bin" ]]; then
    INSTALL_DIR="/usr/local/bin"
  else
    INSTALL_DIR="${HOME}/.local/bin"
  fi
fi
mkdir -p "$INSTALL_DIR"

tmpdir="$(mktemp -d)"
cleanup() { rm -rf "$tmpdir"; }
trap cleanup EXIT

if [[ "$VERSION" == "latest" ]]; then
  # GitHub's /releases/latest ignores prereleases. We instead pick the most recent release
  # (including prereleases) via the GitHub API.
  tag="$(
    curl -fsSL "https://api.github.com/repos/${REPO}/releases?per_page=1" \
      | tr -d '\r\n' \
      | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p'
  )"
  if [[ -z "$tag" ]]; then
    echo "Failed to resolve latest release tag for ${REPO}" >&2
    exit 1
  fi
  VERSION="$tag"
fi

ext="tar.gz"
if [[ "$os" == "windows" ]]; then
  ext="zip"
fi

# Matches GoReleaser default archive naming with our config:
#   oks_<version>_<os>_<arch>.<ext>
# VERSION is a tag like v0.1.0 or v0.0.0-nightly....
# Archive uses GoReleaser's .Version (tag without leading v).
asset="${BIN_NAME}_${VERSION#v}_${os}_${arch}.${ext}"
url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"

echo "Downloading ${url}" >&2
curl -fL --retry 3 --retry-delay 2 -o "${tmpdir}/${asset}" "$url"

if [[ "$ext" == "zip" ]]; then
  command -v unzip >/dev/null 2>&1 || { echo "unzip is required" >&2; exit 1; }
  unzip -q "${tmpdir}/${asset}" -d "$tmpdir"
else
  tar -xzf "${tmpdir}/${asset}" -C "$tmpdir"
fi

src="${tmpdir}/${BIN_NAME}"
if [[ "$os" == "windows" ]]; then
  src="${tmpdir}/${BIN_NAME}.exe"
fi
if [[ ! -f "$src" ]]; then
  echo "Downloaded archive did not contain expected binary: ${BIN_NAME}" >&2
  exit 1
fi

dst="${INSTALL_DIR}/${BIN_NAME}"
if [[ "$os" == "windows" ]]; then
  dst="${INSTALL_DIR}/${BIN_NAME}.exe"
fi

install -m 0755 "$src" "$dst"
echo "Installed ${dst}" >&2
echo "Run: ${BIN_NAME} -h" >&2

