#!/usr/bin/env bash
# Two fictional profiles side by side: links, conflicts, pull, uninstall, in an isolated HOME.
set -Eeuo pipefail
trap 'echo "FAIL multiprofile line $LINENO" >&2' ERR
root=$(cd "$(dirname "$0")/.." && pwd)
. "$root/tests/lib.sh" # builds DOT_BIN before HOME is redirected
S=$(mktemp -d); trap 'rm -rf "$S"' EXIT
export HOME=$S/home GIT_CONFIG_GLOBAL=$S/gitconfig GIT_CONFIG_NOSYSTEM=1
export XDG_CONFIG_HOME=$HOME/.config XDG_DATA_HOME=$HOME/.local/share XDG_CACHE_HOME=$HOME/.cache
export XDG_STATE_HOME=$HOME/.local/state CODEX_HOME=$HOME/.codex DOTFILES_TOOLS=0
unset DOTFILES_DEPLOY DOT_PROFILE DOTFILES_BACKUP_DAYS
mkdir -p "$HOME" "$S/bin" "$S/remotes"
export PATH=$S/bin:$PATH
for cli in claude codex opencode bw pass-cli gh mise; do
  printf '#!/bin/bash\nexit 0\n' >"$S/bin/$cli"; chmod +x "$S/bin/$cli"
done

commit() { git -C "$1" add -A; git -C "$1" -c user.name=Fixture -c user.email=fixture@example.com commit -q -m "${2:-change}"; }
mkremote() { # mkremote <fixture> <dir>
  mkdir -p "$2"; cp -a "$root/tests/fixtures/install/$1/." "$2/"
  git -C "$2" init -q -b main; git -C "$2" config uploadpack.allowFilter true; commit "$2" fixture
}
fails() { local want=$1 code=0; shift; "$@" >"$S/out" 2>"$S/err" || code=$?; [ "$code" -eq "$want" ]; }
registry=$HOME/.dot/profiles.json
A=$S/remotes/a; B=$S/remotes/b; mkremote a "$A"; mkremote b "$B"

dot install "$A" -p a >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
dot install "$B" -p b >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
for p in a b; do
  while IFS= read -r -d '' f; do
    [ "$(readlink "$HOME/${f#"$HOME/.dot/$p/home/"}")" = "$f" ]
  done < <(find "$HOME/.dot/$p/home" -type f -print0)
  [ "$(readlink "$HOME/.local/bin/$p-tool")" = "$HOME/.dot/$p/bin/$p-tool" ]
  [ "$(git -C "$HOME/.dot/$p" config user.email)" = "$p@example.com" ]
done
[ "$(dot config get default)" = a ]
dot config list | grep -q '"b"'
dot install >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
[ "$(grep -c '^==> ' "$S/out")" -eq 2 ]; [ "$(tail -n1 "$S/out")" = ok ]
echo 'OK   two profiles install side by side, the first stays the default, a bare install redoes both'

# A file claimed by two profiles: refused with both paths, nothing written, nothing registered.
mkremote b "$S/remotes/c"
sed -i.bak 's/"b"/"c"/g; s/b\.gitconfig/c.gitconfig/' "$S/remotes/c/dot.json"; rm "$S/remotes/c/dot.json.bak"
mv "$S/remotes/c/home/.config/git/profiles/b.gitconfig" "$S/remotes/c/home/.config/git/profiles/c.gitconfig"
rm -r "$S/remotes/c/bin" "$S/remotes/c/home/.b-rc" "$S/remotes/c/home/.config/b"
mkdir -p "$S/remotes/c/home/.config/c"; printf 'conf of c\n' >"$S/remotes/c/home/.config/c/app.conf"
cp "$registry" "$S/registry.before"; ls -A "$HOME" >"$S/home.before"
printf 'claimed twice\n' >"$S/remotes/c/home/.a-rc"; commit "$S/remotes/c"
fails 1 dot install "$S/remotes/c" -p c
grep -q 'fichier lié par plusieurs profils' "$S/err"
grep -qF "$HOME/.dot/a/home/.a-rc" "$S/err"; grep -qF "$HOME/.dot/c/home/.a-rc" "$S/err"
cmp -s "$S/registry.before" "$registry"; [ ! -e "$HOME/.dot/c" ]
[ ! -e "$HOME/.config/c" ]; [ "$(readlink "$HOME/.a-rc")" = "$HOME/.dot/a/home/.a-rc" ]
[ "$(ls -A "$HOME")" = "$(cat "$S/home.before")" ]
echo 'OK   a file claimed by two profiles refuses the install with both paths and writes nothing'
git -C "$S/remotes/c" rm -q --cached home/.a-rc; rm "$S/remotes/c/home/.a-rc"
mkdir "$S/remotes/c/bin"; printf '#!/bin/sh\n' >"$S/remotes/c/bin/a-tool"; chmod +x "$S/remotes/c/bin/a-tool"
commit "$S/remotes/c"
fails 1 dot install "$S/remotes/c" -p c
grep -qF "$HOME/.dot/c/bin/a-tool" "$S/err"; grep -qF "$HOME/.dot/a/bin/a-tool" "$S/err"
cmp -s "$S/registry.before" "$registry"; [ ! -e "$HOME/.dot/c" ]
echo 'OK   two profiles with the same bin/ name are refused too'

# dot pull: every profile, each with its name, then the links of new files.
printf 'new in a\n' >"$A/home/.a-new"; commit "$A"
printf 'new in b\n' >"$B/home/.b-new"; commit "$B"
dot pull >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
[ "$(readlink "$HOME/.a-new")" = "$HOME/.dot/a/home/.a-new" ]
[ "$(readlink "$HOME/.b-new")" = "$HOME/.dot/b/home/.b-new" ]
grep -qx '==> a' "$S/out"; grep -qx '==> b' "$S/out"; [ "$(tail -n1 "$S/out")" = ok ]
printf 'again\n' >"$A/home/.a-two"; commit "$A"; printf 'again\n' >"$B/home/.b-two"; commit "$B"
dot pull -p a >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
[ -L "$HOME/.a-two" ]; [ ! -e "$HOME/.b-two" ]; ! grep -q '^==> ' "$S/out"
echo 'OK   pull updates every profile, or only the targeted one'

# A failing profile does not stop the others; the exit code is 1 and the name is reported.
mv "$B" "$S/remotes/b.away"
printf 'late\n' >"$A/home/.a-three"; commit "$A"
fails 1 dot pull
[ -L "$HOME/.a-three" ]; grep -q 'pull b' "$S/err"; grep -q 'échec : b' "$S/err"
mv "$S/remotes/b.away" "$B"
dot pull >"$S/out" 2>&1; [ -L "$HOME/.b-two" ]
echo 'OK   pull goes on after a failing profile and exits 1 naming it'

# uninstall: the default cannot leave first; links go, the clone stays; --purge guards.
fails 2 dot uninstall
fails 1 dot uninstall -p a; grep -q 'profil par défaut' "$S/err"; [ -L "$HOME/.a-rc" ]
dot config set default b
dot uninstall -p a >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
[ ! -e "$HOME/.a-rc" ] && [ ! -e "$HOME/.local/bin/a-tool" ] && [ ! -e "$HOME/.config/a" ]
[ -L "$HOME/.b-rc" ] && [ -L "$HOME/.local/bin/b-tool" ]
[ -d "$HOME/.dot/a/home" ]; [ "$(dot config get default)" = b ]
fails 1 dot config get profiles.a.repo
echo 'OK   uninstall -p a removes its links, keeps the clone and leaves the other profile alone'
dot install >"$S/out" 2>&1; [ ! -e "$HOME/.a-rc" ]
dot install "$A" -p a >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
[ -L "$HOME/.a-rc" ]; [ "$(dot config get default)" = b ]
echo 'OK   an existing clone is reused when its profile is installed again'

printf 'wip\n' >"$HOME/.dot/a/wip.txt"
fails 1 dot uninstall -p a --purge; grep -q 'changements non commités' "$S/err"
[ -d "$HOME/.dot/a" ]; [ -L "$HOME/.a-rc" ]; dot config get profiles.a.repo >/dev/null
rm "$HOME/.dot/a/wip.txt"
dot uninstall -p a --purge >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
[ ! -e "$HOME/.dot/a" ] && [ ! -e "$HOME/.a-rc" ]
mkdir "$S/elsewhere"; ln -s "$S/elsewhere" "$HOME/.dot/s"; dot config set profiles.s.repo "$S/remotes/s"
fails 1 dot uninstall -p s --purge; grep -q 'lien' "$S/err"; [ -d "$S/elsewhere" ]
dot install "$A" -p a >"$S/out" 2>&1; printf 'wip\n' >"$HOME/.dot/a/wip.txt"
dot uninstall -p a --purge --force >"$S/out" 2>&1; [ ! -e "$HOME/.dot/a" ]
[ "$(dot config get default)" = b ]; [ -d "$HOME/.dot/b" ]
echo 'OK   --purge deletes the clone, but refuses a link or uncommitted work (unless --force)'
