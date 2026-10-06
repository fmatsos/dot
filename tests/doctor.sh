#!/usr/bin/env bash
# Read-only doctor against a minimal deploy and stubbed CLIs, never the real user config.
# shellcheck disable=SC2088 # Literal ~ in expected display paths and git includes.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
source "$root/tests/lib.sh"
fx=$root/tests/fixtures/doctor
S=$(mktemp -d); trap 'rm -rf "$S"' EXIT
export HOME=$S/home DOTFILES_DEPLOY=$S/deploy DOT_SRC=$S/home/src DOT_SANDBOX=$S/home/sandbox
export GIT_CONFIG_GLOBAL=$S/gitconfig GIT_CONFIG_NOSYSTEM=1 CODEX_HOME=$S/home/.codex
export XDG_CONFIG_HOME=$HOME/.config
export MISE_DATA_DIR=$S/mise XDG_DATA_HOME=$S/home/.local/share
unset DOT_PROFILE NO_COLOR DOTFILES_FORBIDDEN BW_SESSION FAKE_MISSING FAKE_MARKET
export PATH=$S/bin:$MISE_DATA_DIR/shims:/usr/bin:/bin
mkdir -p "$S/bin" "$DOTFILES_DEPLOY/home" "$HOME/.config/git/profiles" "$HOME/.local/bin"
cat >"$S/bin/mise" <<'STUB'
#!/usr/bin/env bash
[ "$1" = ls ] && [ "$2" = --missing ] || exit 1
[ "${MISE_OFFLINE:-}" = true ] || exit 1
[ "${FAKE_MISSING:-}" != 1 ] || printf 'node 99.0.0\n'
exit 0
STUB
# Hide any system-installed plugin CLIs, with no access to external HOME directories.
cat >"$S/bin/codex" <<'STUB'
#!/usr/bin/env bash
[ -z "${FAKE_MARKET:-}" ] || printf 'MARKETPLACE ROOT\nacme %s\n' "$FAKE_MARKET"
exit 0
STUB
cp "$S/bin/codex" "$S/bin/claude"
chmod +x "$S/bin/"*
printf '[user]\n  email = tester@example.com\n' >"$HOME/.config/git/profiles/demo.gitconfig"
cp "$fx/dot.json" "$DOTFILES_DEPLOY/dot.json"
printf '# profiles\n' >"$HOME/.config/git/profiles.gitconfig"
git config --global --add include.path '~/.config/git/profiles.gitconfig'
git init -qb main "$DOTFILES_DEPLOY"
git -C "$DOTFILES_DEPLOY" config core.hooksPath .githooks
git -C "$DOTFILES_DEPLOY" config user.name Tester
git -C "$DOTFILES_DEPLOY" config user.email tester@example.com
printf 'linked\n' >"$DOTFILES_DEPLOY/home/example"
printf '*.local\n' >"$DOTFILES_DEPLOY/.gitignore"
printf 'acmecorp\n' >"$DOTFILES_DEPLOY/forbidden.local"
ln -s "$DOTFILES_DEPLOY/home/example" "$HOME/example"
git -C "$DOTFILES_DEPLOY" add .; git -C "$DOTFILES_DEPLOY" commit -qm fixture
invoke() {
  code=0
  "$DOT_BIN" doctor "$@" >"$S/out" 2>"$S/err" || code=$?
  ! grep -q acmecorp "$S/out" "$S/err"
}
invoke
[ "$code" -eq 0 ]; ! grep -q '^✗' "$S/out" || exit 1
grep -q '^✓ garde-fou activé' "$S/out"
! grep -q '^profil ' "$S/out" || exit 1
echo 'OK   healthy doctor succeeds; fictional forbidden term never printed; no profile header'
rm "$HOME/example"; printf 'detached\n' >"$HOME/example"
invoke
[ "$code" -eq 1 ]; grep -q '1 fichiers détachés' "$S/out"; grep -q '^  ~/example$' "$S/out"
rm "$HOME/example"; ln -s "$DOTFILES_DEPLOY/home/example" "$HOME/example"
echo 'OK   detached home file fails and is listed'
: >"$DOTFILES_DEPLOY/forbidden.local"
invoke
[ "$code" -eq 1 ]; grep -q '^✗ garde-fou désactivé' "$S/out"
printf 'acmecorp\n' >"$DOTFILES_DEPLOY/forbidden.local"
echo 'OK   empty forbidden file fails without reading its contents'
printf '  # commentaire\n\n \t\n' >"$DOTFILES_DEPLOY/forbidden.local"
invoke
[ "$code" -eq 1 ]; grep -q '^✗ garde-fou désactivé' "$S/out"
printf 'acmecorp\n' >"$DOTFILES_DEPLOY/forbidden.local"
echo 'OK   F4: comments-only forbidden list fails'
export FAKE_MISSING=1
invoke
[ "$code" -eq 1 ]; grep -q '^✗ outils mise manquants : node.*mise install' "$S/out"
unset FAKE_MISSING
echo 'OK   missing mise tool fails with name and install hint'
ln -s "$S/absent" "$HOME/.local/bin/dead"
invoke
[ "$code" -eq 1 ]; grep -q '^✗ 1 liens morts' "$S/out"
rm "$HOME/.local/bin/dead"
echo 'OK   dead executable link fails'
git -C "$DOTFILES_DEPLOY" config user.email other@example.com
invoke
[ "$code" -eq 1 ]; grep -q '^✗ email du clone' "$S/out"
git -C "$DOTFILES_DEPLOY" config user.email tester@example.com
echo 'OK   wrong deploy identity fails'
printf 'x\n' >"$DOTFILES_DEPLOY/pending"
invoke
grep -q '^! clone modifié : dot push$' "$S/out"; ! grep -q 'dot send' "$S/out"
rm "$DOTFILES_DEPLOY/pending"
echo 'OK   modified clone points to dot push'
invoke
grep -q '^✓ profil demo$' "$S/out"
mv "$HOME/.config/git/profiles/demo.gitconfig" "$S/profile"
invoke
[ "$code" -eq 1 ]; grep -q '^✗ profil demo : fichier absent ou user.email manquant' "$S/out"
printf '[user]\n  name = Tester\n' >"$HOME/.config/git/profiles/demo.gitconfig"
invoke
[ "$code" -eq 1 ]; grep -q '^✗ profil demo :' "$S/out"
mv "$S/profile" "$HOME/.config/git/profiles/demo.gitconfig"
echo 'OK   declared profile reports missing file and missing email'
printf 'FAKE_TOKEN=bw:fictional-item\n' >"$DOTFILES_DEPLOY/secrets.local"
invoke
[ "$code" -eq 0 ]; grep -q '^! secrets : 0 lisibles / 1 verrouillés / 0 indisponibles' "$S/out"
! grep -qE 'FAKE_TOKEN|fictional-item' "$S/out" "$S/err" || exit 1
echo 'OK   locked secrets warn using counts only, without prompts'
rm "$DOTFILES_DEPLOY/secrets.local"
"$DOT_BIN" doctor --help >"$S/out"
grep -q '^dot doctor — ' "$S/out"
"$DOT_BIN" doctor -h >"$S/out"
grep -q '^dot doctor — ' "$S/out"
code=0; "$DOT_BIN" doctor extra >"$S/out" 2>"$S/err" || code=$?
[ "$code" -eq 2 ]; grep -q '^usage : dot doctor' "$S/err"
echo 'OK   doctor help; extra argument is a usage error'
# Plugin fixtures use a versioned cache directory, not a hardcoded local/ leaf.
export FAKE_MARKET="$S/plugin market"
mkdir -p "$HOME/.claude/plugins" "$CODEX_HOME/plugins/cache/acme/acme/1.0.0"
cp -R "$fx/market" "$FAKE_MARKET"
cp -R "$FAKE_MARKET/plugin/." "$CODEX_HOME/plugins/cache/acme/acme/1.0.0/"
jq -n --arg location "$FAKE_MARKET" '{acme:{installLocation:$location}}' >"$HOME/.claude/plugins/known_marketplaces.json"
invoke
[ "$code" -eq 0 ]; grep -q '^✓ marketplace Claude acme présent' "$S/out"
grep -q '^✓ copie Codex à jour' "$S/out"
printf 'never-print-plugin-content\n' >"$CODEX_HOME/plugins/cache/acme/acme/1.0.0/example"
invoke
[ "$code" -eq 0 ]; grep -q '^! copie Codex périmée : dot pull' "$S/out"
! grep -q 'never-print-plugin-content' "$S/out" "$S/err" || exit 1
echo 'OK   plugin source and discovered cache compare silently; stale copy warns'
jq 'del(.marketplace)' "$DOTFILES_DEPLOY/dot.json" >"$S/manifest"
mv "$S/manifest" "$DOTFILES_DEPLOY/dot.json"
invoke
[ "$code" -eq 0 ]; ! grep -qE 'marketplace|copie Codex' "$S/out"
echo 'OK   absent marketplace skips plugin checks'

# An invalid manifest is a diagnostic result, not an early exit.
cp "$fx/dot.json" "$S/valid-manifest"
jq '.unexpected=true' "$S/valid-manifest" >"$DOTFILES_DEPLOY/dot.json"
invoke
[ "$code" -eq 1 ]; grep -Fxq '✗ dot.json invalide : clé unexpected' "$S/out"
grep -Fxq '! profils et marketplaces ignorés : dot.json invalide' "$S/out"
grep -q '^✓ clone déployé présent' "$S/out"
grep -q '^✓ mise présent' "$S/out"
grep -q '^✓ identités des dépôts cohérentes' "$S/out"
! grep -qE '^✓ profil |marketplace Claude|marketplace Codex|copie Codex|email du clone' "$S/out"
jq '.deploy.path=42' "$S/valid-manifest" >"$DOTFILES_DEPLOY/dot.json"
invoke
[ "$code" -eq 1 ]; grep -Fxq '✗ dot.json invalide : clé deploy.path' "$S/out"
grep -q '^✓ mise présent' "$S/out"
echo 'OK   L3: invalid manifests fail doctor while other checks continue; dependent checks skipped'

# Repositories whose identity disagrees with their git profile fail the report, listing them.
cp "$S/valid-manifest" "$DOTFILES_DEPLOY/dot.json"
cat >"$HOME/.config/git/profiles/demo.gitconfig" <<'PROFILE'
[user]
  email = tester@example.com
[dotfiles]
  profile = demo
PROFILE
git init -qb main "$DOT_SANDBOX/scratch"
git config --global --add "includeIf.gitdir:$DOT_SANDBOX/scratch/.path" "$HOME/.config/git/profiles/demo.gitconfig"
git -C "$DOT_SANDBOX/scratch" config user.email other@example.com
invoke
[ "$code" -eq 1 ]; grep -q '^✗ identités des dépôts incohérentes' "$S/out"
grep -q '/scratch .*✗ email différent du profil demo' "$S/out"
git -C "$DOT_SANDBOX/scratch" config user.email tester@example.com
invoke
[ "$code" -eq 0 ]; grep -q '^✓ identités des dépôts cohérentes' "$S/out"
echo 'OK   inconsistent repository identity fails and is listed'

# Registry mode: no DOTFILES_DEPLOY, profiles come from ~/.dot/profiles.json.
unset DOTFILES_DEPLOY
mkprofile() {
  local d=$HOME/.dot/$1
  mkdir -p "$d/home"
  cp "$fx/dot.json" "$d/dot.json"
  git init -qb main "$d"
  git -C "$d" config core.hooksPath .githooks
  git -C "$d" config user.name Tester
  git -C "$d" config user.email tester@example.com
  printf 'linked %s\n' "$1" >"$d/home/example-$1"
  printf '*.local\n' >"$d/.gitignore"
  printf 'acmecorp\n' >"$d/forbidden.local"
  git -C "$d" add .; git -C "$d" commit -qm fixture
  ln -s "$d/home/example-$1" "$HOME/example-$1"
}
mkprofile alpha; mkprofile beta
printf '{"default":"alpha","profiles":{"alpha":{"repo":"https://git.example.com/acme/a.git"},"beta":{"repo":"https://git.example.com/acme/b.git"}}}\n' >"$HOME/.dot/profiles.json"
invoke
[ "$code" -eq 0 ]; [ "$(grep -c '^profil ' "$S/out" | tr -d ' ')" -eq 2 ]
grep -qx 'profil alpha' "$S/out"; grep -qx 'profil beta' "$S/out"
[ "$(grep -n '^profil ' "$S/out" | head -1 | cut -d: -f2)" = 'profil alpha' ]
[ "$(grep -c '^✓ clone déployé présent' "$S/out")" -eq 2 ]
[ "$(grep -c '^✓ liens home à jour' "$S/out")" -eq 2 ]
for once in 'aucun lien mort' 'mise présent' 'shims mise' 'profils inclus' 'identités des dépôts' 'marketplace Claude' 'marketplace Codex'; do
  [ "$(grep -c "$once" "$S/out")" -eq 1 ] || { echo "FAIL $once"; exit 1; }
done
# Machine-wide checks come after the last profile.
[ "$(grep -n '^✓ aucun lien mort' "$S/out" | cut -d: -f1)" -gt "$(grep -n '^profil beta' "$S/out" | cut -d: -f1)" ]
echo 'OK   no -p: every registered profile under its header, machine-wide checks once, after them'
rm "$HOME/example-beta"; printf 'detached\n' >"$HOME/example-beta"
invoke
[ "$code" -eq 1 ]; grep -q '1 fichiers détachés' "$S/out"; grep -q '^  ~/example-beta$' "$S/out"
[ "$(sed -n '/^profil beta/,$p' "$S/out" | grep -c '^✗ 1 fichiers détachés')" -eq 1 ]
[ "$(sed -n '/^profil alpha/,/^profil beta/p' "$S/out" | grep -c '^✗')" -eq 0 ]
invoke -p alpha
[ "$code" -eq 0 ]; ! grep -q '^profil ' "$S/out" || exit 1
[ "$(grep -c '^✓ clone déployé présent' "$S/out")" -eq 1 ]
code=0; DOT_PROFILE=beta "$DOT_BIN" doctor >"$S/out" 2>&1 || code=$?
[ "$code" -eq 1 ]; ! grep -q '^profil ' "$S/out" || exit 1; grep -q '~/example-beta' "$S/out"
code=0; DOTFILES_DEPLOY=$HOME/.dot/alpha "$DOT_BIN" doctor >"$S/out" 2>&1 || code=$?
[ "$code" -eq 0 ]; ! grep -q '^profil ' "$S/out" || exit 1
rm "$HOME/example-beta"; ln -s "$HOME/.dot/beta/home/example-beta" "$HOME/example-beta"
echo 'OK   -p, DOT_PROFILE and DOTFILES_DEPLOY target one profile without header; a detached file fails only its profile'
jq '.unexpected=true' "$fx/dot.json" >"$HOME/.dot/beta/dot.json"
invoke
[ "$code" -eq 1 ]; grep -Fxq '✗ dot.json invalide : clé unexpected' "$S/out"
grep -Fxq '! profils et marketplaces ignorés : dot.json invalide' "$S/out"
[ "$(sed -n '/^profil alpha/,/^profil beta/p' "$S/out" | grep -c 'dot.json invalide')" -eq 0 ]
cp "$fx/dot.json" "$HOME/.dot/beta/dot.json"
echo 'OK   invalid manifest of one profile is reported under its header'
printf '{"default":"alpha","profiles":{"alpha":{"repo":"https://git.example.com/acme/a.git"}}}\n' >"$HOME/.dot/profiles.json"
invoke
[ "$code" -eq 0 ]; ! grep -q '^profil ' "$S/out" || exit 1
echo 'OK   a single registered profile prints exactly the headerless report'
invoke -p nope
[ "$code" -eq 1 ]; grep -q '^✗ profil inconnu : nope' "$S/out"
grep -q '^✓ mise présent' "$S/out"
rm "$HOME/.dot/profiles.json"
invoke
[ "$code" -eq 1 ]; grep -Fxq '✗ aucun profil inscrit (dot install <url>)' "$S/out"
grep -q '^✓ mise présent' "$S/out"; ! grep -q '^profil ' "$S/out" || exit 1
echo 'OK   unknown profile and empty registry fail, machine-wide checks still run'
