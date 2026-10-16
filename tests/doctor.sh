#!/usr/bin/env bash
# Read-only doctor against a minimal deploy and stubbed CLIs, never the real user config.
# shellcheck disable=SC2088 # Literal ~ in expected display paths and git includes.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
source "$root/tests/lib.sh"
fx=$root/tests/fixtures/doctor
S=$(cd "$(mktemp -d)" && pwd -P); trap 'rm -rf "$S"' EXIT
export HOME=$S/home DOTFILES_DEPLOY=$S/deploy DOT_SRC=$S/home/src DOT_SANDBOX=$S/home/sandbox
export GIT_CONFIG_GLOBAL=$S/gitconfig GIT_CONFIG_NOSYSTEM=1 CODEX_HOME=$S/home/.codex
export XDG_CONFIG_HOME=$HOME/.config
export MISE_DATA_DIR=$S/mise XDG_DATA_HOME=$S/home/.local/share MISE_CALLS=$S/mise.calls
unset DOT_PROFILE NO_COLOR DOTFILES_FORBIDDEN BW_SESSION FAKE_MISSING FAKE_MARKET
export PATH=$S/bin:$MISE_DATA_DIR/shims:/usr/bin:/bin
mkdir -p "$S/bin" "$DOTFILES_DEPLOY/home" "$HOME/.config/git/profiles" "$HOME/.local/bin"
cat >"$S/bin/mise" <<'STUB'
#!/usr/bin/env bash
[ -z "${MISE_CALLS:-}" ] || printf '%s\n' "$*" >>"$MISE_CALLS"
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
mkdir -p "$DOTFILES_DEPLOY/home/.config/mise" "$HOME/.config/mise"
printf '[tools]\n' >"$DOTFILES_DEPLOY/home/.config/mise/config.toml"
ln -s "$DOTFILES_DEPLOY/home/.config/mise/config.toml" "$HOME/.config/mise/config.toml"
printf '*.local\n' >"$DOTFILES_DEPLOY/.gitignore"
printf 'acmecorp\n' >"$DOTFILES_DEPLOY/forbidden.local"
ln -s "$DOTFILES_DEPLOY/home/example" "$HOME/example"
git -C "$DOTFILES_DEPLOY" add .; git -C "$DOTFILES_DEPLOY" commit -qm fixture
mise_checked() { [ -s "$S/mise.calls" ]; } # the stub saw doctor ask for the missing tools
invoke() {
  : >"$S/mise.calls"; code=0
  "$DOT_BIN" doctor "$@" >"$S/out" 2>"$S/err" || code=$?
  not grep -q acmecorp "$S/out" "$S/err"
}
invoke
[ "$code" -eq 0 ]
# message kept: the report header is the only observable of single vs multi-profile output (needs --json)
! grep -q '^profile ' "$S/out" || exit 1
echo 'OK   healthy doctor succeeds; fictional forbidden term never printed; no profile header'
rm "$HOME/example"; printf 'detached\n' >"$HOME/example"
invoke
[ "$code" -eq 1 ]; grep -q '^  ~/example$' "$S/out"
rm "$HOME/example"; ln -s "$DOTFILES_DEPLOY/home/example" "$HOME/example"
echo 'OK   detached home file fails and is listed'
: >"$DOTFILES_DEPLOY/forbidden.local"
invoke
[ "$code" -eq 1 ]
printf 'acmecorp\n' >"$DOTFILES_DEPLOY/forbidden.local"
echo 'OK   empty forbidden file fails without reading its contents'
printf '  # comment\n\n \t\n' >"$DOTFILES_DEPLOY/forbidden.local"
invoke
[ "$code" -eq 1 ]
printf 'acmecorp\n' >"$DOTFILES_DEPLOY/forbidden.local"
echo 'OK   F4: comments-only forbidden list fails'
printf '%s\n' acmecorp 'globexpat(' >"$DOTFILES_DEPLOY/forbidden.local"
invoke
[ "$code" -eq 1 ]
! grep -q globexpat "$S/out" "$S/err" || exit 1
printf 'acmecorp\n' >"$DOTFILES_DEPLOY/forbidden.local"
echo 'OK   invalid forbidden list fails like the guard would, without printing the pattern'
export FAKE_MISSING=1
invoke
[ "$code" -eq 1 ]; grep -q node "$S/out" # the missing tool's name is the data
unset FAKE_MISSING
echo 'OK   missing mise tool fails with name and install hint'
# mise is only expected when a profile ships its config.toml, or when a mise is already there.
mkdir "$S/nomise"; ln -s "$S/bin/codex" "$S/bin/claude" "$S/nomise/"
PATH=$S/nomise:/usr/bin:/bin invoke
[ "$code" -eq 1 ]
rm "$DOTFILES_DEPLOY/home/.config/mise/config.toml" "$HOME/.config/mise/config.toml"
PATH=$S/nomise:/usr/bin:/bin invoke
# message kept: a skipped check is only observable by the absence of its report line (needs --json)
[ "$code" -eq 0 ]; ! grep -qi 'mise' "$S/out" || exit 1
cp "$S/bin/mise" "$HOME/.local/bin/mise"
PATH=$S/nomise:/usr/bin:/bin invoke
[ "$code" -eq 0 ]; mise_checked
rm "$HOME/.local/bin/mise"
git -C "$DOTFILES_DEPLOY" checkout -q -- home/.config/mise/config.toml
ln -s "$DOTFILES_DEPLOY/home/.config/mise/config.toml" "$HOME/.config/mise/config.toml"
echo 'OK   mise checks: config without mise fails; no config and no mise prints nothing (code 0); an existing mise is checked'
ln -s "$S/absent" "$HOME/.local/bin/dead"
invoke
[ "$code" -eq 1 ]
rm "$HOME/.local/bin/dead"
echo 'OK   dead executable link fails'
git -C "$DOTFILES_DEPLOY" config user.email other@example.com
invoke
[ "$code" -eq 1 ]
git -C "$DOTFILES_DEPLOY" config user.email tester@example.com
echo 'OK   wrong deploy identity fails'
printf 'x\n' >"$DOTFILES_DEPLOY/pending"
invoke
# message kept: a warning leaves the exit code at 0, so its report line is the only observable (needs --json)
grep -q '^! clone modified: dot push$' "$S/out"; not grep -q 'dot send' "$S/out"
rm "$DOTFILES_DEPLOY/pending"
echo 'OK   modified clone points to dot push'
invoke
[ "$code" -eq 0 ]
mv "$HOME/.config/git/profiles/demo.gitconfig" "$S/profile"
invoke
[ "$code" -eq 1 ]
printf '[user]\n  name = Tester\n' >"$HOME/.config/git/profiles/demo.gitconfig"
invoke
[ "$code" -eq 1 ]
mv "$S/profile" "$HOME/.config/git/profiles/demo.gitconfig"
echo 'OK   declared profile reports missing file and missing email'
printf 'FAKE_TOKEN=bw:fictional-item\n' >"$DOTFILES_DEPLOY/secrets.local"
invoke
# message kept: the counts of a warning (exit code 0) are only observable in its report line (needs --json)
[ "$code" -eq 0 ]; grep -q '^! secrets: 0 readable / 1 locked / 0 unavailable' "$S/out"
! grep -qE 'FAKE_TOKEN|fictional-item' "$S/out" "$S/err" || exit 1
echo 'OK   locked secrets warn using counts only, without prompts'
rm "$DOTFILES_DEPLOY/secrets.local"
"$DOT_BIN" doctor --help >"$S/out"
[ -s "$S/out" ]
"$DOT_BIN" doctor -h >"$S/out"
[ -s "$S/out" ]
code=0; "$DOT_BIN" doctor extra >"$S/out" 2>"$S/err" || code=$?
[ "$code" -eq 2 ]; [ ! -s "$S/out" ]
echo 'OK   doctor help; extra argument is a usage error'
# Plugin fixtures use a versioned cache directory, not a hardcoded local/ leaf.
export FAKE_MARKET="$S/plugin market"
mkdir -p "$HOME/.claude/plugins" "$CODEX_HOME/plugins/cache/acme/acme/1.0.0"
cp -R "$fx/market" "$FAKE_MARKET"
cp -R "$FAKE_MARKET/plugin/." "$CODEX_HOME/plugins/cache/acme/acme/1.0.0/"
jq -n --arg location "$FAKE_MARKET" '{acme:{installLocation:$location}}' >"$HOME/.claude/plugins/known_marketplaces.json"
invoke
# message kept: a healthy check leaves the exit code at 0, so its report line is the only observable (needs --json)
[ "$code" -eq 0 ]; grep -q '^✓ marketplace Claude acme present' "$S/out"
grep -q '^✓ Codex copy up to date' "$S/out"
printf 'never-print-plugin-content\n' >"$CODEX_HOME/plugins/cache/acme/acme/1.0.0/example"
invoke
# message kept: a warning leaves the exit code at 0, so its report line is the only observable (needs --json)
[ "$code" -eq 0 ]; grep -q '^! Codex copy stale: dot pull' "$S/out"
! grep -q 'never-print-plugin-content' "$S/out" "$S/err" || exit 1
echo 'OK   plugin source and discovered cache compare silently; stale copy warns'
jq 'del(.marketplace)' "$DOTFILES_DEPLOY/dot.json" >"$S/manifest"
mv "$S/manifest" "$DOTFILES_DEPLOY/dot.json"
invoke
# message kept: a skipped check is only observable by the absence of its report line (needs --json)
[ "$code" -eq 0 ]; not grep -qE 'marketplace|Codex copy' "$S/out"
echo 'OK   absent marketplace skips plugin checks'

# An invalid manifest is a diagnostic result, not an early exit.
cp "$fx/dot.json" "$S/valid-manifest"
jq '.unexpected=true' "$S/valid-manifest" >"$DOTFILES_DEPLOY/dot.json"
invoke
# message kept: which key the validation rejected, and that the other checks went on and the dependent ones were skipped,
# are only observable in the report lines (needs --json)
[ "$code" -eq 1 ]; grep -q unexpected "$S/out"
grep -Fxq '! profiles and marketplaces skipped: invalid dot.json' "$S/out"
grep -q '^✓ deployed clone present' "$S/out"
mise_checked
grep -q '^✓ repository identities consistent' "$S/out"
not grep -qE '^✓ profile |marketplace Claude|marketplace Codex|Codex copy|clone email' "$S/out"
jq '.deploy.path=42' "$S/valid-manifest" >"$DOTFILES_DEPLOY/dot.json"
invoke
[ "$code" -eq 1 ]; grep -q deploy.path "$S/out" # message kept: same, the rejected key
mise_checked
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
[ "$code" -eq 1 ]; grep -q '/scratch ' "$S/out" # the listed repository path is the data
git -C "$DOT_SANDBOX/scratch" config user.email tester@example.com
invoke
[ "$code" -eq 0 ]
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
  if [ "$1" = alpha ]; then # only one profile ships the mise config: enough to expect mise
    mkdir -p "$d/home/.config/mise"; printf '[tools]\n' >"$d/home/.config/mise/config.toml"
    rm -f "$HOME/.config/mise/config.toml"; ln -s "$d/home/.config/mise/config.toml" "$HOME/.config/mise/config.toml"
  fi
  printf '*.local\n' >"$d/.gitignore"
  printf 'acmecorp\n' >"$d/forbidden.local"
  git -C "$d" add .; git -C "$d" commit -qm fixture
  ln -s "$d/home/example-$1" "$HOME/example-$1"
}
mkprofile alpha; mkprofile beta
printf '{"default":"alpha","profiles":{"alpha":{"repo":"https://git.example.com/acme/a.git"},"beta":{"repo":"https://git.example.com/acme/b.git"}}}\n' >"$HOME/.dot/profiles.json"
invoke
# message kept: the per-profile headers and one report line per check are the only observable of how the report is
# grouped, per profile or once for the machine (needs --json)
[ "$code" -eq 0 ]; [ "$(grep -c '^profile ' "$S/out" | tr -d ' ')" -eq 2 ]
grep -qx 'profile alpha' "$S/out"; grep -qx 'profile beta' "$S/out"
[ "$(grep -n '^profile ' "$S/out" | head -1 | cut -d: -f2)" = 'profile alpha' ]
[ "$(grep -c '^✓ deployed clone present' "$S/out")" -eq 2 ]
[ "$(grep -c '^✓ home links up to date' "$S/out")" -eq 2 ]
for once in 'no dead links' 'mise present' 'mise shims' 'profiles included' 'repository identities' 'marketplace Claude' 'marketplace Codex'; do
  [ "$(grep -c "$once" "$S/out")" -eq 1 ] || { echo "FAIL $once"; exit 1; }
done
# Machine-wide checks come after the last profile.
[ "$(grep -n '^✓ no dead links' "$S/out" | cut -d: -f1)" -gt "$(grep -n '^profile beta' "$S/out" | cut -d: -f1)" ]
echo 'OK   no -p: every registered profile under its header, machine-wide checks once, after them'
rm "$HOME/example-beta"; printf 'detached\n' >"$HOME/example-beta"
invoke
# message kept: which profile section holds the failure is only observable by the headers and the ✗ marker (needs --json)
[ "$code" -eq 1 ]; grep -q '^  ~/example-beta$' "$S/out"
[ "$(sed -n '/^profile beta/,$p' "$S/out" | grep -c '^✗ 1 detached files')" -eq 1 ]
[ "$(sed -n '/^profile alpha/,/^profile beta/p' "$S/out" | grep -c '^✗')" -eq 0 ]
invoke -p alpha
# message kept: headerless output is only observable by the absence of the header lines (needs --json)
[ "$code" -eq 0 ]; ! grep -q '^profile ' "$S/out" || exit 1
[ "$(grep -c '^✓ deployed clone present' "$S/out")" -eq 1 ]
code=0; DOT_PROFILE=beta "$DOT_BIN" doctor >"$S/out" 2>&1 || code=$?
[ "$code" -eq 1 ]; ! grep -q '^profile ' "$S/out" || exit 1; grep -q '~/example-beta' "$S/out"
code=0; DOTFILES_DEPLOY=$HOME/.dot/alpha "$DOT_BIN" doctor >"$S/out" 2>&1 || code=$?
[ "$code" -eq 0 ]; ! grep -q '^profile ' "$S/out" || exit 1
rm "$HOME/example-beta"; ln -s "$HOME/.dot/beta/home/example-beta" "$HOME/example-beta"
echo 'OK   -p, DOT_PROFILE and DOTFILES_DEPLOY target one profile without header; a detached file fails only its profile'
jq '.unexpected=true' "$fx/dot.json" >"$HOME/.dot/beta/dot.json"
invoke
# message kept: the rejected key and the section holding the report are only observable in the report lines (needs --json)
[ "$code" -eq 1 ]; grep -q unexpected "$S/out"
grep -Fxq '! profiles and marketplaces skipped: invalid dot.json' "$S/out"
[ "$(sed -n '/^profile alpha/,/^profile beta/p' "$S/out" | grep -c 'invalid dot.json')" -eq 0 ]
cp "$fx/dot.json" "$HOME/.dot/beta/dot.json"
echo 'OK   invalid manifest of one profile is reported under its header'
printf '{"default":"alpha","profiles":{"alpha":{"repo":"https://git.example.com/acme/a.git"}}}\n' >"$HOME/.dot/profiles.json"
invoke
[ "$code" -eq 0 ]; ! grep -q '^profile ' "$S/out" || exit 1
echo 'OK   a single registered profile prints exactly the headerless report'
invoke -p nope
[ "$code" -eq 1 ]
mise_checked
rm "$HOME/.dot/profiles.json"
invoke
[ "$code" -eq 1 ]
mise_checked; ! grep -q '^profile ' "$S/out" || exit 1 # message kept: no header, as above
echo 'OK   unknown profile and empty registry fail, machine-wide checks still run'
