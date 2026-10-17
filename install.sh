#!/bin/sh
# Bootstrap dot: download the pinned release, check its sha256, install it, update it to the latest
# signed release, then install a profile.
#   sh install.sh [<profile url> [dot install options]]
# VERSION and the sums are rewritten by scripts/pin-install.sh; they only need a bump when the
# bootstrap itself must change (self-update broken, signing key rotated), not at every release.
set -eu

VERSION=v0.8.1
SHA256_LINUX_X64=757a510b2f560e68c72381dbbf28c86ad53348c627aff03d460258e553d7b32d
SHA256_LINUX_ARM64=ae7ff46ee861f53693ee4db9fb83ce7f9bb748a5e55d4d5051deaa6740064973
SHA256_MACOS_X64=2ed7b7d40e114fd62ceee856d0bc5b1f8bf94562f63bfab96e94cf9d5f7b8212
SHA256_MACOS_ARM64=de63a26bbc2abcade0676a8767bc10ca87b3648e4b5182e223240aeb69253234

die() { echo "install.sh: $*" >&2; exit 1; }

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64) platform=linux-x64; want=$SHA256_LINUX_X64 ;;
  Linux-aarch64 | Linux-arm64) platform=linux-arm64; want=$SHA256_LINUX_ARM64 ;;
  Darwin-x86_64) platform=macos-x64; want=$SHA256_MACOS_X64 ;;
  Darwin-arm64) platform=macos-arm64; want=$SHA256_MACOS_ARM64 ;;
  *) die "unsupported platform: $(uname -s)-$(uname -m)" ;;
esac

case "$VERSION" in v[0-9]*.[0-9]*.[0-9]*) ;; *) die "script not pinned (VERSION=$VERSION): run scripts/pin-install.sh" ;; esac
case "$want" in
  *[!0-9a-f]* | '') die "sha256 sum missing for $platform: script not pinned" ;;
esac
[ "${#want}" -eq 64 ] || die "invalid sha256 sum for $platform"

if command -v sha256sum >/dev/null 2>&1; then sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else die "sha256sum or shasum required"; fi

# DOT_INSTALL_BASE replaces the download URL, for tests only.
base=${DOT_INSTALL_BASE:-https://github.com/fmatsos/dot/releases/download}
dest=$HOME/.local/bin
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

curl -fsSL --proto '=https,file' -o "$tmp/dot" "$base/$VERSION/dot-$platform" || die "download failed: $base/$VERSION/dot-$platform"
[ "$(sha256 "$tmp/dot")" = "$want" ] || die "invalid sha256 for dot-$platform, aborting"

# Copy next to the destination, then rename: dot is never half written.
mkdir -p "$dest"
# An executable already there is kept aside: if the update below fails, it comes back instead of
# a pinned bootstrap that may be older than it.
if [ -e "$dest/dot" ]; then cp -p "$dest/dot" "$dest/.dot.prev.$$" || die "cannot back up $dest/dot"; fi
cp "$tmp/dot" "$dest/.dot.$$"
chmod 755 "$dest/.dot.$$"
mv -f "$dest/.dot.$$" "$dest/dot"
echo "dot $VERSION installed in $dest/dot"
case ":$PATH:" in *":$dest:"*) ;; *) echo "add $dest to your PATH" ;; esac

# The pinned release is only the bootstrap: dot checks the Ed25519 signature of the latest release
# (public key compiled in) and its checksum before replacing itself. If that fails, the previous
# executable comes back, or the pinned binary (already verified above) stays on a first install.
# DOT_INSTALL_NO_UPDATE=1 skips it, for tests.
if [ "${DOT_INSTALL_NO_UPDATE:-}" != 1 ]; then
  if ! "$dest/dot" self-update; then
    if [ -e "$dest/.dot.prev.$$" ]; then
      mv -f "$dest/.dot.prev.$$" "$dest/dot"
      echo "dot update failed: the previous version is kept (run dot self-update to retry)" >&2
    else
      echo "dot update failed: $VERSION stays installed (run dot self-update to retry)" >&2
    fi
  fi
fi
rm -f "$dest/.dot.prev.$$"

if [ $# -gt 0 ]; then exec "$dest/dot" install "$@"; fi
