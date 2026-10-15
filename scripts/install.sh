#!/bin/sh
# Installs the Plexivo CLI from its latest GitHub release.
#   curl -fsSL https://github.com/plexivo/plexivo-cli/releases/latest/download/install.sh | sh
# PLEXIVO_INSTALL_DIR chooses where (default /usr/local/bin, or ~/.local/bin without write access).
set -eu
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) echo "unsupported CPU: $(uname -m)" >&2; exit 1 ;; esac
case "$os" in darwin|linux) ;; *) echo "unsupported system: $os (Windows: download the zip from the release page)" >&2; exit 1 ;; esac
base=https://github.com/plexivo/plexivo-cli/releases/latest/download
name=plexivo-$os-$arch
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
curl -fsSL "$base/$name.tar.gz" -o "$tmp/$name.tar.gz"
curl -fsSL "$base/SHA256SUMS" -o "$tmp/SHA256SUMS"
(cd "$tmp" && grep " $name.tar.gz\$" SHA256SUMS | { command -v sha256sum >/dev/null && sha256sum -c - || shasum -a 256 -c -; }) >/dev/null
tar -xzf "$tmp/$name.tar.gz" -C "$tmp"
dir=${PLEXIVO_INSTALL_DIR:-/usr/local/bin}
if [ ! -w "$dir" ] && [ -z "${PLEXIVO_INSTALL_DIR:-}" ]; then dir=$HOME/.local/bin; mkdir -p "$dir"; fi
install -m 0755 "$tmp/plexivo" "$dir/plexivo"
echo "Installed $("$dir/plexivo" version) to $dir/plexivo"
case ":$PATH:" in *":$dir:"*) ;; *) echo "Add $dir to your PATH." ;; esac
