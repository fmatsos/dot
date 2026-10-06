#!/usr/bin/env bash
# dot backups list / restore, black box, on the backups made by a real dot install, in an isolated HOME.
set -Eeuo pipefail
trap 'echo "FAIL backups line $LINENO" >&2' ERR
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
fails() { # fails <code> <cmd…>: must exit with that code
  local want=$1 code=0; shift
  "$@" >"$S/out" 2>"$S/err" || code=$?
  [ "$code" -eq "$want" ]
}
base=$HOME/.local/state/dotfiles/backup

A=$S/remotes/dots-a; mkdir -p "$A"; cp -a "$root/tests/fixtures/install/a/." "$A/"
git -C "$A" init -q -b main; git -C "$A" config uploadpack.allowFilter true; git -C "$A" add -A
git -C "$A" -c user.name=F -c user.email=f@example.com commit -q -m fixture
clone=$HOME/.dot/a

dot backups list >"$S/out"
grep -qx 'aucune sauvegarde' "$S/out"
fails 1 dot backups restore
grep -q 'aucune sauvegarde à restaurer' "$S/err"
echo 'OK   no backup: clear message, list exits 0, restore exits 1'

# Pre-existing files in ~ that the install will replace and back up.
echo user-rc >"$HOME/.a-rc"; chmod 600 "$HOME/.a-rc"
mkdir -p "$HOME/.config/a"; echo user-app >"$HOME/.config/a/app.conf"
dot install "$A" -p a >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
run=$(ls "$base"); [ "$(printf %s "$run" | wc -l)" -eq 0 ]
[ -f "$base/$run/.a-rc" ] && [ -f "$base/$run/.config/a/app.conf" ]

dot backups list >"$S/out"
head -n1 "$S/out" | grep -qx "$run  2 fichier(s)"
grep -qx '  ~/.a-rc' "$S/out" && grep -qx '  ~/.config/a/app.conf' "$S/out"
dot backups list "$run" >"$S/out"; grep -qx '  ~/.a-rc' "$S/out"
fails 1 dot backups list 20000101-000000
echo 'OK   list shows the run, its files relative to ~, and one run by id'

dot backups restore -n >"$S/out"
grep -q '\[dry\] restauré : ~/.a-rc' "$S/out"
[ -L "$HOME/.a-rc" ] && [ -f "$base/$run/.a-rc" ]
echo 'OK   restore -n prints the plan and writes nothing'

# A real user file at one of the targets: refused, the other one goes back.
rm "$HOME/.a-rc"; echo newer >"$HOME/.a-rc"
fails 1 dot backups restore
grep -q 'refusé (fichier existant) : ~/.a-rc' "$S/err"
grep -q 'restauré : ~/.config/a/app.conf' "$S/out"
grep -q 'dot install recréera les liens' "$S/out"
[ "$(cat "$HOME/.a-rc")" = newer ] && [ -f "$base/$run/.a-rc" ]
[ -f "$HOME/.config/a/app.conf" ] && [ ! -L "$HOME/.config/a/app.conf" ]
[ "$(cat "$HOME/.config/a/app.conf")" = user-app ]
[ -d "$base/$run" ]
echo 'OK   restore refuses a real user file (exit 1), restores the dot link target, keeps the run'

# The dot link goes back to being the user file, mode kept, run dir removed once empty.
rm "$HOME/.a-rc"; ln -s "$clone/home/.a-rc" "$HOME/.a-rc"
dot backups restore "$run" '~/.a-rc' >"$S/out"
[ ! -L "$HOME/.a-rc" ] && [ "$(cat "$HOME/.a-rc")" = user-rc ]
[ "$(stat -c %a "$HOME/.a-rc" 2>/dev/null || stat -f %Lp "$HOME/.a-rc")" = 600 ]
[ ! -e "$base/$run" ]
echo 'OK   restore of a dot link: link replaced by the backup (moved, mode kept), empty run removed'

# Path traversal and unknown paths.
mkdir -p "$base/20260101-000000"; echo x >"$base/20260101-000000/.f"
fails 1 dot backups restore 20260101-000000 ../../../x
fails 1 dot backups restore 20260101-000000 /etc/passwd
fails 1 dot backups restore 20260101-000000 .nope
[ -f "$base/20260101-000000/.f" ] && [ ! -e "$HOME/.f" ]
fails 1 dot backups list ../backup
echo 'OK   traversal, absolute and unknown paths are refused'
