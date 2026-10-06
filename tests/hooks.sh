#!/usr/bin/env bash
# Self-check of the global pre-push hook with a stub `dot`, local pushes and an isolated HOME.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=tests/lib.sh
. "$root/tests/lib.sh"
fx=$root/tests/fixtures/guard
hook=$fx/git-hooks/pre-push
S=$(cd "$(mktemp -d)" && pwd -P); trap 'rm -rf "$S"' EXIT
export HOME=$S/home GIT_CONFIG_GLOBAL=$S/gitconfig GIT_CONFIG_NOSYSTEM=1 STUB=$S/stub
unset DOTFILES_DEPLOY DOTFILES_GUARD
mkdir -p "$HOME/.config/git/profiles" "$HOME/.config/git/hooks" "$STUB" "$S/bin"
git config --global user.name Tester
git config --global user.email tester@example.com
# The hook finds `dot` in the PATH: a stub that records what it receives, then a PATH without any dot.
cat >"$S/bin/dot" <<'BASH'
#!/usr/bin/env bash
set -euo pipefail
cat >"$STUB/refs"
printf '%s\n' "$@" >"$STUB/args"
printf '%s' "${DOTFILES_GUARD:-}" >"$STUB/mode"
[ ! -e "$STUB/block" ]
BASH
chmod +x "$S/bin/dot"
nodot=
IFS=: read -ra dirs <<<"$PATH"
for d in "${dirs[@]}"; do [ -x "$d/dot" ] || nodot+=${nodot:+:}$d; done
PATH=$S/bin:$nodot
git init -q --bare -b main "$S/remote.git"
git init -q -b main "$S/repo"
g() { git -C "$S/repo" "$@"; }
g config core.hooksPath "$fx/git-hooks"
g remote add origin "$S/remote.git"
g commit -q --allow-empty -m fixture
g push -q origin main
sha=$(g rev-parse HEAD)
printf 'refs/heads/main %s refs/heads/main %040d\n' "$sha" 0 >"$S/expected"
cmp -s "$S/expected" "$STUB/refs"
printf 'guard\npush\norigin\n%s\n' "$S/remote.git" >"$S/expected-args"
cmp -s "$S/expected-args" "$STUB/args"
echo 'OK   guard passes: push succeeds, refs and arguments forwarded'
[ ! -s "$STUB/mode" ]
g config dotfiles.guard secrets
g commit -q --allow-empty -m secrets-only
g push -q origin main
[ "$(cat "$STUB/mode")" = secrets ]
g config dotfiles.guard nope
g commit -q --allow-empty -m bad-mode
if g push -q origin main >"$S/out" 2>&1; then exit 1; fi
grep -q 'dotfiles.guard inconnu' "$S/out"
g reset -q --hard HEAD~1
g config --unset dotfiles.guard
sha=$(g rev-parse HEAD)
echo 'OK   dotfiles.guard secrets reaches the guard, an unknown mode blocks'

g commit -q --allow-empty -m next
refused() {
  if g push -q origin main >"$S/out" 2>&1; then exit 1; fi
  [ "$(git --git-dir="$S/remote.git" rev-parse main)" = "$sha" ]
}
touch "$STUB/block"
refused
grep -q 'Push bloqué.*git push --no-verify.*une fois' "$S/out"
echo 'OK   guard failure refuses the push with the bypass hint'
rm "$STUB/block"

export HOOK_LOG=$S/local
cat >"$S/repo/.git/hooks/pre-push" <<'BASH'
#!/usr/bin/env bash
set -euo pipefail
cat >"$HOOK_LOG.refs"
printf '%s\n' "$@" >"$HOOK_LOG.args"
[ ! -e "$HOOK_LOG.block" ] || exit 42
BASH
chmod +x "$S/repo/.git/hooks/pre-push"
touch "$HOOK_LOG.block"
refused
cmp -s "$STUB/refs" "$HOOK_LOG.refs"
printf 'origin\n%s\n' "$S/remote.git" >"$S/expected-args"
cmp -s "$S/expected-args" "$HOOK_LOG.args"
# A direct call also checks the hook's exit status, which git push normalizes to 1.
code=0
(cd "$S/repo" && "$hook" origin "$S/remote.git" <"$S/expected") || code=$?
[ "$code" -eq 42 ]
echo 'OK   repo hook receives the same refs and arguments, its failure blocks'
rm "$HOOK_LOG.block"
g push -q origin main
cmp -s "$STUB/refs" "$HOOK_LOG.refs"
sha=$(g rev-parse HEAD)
echo 'OK   both hooks pass: push succeeds'

git -C "$S/repo" worktree add -q -b linked "$S/worktree"
touch "$HOOK_LOG.block"
if git -C "$S/worktree" push -q origin linked >"$S/out" 2>&1; then exit 1; fi
cmp -s "$STUB/refs" "$HOOK_LOG.refs"
rm "$HOOK_LOG.block"
git -C "$S/worktree" push -q origin linked
cmp -s "$STUB/refs" "$HOOK_LOG.refs"
echo 'OK   linked worktree also chains the common repository hook'

rm "$S/repo/.git/hooks/pre-push"
ln -s "$hook" "$S/repo/.git/hooks/pre-push"
g commit -q --allow-empty -m self
g push -q origin main
sha=$(g rev-parse HEAD)
echo 'OK   repo hook pointing to this script is not run recursively'

g commit -q --allow-empty -m missing
rm "$S/bin/dot"
refused
grep -q 'Garde-fou introuvable' "$S/out"
grep -q 'git push --no-verify' "$S/out"
g push -q --no-verify origin main
echo 'OK   missing dot fails closed, --no-verify bypasses once'

ln -s "$fx/profiles.gitconfig" "$HOME/.config/git/profiles.gitconfig"
ln -s "$fx/profiles/perso.gitconfig" "$HOME/.config/git/profiles/perso.gitconfig"
ln -s "$hook" "$HOME/.config/git/hooks/pre-push"
# shellcheck disable=SC2088 # Git expands the literal tilde in include.path.
git config --global --add include.path '~/.config/git/profiles.gitconfig'
g config --unset core.hooksPath
for url in https://github.com/acmecorp/x.git git@github.com:acmecorp/x.git; do
  g remote set-url origin "$url"
  [ "$(g config --path core.hooksPath)" = "$HOME/.config/git/hooks" ]
  [ "$(g config --get dotfiles.profile)" = perso ]
done
# Actually execute the HOME-relative hook; --path alone only checks config conversion.
if g hook run pre-push </dev/null >"$S/out" 2>&1; then exit 1; fi
grep -q 'Garde-fou introuvable' "$S/out"
g config core.hooksPath .githooks
[ "$(g config --path core.hooksPath)" = .githooks ]
g config --unset core.hooksPath
g remote set-url origin https://git.example.com/team/x.git
if g config --get core.hooksPath >"$S/out"; then exit 1; fi
git init -q -b main "$HOME/src/github.com/acmecorp/x"
[ "$(git -C "$HOME/src/github.com/acmecorp/x" config --path core.hooksPath)" = "$HOME/.config/git/hooks" ]
echo 'OK   real profiles: HTTPS, SSH and directory rules, tilde expansion, local override, other remote excluded'

# End to end with the real dot: the profile of the pushed repo picks its own term list.
PATH=$S/real:$PATH; mkdir "$S/real"; ln -s "$DOT_BIN" "$S/real/dot"
export DOT_BETTERLEAKS=$fx/fake-betterleaks
mkdir -p "$HOME/.dot/perso"
printf '%s\n' acmecorp >"$HOME/.dot/perso/forbidden.local"
# A second, never contacted remote selects the profile; the push itself goes to the local remote.
g remote set-url origin "$S/remote.git"
g remote add site https://github.com/acmecorp/x.git
echo "AcmeCorp" >"$S/repo/t.txt"; g add t.txt; g commit -q --no-verify -m term
if g push -q origin main >"$S/out" 2>&1; then exit 1; fi
grep -q 'contenu' "$S/out"
g reset -q --hard HEAD~1
g push -q origin main
echo 'OK   dot guard push: the term list of the profile in git config blocks, a clean push passes'
