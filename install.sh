#!/bin/sh
# Bootstrap dot: download the pinned release, check its sha256, install it, then install a profile.
#   sh install.sh [<profile url> [dot install options]]
# VERSION and the sums are rewritten by scripts/pin-install.sh after each release.
set -eu

VERSION=v0.1.0
SHA256_LINUX_X64=d79bcf13d73d096d88def787cbabdb9d53bbe1d5677de44d058a83dbc0ddaffa
SHA256_LINUX_ARM64=d1685d7fde2d16ac65deb0f70f88c67a6f12948464809ad18428b21b4519a8cb
SHA256_MACOS_X64=e728018c0eef646f282141b45c7fe2e6d53c47709bfb6dbd44998be04a2c1e01
SHA256_MACOS_ARM64=87a1422b8f93fb08bc3f4512d8d887d555d4159b521850d0a443def76fea54e6

die() { echo "install.sh : $*" >&2; exit 1; }

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64) platform=linux-x64; want=$SHA256_LINUX_X64 ;;
  Linux-aarch64 | Linux-arm64) platform=linux-arm64; want=$SHA256_LINUX_ARM64 ;;
  Darwin-x86_64) platform=macos-x64; want=$SHA256_MACOS_X64 ;;
  Darwin-arm64) platform=macos-arm64; want=$SHA256_MACOS_ARM64 ;;
  *) die "plateforme non gérée : $(uname -s)-$(uname -m)" ;;
esac

case "$VERSION" in v[0-9]*.[0-9]*.[0-9]*) ;; *) die "script non figé (VERSION=$VERSION) : lance scripts/pin-install.sh" ;; esac
case "$want" in
  *[!0-9a-f]* | '') die "somme sha256 absente pour $platform : script non figé" ;;
esac
[ "${#want}" -eq 64 ] || die "somme sha256 invalide pour $platform"

if command -v sha256sum >/dev/null 2>&1; then sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else die "sha256sum ou shasum requis"; fi

# DOT_INSTALL_BASE replaces the download URL, for tests only.
base=${DOT_INSTALL_BASE:-https://github.com/fmatsos/dot/releases/download}
dest=$HOME/.local/bin
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

curl -fsSL --proto '=https,file' -o "$tmp/dot" "$base/$VERSION/dot-$platform" || die "téléchargement impossible : $base/$VERSION/dot-$platform"
[ "$(sha256 "$tmp/dot")" = "$want" ] || die "sha256 invalide pour dot-$platform, abandon"

# Copy next to the destination, then rename: dot is never half written.
mkdir -p "$dest"
cp "$tmp/dot" "$dest/.dot.$$"
chmod 755 "$dest/.dot.$$"
mv -f "$dest/.dot.$$" "$dest/dot"
echo "dot $VERSION installé dans $dest/dot"
case ":$PATH:" in *":$dest:"*) ;; *) echo "ajoute $dest à ton PATH" ;; esac

if [ $# -gt 0 ]; then exec "$dest/dot" install "$@"; fi
