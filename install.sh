#!/bin/sh
# Bootstrap dot: download the pinned release, check its sha256, install it, update it to the latest
# signed release, then install a profile.
#   sh install.sh [<profile url> [dot install options]]
# VERSION and the sums are rewritten by scripts/pin-install.sh; they only need a bump when the
# bootstrap itself must change (self-update broken, signing key rotated), not at every release.
set -eu

VERSION=v0.2.0
SHA256_LINUX_X64=0d01029532e1e10a6b26650533b606b80593b931f6ba10a64430c2494f9e3c44
SHA256_LINUX_ARM64=8792ff887873b9f5fc6954013d89d4cea92c52d45cbb34437e4e6140cee0e5dc
SHA256_MACOS_X64=a3c4c237b2ef6a1f3964a41dad36fc3e7f953792528c1f303995f3a7fa6435a0
SHA256_MACOS_ARM64=f80958c0c7e59dcc2af7e4cbf3ee73c65cab4bb631b1003e21c822dacbe7c946

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
