#!/usr/bin/env bash
# dot push: a commit left behind by a failed push is pushed by the next run.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
. "$root/tests/lib.sh"
S=$(cd "$(mktemp -d)" && pwd -P); trap 'rm -rf "$S"' EXIT
export HOME=$S/home GIT_CONFIG_GLOBAL=$S/gitconfig GIT_CONFIG_NOSYSTEM=1 DOTFILES_DEPLOY=$S/deploy
mkdir -p "$HOME"
git config --global init.defaultBranch main
git config --global user.name Tester; git config --global user.email tester@example.org
git init -q --bare "$S/remote.git"
git clone -q "$S/remote.git" "$DOTFILES_DEPLOY" 2>/dev/null
echo one >"$DOTFILES_DEPLOY/f"; git -C "$DOTFILES_DEPLOY" add f
git -C "$DOTFILES_DEPLOY" commit -qm init; git -C "$DOTFILES_DEPLOY" push -q -u origin main

echo two >"$DOTFILES_DEPLOY/f"
mv "$S/remote.git" "$S/remote.off"
if dot push "second" >"$S/out" 2>"$S/err"; then echo "FAIL push without a remote succeeded"; exit 1; fi
[ "$(git -C "$DOTFILES_DEPLOY" log -1 --format=%s)" = second ]
echo 'OK   failed push leaves the commit'

mv "$S/remote.off" "$S/remote.git"
dot push >"$S/out" 2>"$S/err"
[ "$(git -C "$S/remote.git" log -1 --format=%s main)" = second ]
echo 'OK   next push sends the unpushed commit'

head=$(git -C "$S/remote.git" rev-parse main)
dot push >"$S/out" 2>"$S/err"
[ "$(git -C "$S/remote.git" rev-parse main)" = "$head" ] && [ "$(git -C "$DOTFILES_DEPLOY" rev-parse HEAD)" = "$head" ]
echo 'OK   nothing left: the next push changes nothing'
