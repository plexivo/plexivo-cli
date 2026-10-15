#!/usr/bin/env bash
# Builds every platform's binary and publishes a GitHub release.
#   scripts/release.sh v0.1.0
set -euo pipefail
tag=${1:?usage: release.sh <vX.Y.Z>}
cd "$(dirname "$0")/.."
[[ -z $(git status --porcelain) ]] || { echo "commit first" >&2; exit 2; }
out=dist; rm -rf $out; mkdir -p $out
for target in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64; do
  os=${target%/*}; arch=${target#*/}; name=plexivo-$os-$arch; bin=plexivo; [[ $os == windows ]] && bin=plexivo.exe
  mkdir -p $out/$name
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w -X main.version=${tag#v}" -o $out/$name/$bin ./cmd/plexivo
  cp README.md LICENSE $out/$name/
  if [[ $os == windows ]]; then (cd $out/$name && zip -q ../$name.zip *); else tar -C $out/$name -czf $out/$name.tar.gz $bin README.md LICENSE; fi
done
cp scripts/install.sh $out/install.sh
(cd $out && shasum -a 256 *.tar.gz *.zip install.sh > SHA256SUMS)
git tag -a "$tag" -m "Plexivo CLI $tag" 2>/dev/null || true
git push origin "$tag"
gh release create "$tag" --title "Plexivo CLI $tag" --notes "Connect to your Plexivo databases and caches through an encrypted tunnel: \`plexivo db connect <name>\`." \
  $out/*.tar.gz $out/*.zip $out/install.sh $out/SHA256SUMS
