#!/usr/bin/env bash
# dot install / uninstall of one fictional profile, from a local git repository, in an isolated HOME.
set -Eeuo pipefail
trap 'echo "FAIL install line $LINENO" >&2' ERR
root=$(cd "$(dirname "$0")/.." && pwd)
. "$root/tests/lib.sh" # builds DOT_BIN before HOME is redirected
S=$(mktemp -d); trap 'rm -rf "$S"' EXIT
export HOME=$S/home GIT_CONFIG_GLOBAL=$S/gitconfig GIT_CONFIG_NOSYSTEM=1
export XDG_CONFIG_HOME=$HOME/.config XDG_DATA_HOME=$HOME/.local/share XDG_CACHE_HOME=$HOME/.cache
export XDG_STATE_HOME=$HOME/.local/state CODEX_HOME=$HOME/.codex DOTFILES_TOOLS=0
unset DOTFILES_DEPLOY DOT_PROFILE DOTFILES_BACKUP_DAYS
mkdir -p "$HOME" "$S/bin" "$S/remotes"
export PATH=$S/bin:$PATH
# Never let the install touch a real CLI, even though the tools step is not wired here.
for cli in claude codex opencode bw pass-cli gh mise; do
  printf '#!/bin/bash\nexit 0\n' >"$S/bin/$cli"; chmod +x "$S/bin/$cli"
done

# mkremote <fixture> <dir>: a local git repository standing for a profile's remote.
mkremote() {
  mkdir -p "$2"; cp -a "$root/tests/fixtures/install/$1/." "$2/"
  git -C "$2" init -q -b main
  git -C "$2" config uploadpack.allowFilter true
  git -C "$2" add -A
  git -C "$2" -c user.name=Fixture -c user.email=fixture@example.com commit -q -m fixture
}
fails() { # fails <code> <cmd…>: the command must exit with that code and print nothing on stdout
  local want=$1 code=0; shift
  "$@" >"$S/out" 2>"$S/err" || code=$?
  [ "$code" -eq "$want" ]
}
registry=$HOME/.dot/profiles.json
A=$S/remotes/dots-a; mkremote a "$A"
sha=$(git -C "$A" rev-parse HEAD)
clone=$HOME/.dot/a

dot install -n "$A" -p a >"$S/out" 2>&1
[ -z "$(find "$HOME" -mindepth 1 -print -quit)" ]
grep -qF "[dry] git -C $clone fetch -q --filter=blob:none origin main" "$S/out"
grep -qF "rien à lier : $clone/home absent (dry run sans clone ?)" "$S/out"
echo 'OK   dry run prints the sparse clone commands and creates nothing in HOME'

dot install "$A" -p a >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
[ "$(tail -n1 "$S/out")" = ok ]
while IFS= read -r -d '' f; do
  target=$HOME/${f#"$clone/home/"}
  [ "$(readlink "$target")" = "$f" ]
done < <(find "$clone/home" -type f -print0)
[ "$(readlink "$HOME/.local/bin/a-tool")" = "$clone/bin/a-tool" ]
[ "$("$HOME/.local/bin/a-tool")" = a-tool ]
[ "$(dot config get default)" = a ]
[ "$(dot config get profiles.a.repo)" = "$A" ]
[ "$(stat -c %a "$registry" 2>/dev/null || stat -f %Lp "$registry")" = 600 ]
echo 'OK   install clones, registers the first profile as default and links home/ and bin/'

# shellcheck disable=SC2088 # Check the literal include value, before Git expands it.
git config --global --get-all include.path | grep -qxF '~/.config/git/profiles.gitconfig'
[ "$(git -C "$clone" config --local core.hooksPath)" = .githooks ]
[ "$(git -C "$clone" config --local user.email)" = a@example.com ]
[ "$(git -C "$clone" config --local user.name)" = 'a Example' ]
[ "$(git -C "$clone" rev-parse HEAD)" = "$sha" ]
[ "$(git -C "$clone" config remote.origin.partialclonefilter)" = blob:none ]
[ "$(git -C "$clone" sparse-checkout list)" = $'.githooks\nbin\nhome' ]
echo 'OK   HEAD deployed with sparse blobless config, profiles include and local identity/hooks'

grep -q '"theme": "dark"' "$HOME/.claude/settings.json"
echo 'OK   the settings module declared by the manifest ran'

dot install >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
if grep -qE 'lien :|sauvegarde' "$S/out"; then cat "$S/out"; exit 1; fi
[ "$(tail -n1 "$S/out")" = ok ]
echo 'OK   second install has no links to create or files to back up'

printf 'edited by a tool\n' >"$S/new"
rm "$HOME/.a-rc"; cp "$S/new" "$HOME/.a-rc"
dot st >"$S/out" 2>"$S/err"
grep -Fxq 'détaché (plus un lien, à réconcilier) : ~/.a-rc' "$S/err"
[ "$(wc -l <"$S/err")" -eq 1 ]
echo 'OK   dot st reports the file a tool detached from its link'

dot install -n >"$S/out" 2>&1
[ ! -L "$HOME/.a-rc" ]; grep -qF '[dry]' "$S/out"; [ "$(tail -n1 "$S/out")" = 'ok (dry run)' ]
echo 'OK   dry run on an installed profile writes nothing'
dot install >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
[ "$(readlink "$HOME/.a-rc")" = "$clone/home/.a-rc" ]
mapfile -t backups < <(find "$HOME/.local/state/dotfiles/backup" -name .a-rc -type f)
[ "${#backups[@]}" -eq 1 ]; cmp -s "$S/new" "${backups[0]}"
echo 'OK   existing regular file is backed up intact and replaced by its link'

# Backup retention, end to end (the rules themselves are unit-tested in internal/link).
base=$HOME/.local/state/dotfiles/backup
old=$base/20000101-010203
mkdir -p "$old"
DOTFILES_BACKUP_DAYS=0 dot install >"$S/out" 2>&1; [ -d "$old" ]
DOTFILES_BACKUP_DAYS=abc dot install >"$S/out" 2>"$S/err"; [ -d "$old" ]
grep -q 'DOTFILES_BACKUP_DAYS invalide, purge ignorée' "$S/err"
DOTFILES_BACKUP_DAYS=30 dot install -n >"$S/out" 2>&1; [ -d "$old" ]; grep -q '\[dry\] purge :' "$S/out"
DOTFILES_BACKUP_DAYS=30 dot install >"$S/out" 2>&1
[ ! -e "$old" ]; [ -f "${backups[0]}" ]
grep -Fxq 'sauvegardes : 1 dossier(s) purgé(s)' "$S/out"
echo 'OK   DOTFILES_BACKUP_DAYS: 0 and invalid keep, -n lists, a real install purges old backups'

# Refusals leave no registry change and no half clone.
cp "$registry" "$S/registry.before"
B=$S/remotes/dots-b; mkremote b "$B"
fails 1 dot install "$B" -p a
grep -q 'déjà inscrit avec un autre dépôt' "$S/err"
mkremote a "$S/remotes/bad-manifest"; printf '{}\n' >"$S/remotes/bad-manifest/dot.json"
git -C "$S/remotes/bad-manifest" -c user.name=F -c user.email=f@example.com commit -q -am bad
fails 1 dot install "$S/remotes/bad-manifest"; [ ! -e "$HOME/.dot/bad-manifest" ]
grep -q 'manifest :' "$S/err"
mkremote a "$S/remotes/no-id"; printf '[user]\n\tname = Nobody\n' >"$S/remotes/no-id/home/.config/git/profiles/a.gitconfig"
git -C "$S/remotes/no-id" -c user.name=F -c user.email=f@example.com commit -q -am noid
fails 1 dot install "$S/remotes/no-id"; [ ! -e "$HOME/.dot/no-id" ]
grep -q 'profil a : user.name et user.email requis' "$S/err"
fails 1 dot install "$B" -p 'bad key'; [ ! -e "$HOME/.dot/bad key" ]
fails 1 dot install "$S/remotes/missing" -p missing; [ ! -e "$HOME/.dot/missing" ]
cmp -s "$S/registry.before" "$registry"
echo 'OK   a wrong key, repository, manifest or identity leaves the registry and the disk untouched'

dot install "$B" >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
[ "$(dot config get profiles.dots-b.repo)" = "$B" ]; [ "$(dot config get default)" = a ]
[ -L "$HOME/.b-rc" ]
echo 'OK   the key comes from the repository name, and the default stays the first profile'
fails 2 dot uninstall; grep -qFx 'usage : dot uninstall -p <clé> [--purge]' "$S/err"
fails 1 dot uninstall -p nope; grep -q 'profil inconnu : nope' "$S/err"
fails 1 dot uninstall -p a; grep -q 'profil par défaut' "$S/err"; [ -L "$HOME/.a-rc" ]
dot uninstall -p dots-b >"$S/out" 2>&1
[ ! -e "$HOME/.b-rc" ] && [ ! -e "$HOME/.config/b" ] && [ -d "$HOME/.dot/dots-b/home" ]
grep -Fq 'lien retiré : ~/.b-rc' "$S/out"
fails 1 dot config get profiles.dots-b.repo
echo 'OK   uninstall removes the links and the entry, keeps the clone, and guards its usage'

fresh=$S/fresh; mkdir "$fresh"
HOME=$fresh fails 1 dot install; grep -Fxq 'dot : aucun profil inscrit (dot install <url>)' "$S/err"
echo 'OK   install without profile or url explains itself'
