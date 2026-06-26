#!/usr/bin/env sh
# trimdown installer: curl -fsSL https://raw.githubusercontent.com/itssoumit/trimdown/main/packaging/install.sh | sh
set -eu

REPO="itssoumit/trimdown"
BINARY="trimdown"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64 | amd64) arch="amd64" ;;
  arm64 | aarch64) arch="arm64" ;;
  *) echo "trimdown: unsupported arch: $arch" >&2; exit 1 ;;
esac
case "$os" in
  linux | darwin) ;;
  *) echo "trimdown: unsupported OS: $os (use the Windows installer)" >&2; exit 1 ;;
esac

# Resolve latest tag via the GitHub API.
tag="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep '"tag_name"' | head -1 | cut -d'"' -f4)"
if [ -z "$tag" ]; then
  echo "trimdown: could not determine latest release" >&2
  exit 1
fi

asset="${BINARY}_${os}_${arch}.tar.gz"
url="https://github.com/${REPO}/releases/download/${tag}/${asset}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
echo "Downloading ${asset} (${tag})..."
curl -fsSL "$url" -o "$tmp/$asset"
tar -xzf "$tmp/$asset" -C "$tmp"

# Prefer /usr/local/bin; fall back to ~/.local/bin without sudo.
dest="/usr/local/bin"
if [ ! -w "$dest" ]; then
  if command -v sudo >/dev/null 2>&1 && [ -d "$dest" ]; then
    sudo install -m 0755 "$tmp/$BINARY" "$dest/$BINARY"
    echo "Installed to $dest/$BINARY"
    exit 0
  fi
  dest="$HOME/.local/bin"
  mkdir -p "$dest"
fi
install -m 0755 "$tmp/$BINARY" "$dest/$BINARY"
echo "Installed to $dest/$BINARY"
case ":$PATH:" in
  *":$dest:"*) ;;
  *) echo "Note: add $dest to your PATH." ;;
esac
