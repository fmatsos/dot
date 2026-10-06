#!/usr/bin/env bash
# home@<os>/ and home@<host>/ variants of home/: precedence, dry run, re-pointing, conflicts, adopt, uninstall.
# The host comes from DOT_HOSTNAME (test only); the OS is the real one, so the fixture has both OS variants.
set -Eeuo pipefail
trap 'echo "FAIL variants line $LINENO" >&2' ERR
root=$(cd "$(dirname "$0")/.." && pwd)
. "$root/tests/lib.sh" # builds DOT_BIN before HOME is redirected
S=$(mktemp -d); trap 'rm -rf "$S"' EXIT
export HOME=$S/home GIT_CONFIG_GLOBAL=$S/gitconfig GIT_CONFIG_NOSYSTEM=1
export XDG_CONFIG_HOME=$HOME/.config XDG_DATA_HOME=$HOME/.local/share XDG_CACHE_HOME=$HOME/.cache
export XDG_STATE_HOME=$HOME/.local/state CODEX_HOME=$HOME/.codex DOTFILES_TOOLS=0
export DOT_BETTERLEAKS=$root/tests/fixtures/guard/fake-betterleaks
unset DOTFILES_DEPLOY DOT_PROFILE DOTFILES_FORBIDDEN DOTFILES_GUARD DOT_HOSTNAME
mkdir -p "$HOME" "$S/bin" "$S/remotes"
export PATH=$S/bin:$PATH
for cli in claude codex opencode bw pass-cli gh mise; do
  printf '#!/bin/bash\nexit 0\n' >"$S/bin/$cli"; chmod +x "$S/bin/$cli"
done
commit() { git -C "$1" add -A; git -C "$1" -c user.name=Fixture -c user.email=fixture@example.com commit -q -m "${2:-change}"; }
mkremote() { mkdir -p "$2"; cp -a "$root/tests/fixtures/variants/$1/." "$2/"
  git -C "$2" init -q -b main; git -C "$2" config uploadpack.allowFilter true; commit "$2" fixture; }
fails() { local want=$1 code=0; shift; "$@" >"$S/out" 2>"$S/err" || code=$?; [ "$code" -eq "$want" ] || { echo "exit $code: $*" >&2; cat "$S/err" >&2; return 1; }; }

os=$(uname | tr '[:upper:]' '[:lower:]'); case $os in darwin) other=linux ;; *) os=linux; other=darwin ;; esac
V=$HOME/.dot/v
mkremote v "$S/remotes/v"; mkremote w "$S/remotes/w"
export DOT_HOSTNAME=Laptop.example.com

dot install "$S/remotes/v" -p v >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
[ "$(readlink "$HOME/.shared")" = "$V/home@laptop/.shared" ]
[ "$(readlink "$HOME/.base")" = "$V/home/.base" ]
[ "$(readlink "$HOME/.$os-only")" = "$V/home@$os/.$os-only" ]
[ "$(readlink "$HOME/.laptop-only")" = "$V/home@laptop/.laptop-only" ]
for gone in ".$other-only" .desk-only; do [ ! -e "$HOME/$gone" ] && [ ! -L "$HOME/$gone" ]; done
echo 'OK   host beats OS beats home/, and other systems or hosts are not linked'
rm -f "$HOME/.shared" "$HOME/.base" "$HOME/.$os-only"
rm -f "$HOME/.shared" "$HOME/.base"
dot install -n >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
grep -qF "  lien : ~/.$os-only (home@$os)" "$S/out"
grep -qxF "  lien : ~/.base" "$S/out" || { cat "$S/out"; exit 1; }
grep -qxF "  lien : ~/.base" "$S/out" || { cat "$S/out"; exit 1; }
[ ! -e "$HOME/.shared" ]
if grep -qE "desk|$other" "$S/out"; then cat "$S/out"; exit 1; fi
echo 'OK   dry run shows the winning layer only when it is not home/'
dot install >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }

dot st >"$S/out" 2>"$S/err"; [ ! -s "$S/err" ]
dot doctor >"$S/out" 2>&1 || true; grep -q 'liens home à jour' "$S/out"
echo 'OK   status and doctor see no detached file with variants in place'

# A variant file deleted from the host layer: the link falls back to the OS layer, nothing is backed up.
git -C "$V" rm -q home@laptop/.shared home@laptop/.laptop-only; commit "$V" 'drop host variants'
dot install >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
[ "$(readlink "$HOME/.shared")" = "$V/home@$os/.shared" ]
[ ! -L "$HOME/.laptop-only" ] && [ ! -e "$HOME/.laptop-only" ]
if grep -q sauvegarde "$S/out"; then cat "$S/out"; exit 1; fi
echo 'OK   a link into a variant that stopped winning is re-pointed or removed, never backed up'

# Another host: the layer of the previous one is left, its links re-pointed or removed.
git -C "$V" revert --no-edit HEAD >/dev/null
dot install >/dev/null 2>&1
[ "$(readlink "$HOME/.shared")" = "$V/home@laptop/.shared" ]
DOT_HOSTNAME=desk dot install >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
[ "$(readlink "$HOME/.shared")" = "$V/home@desk/.shared" ]
[ "$(readlink "$HOME/.desk-only")" = "$V/home@desk/.desk-only" ]
[ ! -L "$HOME/.laptop-only" ] && [ ! -e "$HOME/.laptop-only" ]
echo 'OK   changing the host name switches the layer'

# A host named like an OS, or an invalid one, gets no host layer and no error.
for h in linux darwin 'Bad_Name' ''; do
  DOT_HOSTNAME=$h dot install >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
  [ "$(readlink "$HOME/.shared")" = "$V/home@$os/.shared" ]
done
echo 'OK   an OS-like or invalid host name means no host layer'

# Two profiles providing the same ~ file here conflict; one that only has it for the other OS does not.
export DOT_HOSTNAME=laptop
dot install "$S/remotes/w" -p w >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
[ "$(readlink "$HOME/.w$os")" = "$HOME/.dot/w/home@$os/.w$os" ]
# Uncommitted files: the clone is what counts. The other OS' variant is not on this machine.
mkdir -p "$HOME/.dot/w/home@$other"; echo w >"$HOME/.dot/w/home@$other/.shared"
dot install >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
echo w >"$HOME/.dot/w/home@$os/.shared"
fails 1 dot install
grep -q 'fichier lié par plusieurs profils' "$S/err"; grep -qF "~/.shared : v ($V/home@laptop/.shared) w ($HOME/.dot/w/home@$os/.shared)" "$S/err"
echo 'OK   conflicts are computed on the resolved layers'
rm "$HOME/.dot/w/home@$os/.shared" "$HOME/.dot/w/home@$other/.shared"
dot install >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }

# A pull that adds a variant to deploy.sparse: its files are checked out before the conflict check.
mkdir -p "$S/remotes/w/home@laptop"; echo w >"$S/remotes/w/home@laptop/.laptop-only"; commit "$S/remotes/w" 'laptop file'
dot pull -p w >"$S/out" 2>&1 || { cat "$S/out"; exit 1; } # not in the sparse set yet: nothing to link
sed -i.bak 's/"home@darwin", ".githooks"/"home@darwin", "home@laptop", ".githooks"/' "$S/remotes/w/dot.json"
rm "$S/remotes/w/dot.json.bak"; commit "$S/remotes/w" 'sparse laptop'
fails 1 dot pull -p w
grep -qF "~/.laptop-only : v ($V/home@laptop/.laptop-only) w ($HOME/.dot/w/home@laptop/.laptop-only)" "$S/err"
[ "$(readlink "$HOME/.laptop-only")" = "$V/home@laptop/.laptop-only" ]
git -C "$S/remotes/w" rm -rq home@laptop; commit "$S/remotes/w" 'drop laptop file'
dot pull -p w >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
echo 'OK   a variant added to deploy.sparse by a pull is checked for conflicts before linking'

# adopt: --os and --host target the variant directories, and are exclusive.
printf 'mine\n' >"$HOME/.adopted-os"; printf 'mine\n' >"$HOME/.adopted-host"
printf '%s\n' acmecorp >"$V/forbidden.local"
dot adopt --os "$HOME/.adopted-os" >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
grep -qF "adopt ~/.adopted-os -> v:home@$os/.adopted-os" "$S/out"
[ "$(readlink "$HOME/.adopted-os")" = "$V/home@$os/.adopted-os" ]
dot adopt --host "$HOME/.adopted-host" >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
grep -qF 'adopt ~/.adopted-host -> v:home@laptop/.adopted-host' "$S/out"
[ "$(readlink "$HOME/.adopted-host")" = "$V/home@laptop/.adopted-host" ]
printf 'x\n' >"$HOME/.both"; fails 2 dot adopt --os --host "$HOME/.both"; [ -f "$HOME/.both" ] && [ ! -L "$HOME/.both" ]
DOT_HOSTNAME=linux fails 1 dot adopt --host "$HOME/.both"; grep -q 'nom de machine' "$S/err"
# A path a higher layer already gives would never be linked from the lower one: refused.
printf 'host\n' >"$V/home@laptop/.shadowed"; printf 'mine\n' >"$HOME/.shadowed"
fails 1 dot adopt "$HOME/.shadowed"; grep -q 'masqué par v:home@laptop/.shadowed' "$S/err"
fails 1 dot adopt --os "$HOME/.shadowed"; [ ! -L "$HOME/.shadowed" ] && [ ! -e "$V/home/.shadowed" ]
rm -f "$V/home@laptop/.shadowed" "$HOME/.shadowed"
echo 'OK   adopt --os and --host file into the variants, refuse to be combined or shadowed'

# Uninstall removes the links into every layer, the inactive ones included.
ln -s "$V/home@desk/.desk-only" "$HOME/.desk-only"
dot config set default w >/dev/null
dot uninstall -p v >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
for f in .shared .base ".$os-only" .desk-only .adopted-os .adopted-host; do [ ! -L "$HOME/$f" ] && [ ! -e "$HOME/$f" ]; done
echo 'OK   uninstall removes the links into every layer of the profile'
