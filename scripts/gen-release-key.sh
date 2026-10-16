#!/usr/bin/env bash
# Generate the Ed25519 release signing key pair.
#   scripts/gen-release-key.sh [private-key-file]     default: ./release-signing-key.pem
# Writes the public key (base64 of the raw 32 bytes, one line) to internal/selfupdate/release.pub,
# where it is compiled into the binary, and the private key (PEM, mode 600) to the given file.
# The private key content is never printed. Needs OpenSSL 3 (LibreSSL cannot sign raw Ed25519).
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
pub=$root/internal/selfupdate/release.pub
priv=${1:-release-signing-key.pem}

[ ! -s "$pub" ] || { echo "$pub already holds a key: replacing it would invalidate the published binaries. Empty it first if that is intended" >&2; exit 1; }
[ ! -e "$priv" ] || { echo "$priv already exists: aborting" >&2; exit 1; }

(umask 077 && openssl genpkey -algorithm ed25519 -out "$priv")
chmod 600 "$priv"
# The raw public key is the last 32 bytes of the DER SubjectPublicKeyInfo.
openssl pkey -in "$priv" -pubout -outform DER | tail -c 32 | base64 | tr -d '\n' >"$pub"
echo >>"$pub"

cat <<MSG
Public key written to $pub (to commit):
  $(cat "$pub")
Private key written to $priv (mode 600, never commit it).

To store it as a GitHub secret, then delete the file:
  gh secret set RELEASE_SIGNING_KEY --repo fmatsos/dot < "$priv"
  shred -u "$priv" 2>/dev/null || rm -P "$priv" 2>/dev/null || rm -f "$priv"
Keep a copy of the private key in your password manager before deleting it.
MSG
