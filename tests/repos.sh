#!/usr/bin/env bash
# Fictional origins, local bare remotes and isolated git profiles; no network.
# shellcheck disable=SC2088 # Literal ~ in expected display paths and git includes.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
source "$root/tests/lib.sh"
S=$(cd "$(mktemp -d)" && pwd -P); trap 'rm -rf "$S"' EXIT
export HOME=$S/home DOT_SRC="$S/home/source repos" DOT_SANDBOX=$S/home/sandbox
export DOTFILES_DEPLOY=$S/deploy GIT_CONFIG_GLOBAL=$S/gitconfig GIT_CONFIG_NOSYSTEM=1
mkdir -p "$HOME/.config/git/profiles" "$DOTFILES_DEPLOY" "$S/remotes/team"
git config --global user.name Tester
git config --global user.email tester@example.com
git config --global url."file://$S/remotes/".insteadOf https://git.example.com/
git init -qb main "$S/seed"
printf 'fixture\n' >"$S/seed/marker"
git -C "$S/seed" add marker; git -C "$S/seed" commit -qm fixture
for name in clean dirty ahead; do
  git clone -q --bare "$S/seed" "$S/remotes/team/$name.git"
  "$DOT_BIN" clone "https://git.example.com/team/$name.git" >"$S/out" 2>"$S/err"
done
base=$DOT_SRC/git.example.com/team
cat >"$HOME/.config/git/profiles/perso.gitconfig" <<'PROFILE'
[user]
  email = tester@example.com
[dotfiles]
  profile = perso
[core]
  hooksPath = ~/.config/git/hooks
PROFILE
for name in clean dirty; do
  git config --global --add "includeIf.gitdir:$base/$name/.path" "$HOME/.config/git/profiles/perso.gitconfig"
done
git -C "$base/dirty" config user.email other@example.com
printf 'untracked\n' >"$base/dirty/new file"
printf 'ahead\n' >>"$base/ahead/marker"
git -C "$base/ahead" commit -qam ahead
git -C "$base/clean" worktree add -qb side "$base/clean.wt/side"
git -C "$base/clean" worktree add -qb linked "$base/linked"
ln -s "$base/clean" "$base/mirror"
mkdir -p "$base/plain"
git init -q "$DOT_SANDBOX/scratch"
repos() { "$DOT_BIN" repos "$@"; }
out=$(repos)
[ "$(wc -l <<<"$out")" -eq 4 ]
grep -q '~/source repos/git.example.com/team/clean .*✎0.*⇡0 ⇣0.*perso.*tester@example.com' <<<"$out"
grep -q '/dirty .*✎1.*✗ email différent du profil perso' <<<"$out"
grep -q '/ahead .*⇡1 ⇣0' <<<"$out"
grep -q '~/sandbox/scratch ' <<<"$out"
! grep -qE '/(clean.wt|linked|plain|mirror)' <<<"$out" || exit 1
echo 'OK   overview: clean, dirty, ahead, sandbox; worktrees and plain directories excluded'
code=0
out=$(repos --problems) || code=$?
[ "$code" -eq 1 ] && [ "$(wc -l <<<"$out")" -eq 1 ] && grep -q '/dirty ' <<<"$out"
echo 'OK   profile mismatch: only the wrong identity is flagged, exit 1'
git -C "$base/dirty" config user.email tester@example.com
[ -z "$(repos --problems)" ]
git -C "$base/clean" checkout -q --detach
grep -q '/clean .*détaché' <<<"$(repos)"
echo 'OK   corrected identity and detached branch'
git -C "$base/clean" config core.hooksPath .husky
code=0; repos --problems >"$S/out" || code=$?
[ "$code" -eq 1 ]; grep -q '/clean .*✗ hooksPath local : garde-fou contourné' "$S/out"
git -C "$base/clean" config user.email other@example.com
code=0; repos --problems >"$S/out" || code=$?
[ "$code" -eq 1 ]; grep -q '✗ email différent du profil perso ; ✗ hooksPath local' "$S/out"
git -C "$base/clean" config user.email tester@example.com
git -C "$base/clean" config core.hooksPath .githooks
mkdir -p "$base/clean/.githooks"
printf '#!/bin/bash\nexit 0\n' >"$base/clean/.githooks/guard"
chmod +x "$base/clean/.githooks/guard"
[ -z "$(repos --problems)" ]
git -C "$base/clean" config --unset core.hooksPath
echo 'OK   F12: local hooks bypass flagged, reasons combined, executable guard excepted'
cat >"$HOME/.config/git/profiles/demo.gitconfig" <<'PROFILE'
[user]
  email = tester@example.com
[dotfiles]
  profile = demo
PROFILE
git config --global --add "includeIf.gitdir:$DOT_SANDBOX/scratch/.path" "$HOME/.config/git/profiles/demo.gitconfig"
git -C "$DOT_SANDBOX/scratch" config core.hooksPath .husky
[ -z "$(repos --problems)" ]
echo 'OK   F12: profile without hooksPath leaves local hooks unflagged'
# Export replaces an existing non-private file atomically.
printf 'old\n' >"$DOTFILES_DEPLOY/repos.local"; chmod 644 "$DOTFILES_DEPLOY/repos.local"
out=$(repos export)
[ "$(stat -c '%a' "$DOTFILES_DEPLOY/repos.local" 2>/dev/null || stat -f %Lp "$DOTFILES_DEPLOY/repos.local")" = 600 ]
for name in ahead clean dirty; do printf 'https://git.example.com/team/%s.git\n' "$name"; done >"$S/expected"
cmp -s "$S/expected" "$DOTFILES_DEPLOY/repos.local"
! grep -qE 'https://|file://' <<<"$out" || exit 1
grep -q '^3 remotes' <<<"$out"
echo 'OK   sorted origin export, private mode, count only; sandbox excluded'
export DOT_SRC="$S/fresh repos"
repos clone >"$S/out" 2>"$S/err"
grep -q '^3 dépôts réutilisés ou clonés, 0 échecs' "$S/out"
for name in clean dirty ahead; do [ -f "$DOT_SRC/git.example.com/team/$name/marker" ]; done
repos clone >"$S/out" 2>"$S/err"
grep -q '^3 dépôts réutilisés ou clonés, 0 échecs' "$S/out"
echo 'OK   clone uses dot clone, local URL rewrite and idempotent second run'
{ printf '\n# comment\ninvalid-url\n'; cat "$S/expected"; } >"$S/list"
code=0; repos clone "$S/list" >"$S/out" 2>"$S/err" || code=$?
[ "$code" -eq 1 ]; grep -q '^3 dépôts réutilisés ou clonés, 1 échecs' "$S/out"
! grep -qE 'https://|file://|invalid-url' "$S/out" "$S/err" || exit 1
echo 'OK   clone skips comments and blanks, continues after failures, no URLs printed'
printf 'bad-url\nhttps://git.example.com/team/unreachable.git\nfile:///fictional/missing.git\n' >"$S/list"
code=0; repos clone "$S/list" >"$S/out" 2>"$S/err" || code=$?
[ "$code" -eq 1 ]; grep -Fxq "échec : $DOT_SRC/git.example.com/team/unreachable" "$S/err"
grep -Fxq 'échec : (URL invalide)' "$S/err"
! grep -qE 'https://|file://|bad-url' "$S/out" "$S/err" || exit 1
echo 'OK   F11: failed destinations reported without URLs'
for cmd in export clone; do grep -q "^usage : dot repos $cmd " <<<"$(repos "$cmd" --help)"; done
grep -q '^usage : dot repos \[--problems\]' <<<"$(repos --help)"
[ "$(dot help repos export)" = "$(repos export --help)" ]
echo 'OK   repos help, including each subcommand'
for args in nope "export a b" "clone a b" "--problems extra"; do
  code=0; repos $args >"$S/out" 2>"$S/err" || code=$?
  [ "$code" -eq 2 ]; grep -q '^usage : dot repos ' "$S/err"
done
echo 'OK   repos invalid usage rejected with exit 2'
