#!/usr/bin/env bash
# Pin a dot release in install.sh: download its SHA256SUMS and SHA256SUMS.sig, verify the signature
# with the committed public key (OpenSSL 3), then rewrite VERSION and the SHA256_<platform> lines.
#   scripts/pin-install.sh <vX.Y.Z>
# DOT_RELEASE_BASE replaces the download URL, for tests only.
set -euo pipefail

version=${1:?usage : scripts/pin-install.sh <vX.Y.Z>}
[[ $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "version invalide : $version" >&2; exit 2; }
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
base=${DOT_RELEASE_BASE:-https://github.com/fmatsos/dot/releases/download}/$version
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT

key=$(tr -d '[:space:]' <"$root/internal/selfupdate/release.pub")
[ -n "$key" ] || { echo "release.pub est vide : aucune clé de signature, abandon." >&2; exit 1; }
# Wrap the raw key in the fixed SubjectPublicKeyInfo header of Ed25519.
{ printf '\x30\x2a\x30\x05\x06\x03\x2b\x65\x70\x03\x21\x00'; printf %s "$key" | base64 -d; } >"$tmp/pub.der"
[ "$(wc -c <"$tmp/pub.der" | tr -d ' ')" = 44 ] || { echo "release.pub invalide (32 octets attendus)." >&2; exit 1; }
openssl pkey -pubin -inform DER -in "$tmp/pub.der" -out "$tmp/pub.pem"

for f in SHA256SUMS SHA256SUMS.sig; do
  curl -fsSL -o "$tmp/$f" "$base/$f" || { echo "téléchargement impossible : $base/$f" >&2; exit 1; }
done
openssl pkeyutl -verify -pubin -inkey "$tmp/pub.pem" -rawin -in "$tmp/SHA256SUMS" -sigfile "$tmp/SHA256SUMS.sig" >/dev/null ||
  { echo "signature de SHA256SUMS invalide : abandon." >&2; exit 1; }

cp "$root/install.sh" "$tmp/install.sh"
sed -i.bak "s/^VERSION=.*/VERSION=$version/" "$tmp/install.sh"
for p in linux-x64 linux-arm64 macos-x64 macos-arm64; do
  sum=$(awk -v n="dot-$p" '$2 == n || $2 == "*" n { print $1 }' "$tmp/SHA256SUMS")
  [[ $sum =~ ^[0-9a-f]{64}$ ]] || { echo "somme absente ou invalide pour dot-$p" >&2; exit 1; }
  var=SHA256_$(printf %s "$p" | tr '[:lower:]-' '[:upper:]_')
  sed -i.bak "s/^$var=.*/$var=$sum/" "$tmp/install.sh"
done
cat "$tmp/install.sh" >"$root/install.sh"
echo "install.sh figé sur $version (signature vérifiée)."
