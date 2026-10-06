#!/usr/bin/env bash
# dot adopt on two fictional profiles in an isolated HOME: happy path, dry run, and every refusal.
set -Eeuo pipefail
trap 'echo "FAIL adopt line $LINENO" >&2' ERR
root=$(cd "$(dirname "$0")/.." && pwd)
. "$root/tests/lib.sh" # builds DOT_BIN before HOME is redirected
S=$(cd "$(mktemp -d)" && pwd -P); trap 'rm -rf "$S"' EXIT
export HOME=$S/home GIT_CONFIG_GLOBAL=$S/gitconfig GIT_CONFIG_NOSYSTEM=1
export XDG_CONFIG_HOME=$HOME/.config XDG_DATA_HOME=$HOME/.local/share XDG_CACHE_HOME=$HOME/.cache
export XDG_STATE_HOME=$HOME/.local/state CODEX_HOME=$HOME/.codex DOTFILES_TOOLS=0
export DOT_BETTERLEAKS=$root/tests/fixtures/guard/fake-betterleaks
unset DOTFILES_DEPLOY DOT_PROFILE DOTFILES_FORBIDDEN DOTFILES_GUARD
mkdir -p "$HOME" "$S/bin" "$S/remotes" "$S/outside"
export PATH=$S/bin:$PATH
for cli in claude codex opencode bw pass-cli gh mise; do
  printf '#!/bin/bash\nexit 0\n' >"$S/bin/$cli"; chmod +x "$S/bin/$cli"
done
faketoken() { printf 'ghp_%s' "$(head -c 300 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 36)"; }

commit() { git -C "$1" add -A; git -C "$1" -c user.name=Fixture -c user.email=fixture@example.com commit -q -m "${2:-change}"; }
mkremote() { mkdir -p "$2"; cp -a "$root/tests/fixtures/install/$1/." "$2/"
  git -C "$2" init -q -b main; git -C "$2" config uploadpack.allowFilter true; commit "$2" fixture; }
fails() { local want=$1 code=0; shift; "$@" >"$S/out" 2>"$S/err" || code=$?; [ "$code" -eq "$want" ]; }
mkremote a "$S/remotes/a"; mkremote b "$S/remotes/b"
dot install "$S/remotes/a" -p a >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
dot install "$S/remotes/b" -p b >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
A=$HOME/.dot/a
printf '%s\n' acmecorp >"$A/forbidden.local"; printf '%s\n' globex >"$HOME/.dot/b/forbidden.local"

# Happy path: moved, linked with an absolute target, mode kept, nothing committed.
mkdir -p "$HOME/.config/foo"; printf 'hello\n' >"$HOME/.config/foo/rc"; chmod 640 "$HOME/.config/foo/rc"
printf 'x\n' >"$HOME/.plain"
dot adopt "$HOME/.config/foo/rc" >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
grep -qF 'adopt ~/.config/foo/rc -> a:home/.config/foo/rc' "$S/out"; grep -q 'dot push' "$S/out"
[ "$(readlink "$HOME/.config/foo/rc")" = "$A/home/.config/foo/rc" ]
[ "$(cat "$A/home/.config/foo/rc")" = hello ]; [ "$(stat -c %a "$A/home/.config/foo/rc" 2>/dev/null || stat -f %Lp "$A/home/.config/foo/rc")" = 640 ]
git -C "$A" status --short | grep -q '^?? home/.config/foo/'
dot install >"$S/out" 2>&1 || { cat "$S/out"; exit 1; } # the link is what install would make
[ "$(readlink "$HOME/.config/foo/rc")" = "$A/home/.config/foo/rc" ]
echo 'OK   adopt moves the file into the profile and links it back'

# -p picks another profile; -n writes nothing.
fails 0 dot adopt -n -p b "$HOME/.plain"; grep -qF 'adopt ~/.plain -> b:home/.plain' "$S/out"
[ -f "$HOME/.plain" ] && [ ! -L "$HOME/.plain" ]; [ ! -e "$HOME/.dot/b/home/.plain" ]
echo 'OK   -n prints the plan and writes nothing'
dot adopt -p b "$HOME/.plain" >/dev/null; [ -f "$HOME/.dot/b/home/.plain" ]; [ "$(readlink "$HOME/.plain")" = "$HOME/.dot/b/home/.plain" ]

# Refusals leave the file untouched and exit 1.
refused() { # refused <label> <file> [grep]: the file stays a regular file, the clone gets nothing
  local f=$1 why=${2:-}; local before; before=$(cksum <"$f")
  fails 1 dot adopt "$f"; [ -f "$f" ] && [ ! -L "$f" ]; [ "$(cksum <"$f")" = "$before" ]
  [ -z "$why" ] || grep -q "$why" "$S/err"
}
printf 'tenant AcmeCorp\n' >"$HOME/.term"
refused "$HOME/.term" 'référence interdite'; [ ! -e "$A/home/.term" ]
grep -qi acmecorp "$S/err" && exit 1
# A forbidden name refused earlier (existing destination) is not echoed in the reason either.
printf 'x\n' >"$HOME/.acmecorp-rc"; printf 'y\n' >"$A/home/.acmecorp-rc"
refused "$HOME/.acmecorp-rc" 'détail masqué'
grep -qi acmecorp "$S/err" && exit 1
rm -f "$A/home/.acmecorp-rc" "$HOME/.acmecorp-rc"
echo 'OK   a forbidden term refuses the file, which is untouched, and is never echoed'
tok=$(faketoken); printf 'key=%s\n' "$tok" >"$HOME/.secret"
refused "$HOME/.secret" 'secret'; [ ! -e "$A/home/.secret" ]
not grep -qF "$tok" "$S/err"
echo 'OK   a secret refuses the file'
fails 1 dot adopt "$HOME/.plain"; grep -q 'lien symbolique' "$S/err"
fails 1 dot adopt "$HOME/.config/foo/rc"; grep -q 'lien symbolique' "$S/err"
echo 'OK   a symlink, linked by dot or not, is refused'
printf 'o\n' >"$S/outside/f"; refused "$S/outside/f" 'hors de ~'
fails 1 dot adopt "$HOME/../outside/f"; grep -q 'hors de ~' "$S/err"
mkdir -p "$HOME/d"; fails 1 dot adopt "$HOME/d"; grep -q 'dossier' "$S/err"
echo 'OK   a file outside HOME, or a directory, is refused'
printf 'mine\n' >"$HOME/.exists"; mkdir -p "$A/home"; printf 'theirs\n' >"$A/home/.exists"
refused "$HOME/.exists" 'destination existante'; [ "$(cat "$A/home/.exists")" = theirs ]
echo 'OK   an existing destination is refused'
printf 'mine\n' >"$HOME/.b-only"; printf 'b\n' >"$HOME/.dot/b/home/.b-only"
refused "$HOME/.b-only" 'plusieurs profils'
echo 'OK   a file another profile already holds is refused'

# Without a term list, fail closed.
mv "$A/forbidden.local" "$S/forbidden.bak"; printf 'y\n' >"$HOME/.y"
refused "$HOME/.y" 'termes interdits'
mv "$S/forbidden.bak" "$A/forbidden.local"
echo 'OK   a missing term list refuses'

# A sparse set without home/ refuses.
sed -i.bak 's/"home", //' "$A/dot.json"; rm "$A/dot.json.bak"
refused "$HOME/.y" 'sparse'
echo 'OK   home/ outside the sparse set is refused'
