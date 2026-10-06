#!/usr/bin/env bash
# Pin a betterleaks release: download the archive of each platform from the official GitHub
# release, compute its sha256 and print the Go lines to paste into the table
# `Betterleaks.Assets` of internal/guard/betterleaks.go (update Version there too).
#   scripts/pin-betterleaks.sh <version>        e.g. 1.9.0
# The archive names follow betterleaks_<version>_<os>_<arch>.tar.gz; check them against the
# release page and override the pattern if they differ:
#   ASSET_PATTERN='bl_%s_%s_%s.tar.gz' scripts/pin-betterleaks.sh 1.9.0   (version, os, arch)
set -euo pipefail

version=${1:?usage : scripts/pin-betterleaks.sh <version>}
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "version invalide : $version" >&2; exit 2; }
pattern=${ASSET_PATTERN:-betterleaks_%s_%s_%s.tar.gz}
base=https://github.com/betterleaks/betterleaks/releases/download/v$version
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT

sha256() { if command -v sha256sum >/dev/null; then sha256sum "$1"; else shasum -a 256 "$1"; fi | cut -d' ' -f1; }

# <table key> <os in the asset name> <arch in the asset name>
for p in "linux-x64 linux x64" "linux-arm64 linux arm64" "macos-x64 darwin x64" "macos-arm64 darwin arm64"; do
  read -r key os arch <<<"$p"
  # shellcheck disable=SC2059 # The pattern is a printf format on purpose.
  name=$(printf "$pattern" "$version" "$os" "$arch")
  curl -fsSL --proto '=https' -o "$tmp/$name" "$base/$name" || { echo "téléchargement impossible : $base/$name" >&2; exit 1; }
  printf '\t\t"%s": {Name: "%s", SHA256: "%s"},\n' "$key" "$name" "$(sha256 "$tmp/$name")"
done
