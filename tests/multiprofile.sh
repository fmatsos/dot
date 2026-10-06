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
  done < <(find "$HOME/.dot/$p/home" -type f ! -name settings.base.json -print0)
  [ "$(readlink "$HOME/.local/bin/$p-tool")" = "$HOME/.dot/$p/bin/$p-tool" ]
  [ "$(git -C "$HOME/.dot/$p" config user.email)" = "$p@example.com" ]
done
[ "$(dot config get default)" = a ]
dot config list | grep -q '"b"'
[ ! -e "$HOME/.claude/settings.base.json" ] # a module source is no link once two profiles are registered
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

# push and status: every profile without -p, one with -p; each clone has its own bare remote.
rm -f "$HOME/.dot/s"; dot config unset profiles.s.repo
dot install "$A" -p a >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
for p in a b; do
  git init -q --bare -b main "$S/remotes/push-$p.git"
  git -C "$HOME/.dot/$p" remote set-url origin "$S/remotes/push-$p.git"
  git -C "$HOME/.dot/$p" push -q -u origin main 2>/dev/null
done
subject() { git -C "$S/remotes/push-$1.git" log -1 --format=%s; }
for p in a b; do printf 'edit\n' >>"$HOME/.dot/$p/home/.$p-new"; done
rm "$HOME/.a-rc" "$HOME/.b-rc"; printf 'tool\n' >"$HOME/.a-rc"; printf 'tool\n' >"$HOME/.b-rc"
dot status >"$S/out" 2>"$S/err"
grep -qx '==> a' "$S/out"; grep -qx '==> b' "$S/out"
section() { awk -v k="==> $1" '/^==> /{on = ($0 == k); next} on' "$S/out"; } # lines under one profile header
section a | grep -q 'M home/.a-new'; ! section a | grep -q 'home/.b-new'
section b | grep -q 'M home/.b-new'; ! section b | grep -q 'home/.a-new'
grep -qF '~/.a-rc' "$S/err"; grep -qF '~/.b-rc' "$S/err"
[ "$(dot st 2>/dev/null)" = "$(cat "$S/out")" ]
dot status -p a >"$S/out" 2>"$S/err"; ! grep -q '^==> ' "$S/out"; grep -q 'M home/.a-new' "$S/out"; ! grep -q 'home/.b-new' "$S/out"
DOT_PROFILE=b dot status >"$S/out" 2>&1; ! grep -q '^==> ' "$S/out"; grep -q 'M home/.b-new' "$S/out"
for p in a b; do ln -sf "$HOME/.dot/$p/home/.$p-rc" "$HOME/.$p-rc"; done
echo 'OK   status lists every profile under its name with its changes and detached files, or only the targeted one'

dot push "msg all" >"$S/out" 2>"$S/err" || { cat "$S/out" "$S/err"; exit 1; }
grep -qx '==> a' "$S/out"; grep -qx '==> b' "$S/out"
[ "$(subject a)" = "msg all" ]; [ "$(subject b)" = "msg all" ]
dot push >"$S/out" 2>&1; [ "$(grep -c '^rien à envoyer$' "$S/out")" -eq 2 ]
echo 'OK   push without -p commits and pushes every profile with the same message'

for p in a b; do printf 'edit\n' >>"$HOME/.dot/$p/home/.$p-new"; done
dot push -p a "only a" >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
! grep -q '^==> ' "$S/out"; [ "$(subject a)" = "only a" ]; [ "$(subject b)" = "msg all" ]
[ -n "$(git -C "$HOME/.dot/b" status --porcelain)" ]
echo 'OK   push -p a pushes that profile only'

# A profile whose push fails does not stop the others: exit 1, name reported.
mv "$S/remotes/push-b.git" "$S/remotes/push-b.away"
printf 'edit\n' >>"$HOME/.dot/a/home/.a-new"
fails 1 dot push "late"
[ "$(subject a)" = "late" ]; grep -qx 'échec : b' "$S/err"; ! grep -q 'échec : a' "$S/err"
mv "$S/remotes/push-b.away" "$S/remotes/push-b.git"
echo 'OK   push goes on after a failing profile and exits 1 naming it'

# send is gone: it falls back to git like any unknown word.
fails 1 dot send; grep -q "'send' is not a git command" "$S/err"
echo 'OK   dot send is no dot command any more (git fallback)'

# Multi-profile extensions: search all profiles when no -p, fail if claimed by two, run if by one.
mkdir -p "$HOME/.dot/a/bin" "$HOME/.dot/b/bin"
printf '#!/bin/sh\necho "a-only from profile a"\nexit 0\n' >"$HOME/.dot/a/bin/dot-a-only"; chmod +x "$HOME/.dot/a/bin/dot-a-only"
printf '#!/bin/sh\necho "both from a"\nexit 0\n' >"$HOME/.dot/a/bin/dot-shared"; chmod +x "$HOME/.dot/a/bin/dot-shared"
printf '#!/bin/sh\necho "both from b"\nexit 0\n' >"$HOME/.dot/b/bin/dot-shared"; chmod +x "$HOME/.dot/b/bin/dot-shared"
[ "$(dot a-only)" = "a-only from profile a" ] || { echo "FAIL: a-only extension"; exit 1; }
echo 'OK   extension only in profile a is run'
fails 1 dot shared
grep -q 'plusieurs profils' "$S/err" && grep -q '\ba\b' "$S/err" && grep -q '\bb\b' "$S/err" || { echo "FAIL: multi-profile error"; cat "$S/err"; exit 1; }
echo 'OK   extension claimed by two profiles is refused with profile names'
dot -p a shared >"$S/out" 2>&1; [ "$(cat "$S/out")" = "both from a" ] || { echo "FAIL: -p a should run a version"; exit 1; }
echo 'OK   with -p, extension is run from that profile even if others have it'
printf '#!/bin/sh\necho "from path"\nexit 0\n' >"$S/bin/dot-pathonly"; chmod +x "$S/bin/dot-pathonly"
[ "$(dot pathonly)" = "from path" ] || { echo "FAIL: path-only extension"; exit 1; }
echo 'OK   extension only in PATH is run when no profile has it'

# Both profiles provide the module sources (settings.base.json, servers.json): the install must
# work, nothing links them, and `dot settings` / `dot mcp` merge the two. Separate HOME and PATH.
(
  export HOME=$S/home2 XDG_CONFIG_HOME=$S/home2/.config XDG_DATA_HOME=$S/home2/.local/share XDG_CACHE_HOME=$S/home2/.cache
  export XDG_STATE_HOME=$S/home2/.local/state CODEX_HOME=$S/home2/.codex
  mkdir -p "$HOME" "$S/bin2"; cp "$S/bin/claude" "$S/bin2/claude"
  export PATH=$S/bin2:/usr/bin:/bin
  for n in p1 p2; do
    mkremote a "$S/remotes/$n"; rm -rf "$S/remotes/$n/bin"
    sed -i.bak "s/\"a\"/\"$n\"/g; s/a\.gitconfig/$n.gitconfig/; s/\"modules\": \[.*\]/\"modules\": [\"settings\", \"mcp\"]/" "$S/remotes/$n/dot.json"; rm "$S/remotes/$n/dot.json.bak"
    mv "$S/remotes/$n/home/.config/git/profiles/a.gitconfig" "$S/remotes/$n/home/.config/git/profiles/$n.gitconfig"
    rm -r "$S/remotes/$n/home/.a-rc" "$S/remotes/$n/home/.config/a"
    printf '{"%s-key":true}\n' "$n" >"$S/remotes/$n/home/.claude/settings.base.json"
    mkdir -p "$S/remotes/$n/home/.config/mcp"; printf '{"%s-srv":{"command":"%s-mcp"}}\n' "$n" "$n" >"$S/remotes/$n/home/.config/mcp/servers.json"
    commit "$S/remotes/$n"
  done
  dot install "$S/remotes/p1" -p p1 >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
  [ "$(readlink "$HOME/.claude/settings.base.json")" = "$HOME/.dot/p1/home/.claude/settings.base.json" ]
  [ "$(readlink "$HOME/.config/mcp/servers.json")" = "$HOME/.dot/p1/home/.config/mcp/servers.json" ]
  dot install "$S/remotes/p2" -p p2 >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
  [ "$(grep -c 'lien retiré' "$S/out")" -eq 2 ]
  [ ! -e "$HOME/.claude/settings.base.json" ] && [ ! -e "$HOME/.config/mcp/servers.json" ]
  jq -e '."p1-key" and ."p2-key"' "$HOME/.claude/settings.json" >/dev/null
  rm "$HOME/.claude/settings.json"; dot settings -n >"$S/out" 2>&1
  grep -q 'p1-key' "$S/out"; grep -q 'p2-key' "$S/out"; [ ! -e "$HOME/.claude/settings.json" ]
  dot mcp -n >"$S/out" 2>&1
  grep -Fxq 'claude : ajout p1-srv' "$S/out"; grep -Fxq 'claude : ajout p2-srv' "$S/out"
  # -p narrows the targets, not the fact that servers.json is no longer linked: apply must still find its source.
  dot mcp -p p1 >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
  ! grep -q 'invalide ou absent' "$S/out"
  dot install >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
  [ ! -e "$HOME/.claude/settings.base.json" ]; ! grep -q 'lien retiré' "$S/out"
  dot status >"$S/out" 2>"$S/err"; ! grep -q 'settings.base.json' "$S/err"
  echo 'OK   two profiles with the same module sources install, link neither and are merged by dot settings and dot mcp'

  dot config set default p1
  dot uninstall -p p2 >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
  [ ! -e "$HOME/.claude/settings.base.json" ] && [ ! -e "$HOME/.config/mcp/servers.json" ]
  grep -q 'un seul profil reste' "$S/out"; grep -q 'dot install' "$S/out"
  dot install >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
  [ "$(readlink "$HOME/.claude/settings.base.json")" = "$HOME/.dot/p1/home/.claude/settings.base.json" ]
  [ "$(readlink "$HOME/.config/mcp/servers.json")" = "$HOME/.dot/p1/home/.config/mcp/servers.json" ]
  echo 'OK   uninstalling one of two profiles does not relink the last one by itself, the message says to run dot install'
)
