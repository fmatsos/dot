#!/usr/bin/env bash
# Extensions (dot-<cmd>), git fallback on the profile clone, -p, st, push, whoami, repos colors.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
source "$root/tests/lib.sh"
S=$(cd "$(mktemp -d)" && pwd -P); trap 'rm -rf "$S"' EXIT
export HOME=$S/home GIT_CONFIG_GLOBAL=$S/gitconfig GIT_CONFIG_NOSYSTEM=1 DOT_SRC=$S/src DOT_SANDBOX=$S/sandbox
unset DOTFILES_DEPLOY DOT_PROFILE NO_COLOR
mkdir -p "$HOME/.dot" "$S/pathbin" "$S/remotes"
export PATH=$S/pathbin:$PATH
git config --global user.name Tester
git config --global user.email tester@example.com
git config --global init.defaultBranch main
pass=0
ok() { echo "OK   $1"; pass=$((pass + 1)); }
ko() { echo "FAIL $1"; exit 1; }

# Two fictional profiles, each a clone of its own bare remote.
for key in perso acmecorp; do
  git init -q --bare "$S/remotes/$key.git"
  git clone -q "$S/remotes/$key.git" "$HOME/.dot/$key" 2>/dev/null
  mkdir -p "$HOME/.dot/$key/home" "$HOME/.dot/$key/bin"
  printf '%s\n' "$key" >"$HOME/.dot/$key/home/file"
  git -C "$HOME/.dot/$key" add home/file
  git -C "$HOME/.dot/$key" commit -qm "init $key"
  git -C "$HOME/.dot/$key" push -q origin main 2>/dev/null
done
printf '{"default":"perso","profiles":{"perso":{"repo":"https://example.com/p.git"},"acmecorp":{"repo":"https://example.com/a.git"}}}\n' >"$HOME/.dot/profiles.json"

[ "$(dot log --format=%s)" = "init perso" ] || ko "git fallback: log"
dot add home/file
printf 'new\n' >"$HOME/.dot/perso/home/new"; dot add home/new
[ "$(git -C "$HOME/.dot/perso" diff --cached --name-only)" = home/new ] || ko "dot add: stages in the clone"
git -C "$HOME/.dot/perso" reset -q home/new; rm "$HOME/.dot/perso/home/new"
dot --help | grep -q '^  add ' || ko "dot add: listed in the help"
[ "$(dot -p acmecorp log --format=%s)" = "init acmecorp" ] || ko "git fallback: -p before the command"
[ "$(DOT_PROFILE=acmecorp dot log --format=%s)" = "init acmecorp" ] || ko "git fallback: DOT_PROFILE"
printf 'more\n' >>"$HOME/.dot/perso/home/file"
dot diff | grep -q '^+more' || ko "git fallback: diff"
dot diff --stat | grep -q 'home/file' || ko "git fallback: diff --stat"
code=0; dot nosuchgitcmd >"$S/out" 2>"$S/err" || code=$?
[ "$code" -ne 0 ] || ko "git fallback: unknown command"
ok "git fallback: dot log, diff, add, -p, DOT_PROFILE, failure propagated"

cat >"$HOME/.dot/perso/bin/dot-hello" <<'BASH'
#!/usr/bin/env bash
echo "hello perso deploy=$DOTFILES_DEPLOY args=$*"
exit "${HELLO_CODE:-0}"
BASH
cat >"$S/pathbin/dot-hello" <<'BASH'
#!/usr/bin/env bash
echo "hello path deploy=${DOTFILES_DEPLOY:-none} args=$*"
BASH
cat >"$S/pathbin/dot-only" <<'BASH'
#!/usr/bin/env bash
echo "only deploy=${DOTFILES_DEPLOY:-none} args=$*"
exit 7
BASH
chmod +x "$HOME/.dot/perso/bin/dot-hello" "$S/pathbin/dot-hello" "$S/pathbin/dot-only"
[ "$(dot hello a --b)" = "hello perso deploy=$HOME/.dot/perso args=a --b" ] || ko "profile extension"
[ "$(dot -p acmecorp hello x)" = "hello path deploy=$HOME/.dot/acmecorp args=x" ] || ko "PATH extension, targeted profile"
code=0; HELLO_CODE=5 dot hello >/dev/null || code=$?
[ "$code" -eq 5 ] || ko "extension exit code"
code=0; out=$(dot only z) || code=$?
[ "$code" -eq 7 ] && [ "$out" = "only deploy=$HOME/.dot/perso args=z" ] || ko "PATH-only extension"
[ "$(dot help only 2>&1)" = "only deploy=$HOME/.dot/perso args=--help" ] || ko "dot help <extension>"
[ "$(dot only --help)" = "only deploy=$HOME/.dot/perso args=--help" ] || ko "dot <extension> --help"
ok "extensions: profile bin/ before PATH, DOTFILES_DEPLOY, exit code, help"

# st: git status -sb on the targeted profile clone (several profiles: see multiprofile.sh).
out=$(dot -p perso st)
grep -q '^## main' <<<"$out" && grep -q 'home/file' <<<"$out" || ko "st"
[ "$(dot -p perso status)" = "$out" ] || ko "status alias"
ok "st and status: clone state"

# push: commits tracked changes only, warns about new files, pushes, then has nothing left.
printf 'untracked\n' >"$HOME/.dot/perso/home/new"
dot -p perso push >"$S/out" 2>"$S/err"
grep -Fq 'home/new' "$S/err" && [ -z "$(git -C "$HOME/.dot/perso" ls-files home/new)" ] || ko "push: new files"
[ "$(git -C "$HOME/.dot/perso" log -1 --format=%s)" = "Update file" ] || ko "push: default message"
[ "$(git -C "$S/remotes/perso.git" log -1 --format=%s)" = "Update file" ] || ko "push: push"
head=$(git -C "$S/remotes/perso.git" rev-parse main)
dot -p perso push >/dev/null 2>&1 || ko "push: nothing to send"
[ "$(git -C "$S/remotes/perso.git" rev-parse main)" = "$head" ] && [ "$(git -C "$HOME/.dot/perso" rev-parse HEAD)" = "$head" ] || ko "push: nothing to send"
printf 'again\n' >>"$HOME/.dot/perso/home/file"
dot -p perso push fix: a free message >/dev/null 2>&1
[ "$(git -C "$S/remotes/perso.git" log -1 --format=%s)" = "fix: a free message" ] || ko "push: message"
ok "push: tracked files committed, warning, push, nothing left to send"

# whoami: identity in force here, and where it comes from.
out=$(dot whoami 2>&1)
grep -q 'tester@example.com' <<<"$out" && grep -q 'Tester <tester@example.com>' <<<"$out" || ko "whoami"
git config --global --unset user.email
git config --global user.useConfigOnly true # git then refuses to guess an identity, whatever the host name
code=0; dot whoami >/dev/null 2>&1 || code=$?
[ "$code" -ne 0 ] || ko "whoami without email"
git config --global --unset user.useConfigOnly
git config --global user.email tester@example.com
ok "whoami: identity and origin, failure without email"

# repos prints its reason in red on a terminal only, and not with NO_COLOR.
if command -v script >/dev/null; then
  mkdir -p "$HOME/.config/git/profiles" "$DOT_SRC/git.example.com/team"
  printf '[user]\n  email = expected@example.com\n[dotfiles]\n  profile = perso\n' >"$HOME/.config/git/profiles/perso.gitconfig"
  git clone -q "$S/remotes/perso.git" "$DOT_SRC/git.example.com/team/bad" 2>/dev/null
  git -C "$DOT_SRC/git.example.com/team/bad" config dotfiles.profile perso
  grep -q $'\e\\[31m' <<<"$(in_pty /dev/null "$DOT_BIN repos")" || ko "no red on a terminal"
  ! grep -q $'\e\\[' <<<"$(NO_COLOR=1 in_pty /dev/null "$DOT_BIN repos")" || ko "NO_COLOR ignored"
  ! grep -q $'\e\\[' <<<"$(dot repos)" || ko "red off a terminal"
fi
ok "repos: red on a terminal only, NO_COLOR honored"
echo "== $pass OK, 0 FAIL"
