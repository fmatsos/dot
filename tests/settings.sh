#!/usr/bin/env bash
# Self-check of dot settings with fictional settings and an isolated HOME.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
. "$root/tests/lib.sh" # builds DOT_BIN before HOME is redirected
S=$(cd "$(mktemp -d)" && pwd -P); trap 'rm -rf "$S"' EXIT
export HOME=$S/home DOTFILES_DEPLOY=$S/deploy
mkdir -p "$DOTFILES_DEPLOY/home/.claude"
cat >"$DOTFILES_DEPLOY/home/.claude/settings.base.json" <<'JSON'
{"env":{"GENERIC_PREF":"base"},"theme":"auto"}
JSON
settings() { "$DOT_BIN" settings "$@"; }

settings -n >"$S/out"
[ ! -e "$HOME" ]; grep -q '^+.*GENERIC_PREF' "$S/out"
echo 'OK   dry run without settings writes nothing'
settings >"$S/out"
cmp -s <(jq -S . "$DOTFILES_DEPLOY/home/.claude/settings.base.json") <(jq -S . "$HOME/.claude/settings.json")
echo 'OK   absent settings → base'

cat >"$HOME/.claude/settings.json" <<'JSON'
{"env":{"GENERIC_PREF":"local","LOCAL_PREF":"keep"},"theme":"dark","extra":true,"localArray":["alpha","beta"]}
JSON
cp "$HOME/.claude/settings.json" "$S/before"
settings -n >"$S/out"
cmp -s "$S/before" "$HOME/.claude/settings.json"
if grep -q 'LOCAL_PREF' "$S/out"; then exit 1; fi
[ ! -d "$HOME/.local" ]
echo 'OK   dry run preserves current settings and creates no backup'
settings >"$S/out"
jq -e '.extra and .env.LOCAL_PREF == "keep" and .localArray == ["alpha","beta"]' "$HOME/.claude/settings.json" >/dev/null
echo 'OK   extra keys and arrays preserved'
jq -e '.theme == "auto" and .env.GENERIC_PREF == "base"' "$HOME/.claude/settings.json" >/dev/null
echo 'OK   base keys win, objects deep-merge'
mapfile -t backups < <(find "$HOME/.local/state/dotfiles/backup" -name settings.json)
[ "${#backups[@]}" -eq 1 ]; cmp -s "$S/before" "${backups[0]}"
echo 'OK   original settings backed up intact'
cp "$HOME/.claude/settings.json" "$S/after"
[ "$(settings)" = 'settings: à jour' ]
cmp -s "$S/after" "$HOME/.claude/settings.json"
[ "$(find "$HOME/.local/state/dotfiles/backup" -name settings.json | wc -l)" -eq 1 ]
echo 'OK   second run: settings: à jour, no write or new backup'
if settings --invalid >"$S/out" 2>&1; then exit 1; fi
printf 'invalid json\n' >"$HOME/.claude/settings.json"
cp "$HOME/.claude/settings.json" "$S/invalid"
if settings >"$S/out" 2>&1; then exit 1; fi
cmp -s "$S/invalid" "$HOME/.claude/settings.json"
echo 'OK   invalid option and malformed JSON rejected without replacement'

# Multi-profile: without DOTFILES_DEPLOY, every registered profile is merged in registry order.
unset DOTFILES_DEPLOY
export HOME=$S/home2
mkdir -p "$HOME/.dot/acme/home/.claude" "$HOME/.dot/perso/home/.claude" "$HOME/.dot/empty"
cp "$root/tests/fixtures/settings/settings.base.json" "$HOME/.dot/perso/home/.claude/settings.base.json"
cp "$root/tests/fixtures/settings/settings.base2.json" "$HOME/.dot/acme/home/.claude/settings.base.json"
printf '%s\n' '{"default":"perso","profiles":{"perso":{"repo":"https://example.com/p.git"},"acme":{"repo":"https://example.com/a.git"},"empty":{"repo":"https://example.com/e.git"}}}' >"$HOME/.dot/profiles.json"
settings >"$S/out"
jq -e '.theme == "auto" and .language == "French" and .env == {GENERIC_PREF:"second",SECOND_PREF:"second"}' "$HOME/.claude/settings.json" >/dev/null
echo 'OK   all profiles merge in registry order, the last wins, a profile without base is skipped'
rm -r "$HOME/.claude"
"$DOT_BIN" -p perso settings >"$S/out"
jq -e '.env == {GENERIC_PREF:"base"} and (has("language") | not)' "$HOME/.claude/settings.json" >/dev/null
echo 'OK   -p targets a single profile'
rm -r "$HOME/.dot"
if settings >"$S/out" 2>"$S/err"; then exit 1; fi
grep -q 'aucun profil inscrit' "$S/err"
echo 'OK   empty registry without DOTFILES_DEPLOY: clear error'
echo '== 11 OK, 0 FAIL'
