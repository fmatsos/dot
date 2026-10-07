#!/bin/sh
# Bootstrap dot: download the pinned release, check its sha256, install it, update it to the latest
# signed release, then install a profile.
#   sh install.sh [<profile url> [dot install options]]
# VERSION and the sums are rewritten by scripts/pin-install.sh; they only need a bump when the
# bootstrap itself must change (self-update broken, signing key rotated), not at every release.
set -eu

VERSION=v0.1.1
SHA256_LINUX_X64=80313fe93c3ea9843f122a97db9d85c64be0269fd112df45dff12826b65a817e
SHA256_LINUX_ARM64=73ad0f8daeb85292361786f29274a3dee416f5d10251b6057e9f78d82f1a6868
SHA256_MACOS_X64=80c0ddf77bbb5b35ef0c7ff2633d4ec7a3ac964b626de685e0a725dd297cb2a9
SHA256_MACOS_ARM64=2239228a4c4c1bb4e83429a02a2418dad67e08b23ad93b4fc222117d016d15b7

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

# The pinned release is only the bootstrap: dot checks the Ed25519 signature of the latest release
# (public key compiled in) and its checksum before replacing itself. If that fails, the pinned
# binary, already verified above, stays. DOT_INSTALL_NO_UPDATE=1 skips it, for tests.
if [ "${DOT_INSTALL_NO_UPDATE:-}" != 1 ]; then
  "$dest/dot" self-update || echo "mise à jour de dot impossible : $VERSION reste installé (dot self-update pour réessayer)" >&2
fi

if [ $# -gt 0 ]; then exec "$dest/dot" install "$@"; fi
