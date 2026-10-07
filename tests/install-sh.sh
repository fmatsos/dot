#!/usr/bin/env bash
# install.sh, scripts/pin-install.sh and the fail-closed self-update, offline (file:// releases, throwaway key).
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
source "$root/tests/lib.sh"
S=$(cd "$(mktemp -d)" && pwd -P); trap 'rm -rf "$S"' EXIT
fail() { echo "FAIL install-sh: $*" >&2; exit 1; }

sh -n "$root/install.sh" || fail "syntax"

# The committed script is pinned to a release: the tests start from an unpinned copy of it.
sed -E 's/^(VERSION|SHA256_[A-Z0-9_]+)=.*/\1=PLACEHOLDER/' "$root/install.sh" >"$S/install.unpinned.sh"
grep -q PLACEHOLDER "$S/install.unpinned.sh" || fail "unpinned copy"

# An unpinned script refuses to install anything.
out=$(HOME=$S/h0 sh "$S/install.unpinned.sh" 2>&1) && fail "placeholder accepted"
case $out in *"non figé"*) ;; *) fail "placeholder message: $out" ;; esac
[ ! -e "$S/h0" ] || fail "placeholder wrote files"

# Self-update without a compiled-in key is refused, dry run included. Once the real public key is
# committed the binary has one and would go to the network: the Go tests cover that path.
if [ ! -s "$root/internal/selfupdate/release.pub" ]; then
  for args in "" "-n --version v9.9.9"; do
    # shellcheck disable=SC2086
    out=$(HOME=$S/h0 dot self-update $args 2>&1) && fail "self-update without key succeeded"
    case $out in *"clé de signature"*) ;; *) fail "self-update message: $out" ;; esac
  done
fi

command -v openssl >/dev/null && openssl genpkey -algorithm ed25519 -out "$S/k.pem" 2>/dev/null || { echo "install-sh: openssl 3 absent, partie pin ignorée"; exit 0; }

# A fake release signed with a throwaway key, in a copy of the repo scripts.
tag=v9.9.9
rel=$S/rel/$tag; mkdir -p "$rel" "$S/repo/scripts" "$S/repo/internal/selfupdate"
cp "$S/install.unpinned.sh" "$S/repo/install.sh"; cp "$root/scripts/pin-install.sh" "$S/repo/scripts/"
openssl pkey -in "$S/k.pem" -pubout -outform DER | tail -c 32 | base64 | tr -d '\n' >"$S/repo/internal/selfupdate/release.pub"
for p in linux-x64 linux-arm64 macos-x64 macos-arm64; do printf '#!/bin/sh\necho fake-dot "$@" >>"$HOME/ran"\n[ "$1" = self-update ] && [ -e "$HOME/update-fails" ] && exit 1\nexit 0\n' >"$rel/dot-$p"; done
(cd "$rel" && sha256sum dot-* >SHA256SUMS 2>/dev/null || shasum -a 256 dot-* >SHA256SUMS)
openssl pkeyutl -sign -rawin -inkey "$S/k.pem" -in "$rel/SHA256SUMS" -out "$rel/SHA256SUMS.sig"

export DOT_RELEASE_BASE=file://$S/rel
"$S/repo/scripts/pin-install.sh" "$tag" >/dev/null || fail "pin-install"
grep -q "^VERSION=$tag$" "$S/repo/install.sh" || fail "VERSION not pinned"
! grep -q PLACEHOLDER "$S/repo/install.sh" || fail "placeholder left"

mkdir "$S/h1"
HOME=$S/h1 DOT_INSTALL_BASE=file://$S/rel sh "$S/repo/install.sh" https://example.invalid/p.git >/dev/null || fail "install.sh run"
[ -x "$S/h1/.local/bin/dot" ] || fail "dot not installed"
[ "$(cat "$S/h1/ran")" = "fake-dot self-update
fake-dot install https://example.invalid/p.git" ] || fail "self-update then profile url not run: $(cat "$S/h1/ran")"

# A failing self-update keeps the verified pinned binary and the install goes on.
mkdir "$S/h3"; touch "$S/h3/update-fails"
HOME=$S/h3 DOT_INSTALL_BASE=file://$S/rel sh "$S/repo/install.sh" https://example.invalid/p.git >/dev/null 2>"$S/err3" || fail "failing self-update stopped the install"
grep -q "mise à jour de dot impossible" "$S/err3" || fail "failed update not reported"
[ "$(tail -n 1 "$S/h3/ran")" = "fake-dot install https://example.invalid/p.git" ] || fail "profile not installed after a failed update"

# A failing self-update puts back the executable that was already there: no downgrade.
mkdir -p "$S/h5/.local/bin"; touch "$S/h5/update-fails"
printf '#!/bin/sh\necho old-dot "$@" >>"$HOME/ran"\nexit 0\n' >"$S/h5/.local/bin/dot"; chmod 755 "$S/h5/.local/bin/dot"
HOME=$S/h5 DOT_INSTALL_BASE=file://$S/rel sh "$S/repo/install.sh" https://example.invalid/p.git >/dev/null 2>"$S/err5" || fail "previous binary run"
grep -q "version précédente est conservée" "$S/err5" || fail "restore not reported: $(cat "$S/err5")"
grep -q old-dot "$S/h5/.local/bin/dot" || fail "previous executable not restored"
[ "$(tail -n 1 "$S/h5/ran")" = "old-dot install https://example.invalid/p.git" ] || fail "install not run by the previous binary"
[ "$(ls -A "$S/h5/.local/bin")" = dot ] || fail "temporary copies left"

# DOT_INSTALL_NO_UPDATE=1 skips the update.
mkdir "$S/h4"
HOME=$S/h4 DOT_INSTALL_NO_UPDATE=1 DOT_INSTALL_BASE=file://$S/rel sh "$S/repo/install.sh" https://example.invalid/p.git >/dev/null || fail "no-update run"
[ "$(cat "$S/h4/ran")" = "fake-dot install https://example.invalid/p.git" ] || fail "update not skipped"

# A tampered binary is refused and nothing is installed.
echo evil >>"$rel/dot-linux-x64"; echo evil >>"$rel/dot-linux-arm64"; echo evil >>"$rel/dot-macos-x64"; echo evil >>"$rel/dot-macos-arm64"
mkdir "$S/h2"
HOME=$S/h2 DOT_INSTALL_BASE=file://$S/rel sh "$S/repo/install.sh" >/dev/null 2>&1 && fail "tampered binary accepted"
[ ! -e "$S/h2/.local/bin/dot" ] || fail "tampered binary installed"

# A forged SHA256SUMS fails the signature check in pin-install.
echo "$(printf '0%.0s' {1..64})  dot-linux-x64" >>"$rel/SHA256SUMS"
cp "$S/install.unpinned.sh" "$S/repo/install.sh"
"$S/repo/scripts/pin-install.sh" "$tag" >/dev/null 2>&1 && fail "forged sums accepted"
grep -q PLACEHOLDER "$S/repo/install.sh" || fail "install.sh modified after failed verification"
echo "ok install-sh"
