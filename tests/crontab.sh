#!/usr/bin/env bash
# dot install manages a fictional crontab through a stub, in an isolated HOME.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
. "$root/tests/lib.sh" # builds DOT_BIN before HOME is redirected
S=$(cd "$(mktemp -d)" && pwd -P); trap 'rm -rf "$S"' EXIT
export S HOME=$S/home GIT_CONFIG_GLOBAL=$S/gitconfig GIT_CONFIG_NOSYSTEM=1
export XDG_CONFIG_HOME=$HOME/.config XDG_DATA_HOME=$HOME/.local/share XDG_CACHE_HOME=$HOME/.cache
export XDG_STATE_HOME=$HOME/.local/state CODEX_HOME=$HOME/.codex DOTFILES_TOOLS=0
unset DOTFILES_DEPLOY DOT_PROFILE DOTFILES_BACKUP_DAYS
mkdir -p "$HOME" "$S/bin" "$S/remote/home/.config/git/profiles"
export PATH=$S/bin:$PATH
for cli in claude codex opencode bw pass-cli gh mise; do
  printf '#!/bin/bash\nexit 0\n' >"$S/bin/$cli"; chmod +x "$S/bin/$cli"
done
cat >"$S/bin/crontab" <<'SH'
#!/bin/bash
set -euo pipefail
case "$1" in
  -l)
    if [ -f "$S/read-error" ]; then echo 'read refused' >&2; exit 1; fi
    if [ -f "$S/crontab.txt" ]; then cat "$S/crontab.txt";
    else echo 'no crontab for test' >&2; exit 1; fi ;;
  -) cat >"$S/crontab.txt"; echo write >>"$S/writes" ;;
  *) exit 2 ;;
esac
SH
chmod +x "$S/bin/crontab"
cat >"$S/remote/dot.json" <<'JSON'
{
  "repo": "https://example.com/acme/dots.git",
  "deploy": {"sparse": ["home"]},
  "profiles": {"acme": "home/.config/git/profiles/acme.gitconfig"},
  "deployProfile": "acme",
  "modules": ["crontab"]
}
JSON
printf '[user]\n\tname = Acme Example\n\temail = acme@example.com\n' >"$S/remote/home/.config/git/profiles/acme.gitconfig"
printf '0 9 * * 1 $HOME/bin/acme-task\n' >"$S/remote/home/.config/crontab"
git -C "$S/remote" init -q -b main
git -C "$S/remote" config uploadpack.allowFilter true
git -C "$S/remote" add -A
git -C "$S/remote" -c user.name=Fixture -c user.email=fixture@example.com commit -q -m fixture

dot install "$S/remote" -p acme >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
cat >"$S/expected" <<'CRON'
# >>> dot >>>
0 9 * * 1 $HOME/bin/acme-task
# <<< dot <<<
CRON
cmp -s "$S/expected" "$S/crontab.txt"
[ "$(wc -l <"$S/writes")" -eq 1 ]
echo 'OK   absent crontab gets one managed block'

printf '# foreign\n0 7 * * * $HOME/bin/acme-local\n' >>"$S/crontab.txt"
printf '# foreign\n0 7 * * * $HOME/bin/acme-local\n' >>"$S/expected"
dot install >"$S/out" 2>&1
cmp -s "$S/expected" "$S/crontab.txt"
[ "$(grep -cFx '# >>> dot >>>' "$S/crontab.txt")" -eq 1 ]
echo 'OK   foreign lines survive reinstall without a duplicate block'

[ "$(wc -l <"$S/writes")" -eq 1 ]
echo 'OK   unchanged reinstall does not rewrite the crontab'

source=$HOME/.dot/acme/home/.config/crontab
printf '0 10 * * 1 $HOME/bin/acme-task\n' >"$source"
dot install -n >"$S/out" 2>&1
grep -Fq '0 10 * * 1 $HOME/bin/acme-task' "$S/out"
cmp -s "$S/expected" "$S/crontab.txt"
[ "$(wc -l <"$S/writes")" -eq 1 ]
echo 'OK   dry run previews the changed block without writing'

: >"$source"
dot install >"$S/out" 2>&1
printf '# foreign\n0 7 * * * $HOME/bin/acme-local\n' >"$S/expected"
cmp -s "$S/expected" "$S/crontab.txt"
[ "$(wc -l <"$S/writes")" -eq 2 ]
echo 'OK   empty source removes the block and keeps foreign lines'

# Restore the local source before pulling a new version from its fictional remote.
git -C "$HOME/.dot/acme" checkout -- home/.config/crontab
printf '0 11 * * 1 $HOME/bin/acme-task\n' >"$S/remote/home/.config/crontab"
git -C "$S/remote" -c user.name=Fixture -c user.email=fixture@example.com commit -q -am 'update cron'
dot pull >"$S/out" 2>&1
cat >>"$S/expected" <<'CRON'
# >>> dot >>>
0 11 * * 1 $HOME/bin/acme-task
# <<< dot <<<
CRON
cmp -s "$S/expected" "$S/crontab.txt"
[ "$(wc -l <"$S/writes")" -eq 3 ]
echo 'OK   pull installs the updated source and keeps foreign lines'

mkdir -p "$S/second/home/.config/git/profiles"
cat >"$S/second/dot.json" <<'JSON'
{
  "repo": "https://example.com/acme/second.git",
  "deploy": {"sparse": ["home"]},
  "profiles": {"demo": "home/.config/git/profiles/demo.gitconfig"},
  "deployProfile": "demo",
  "modules": ["crontab"]
}
JSON
printf '[user]\n\tname = Demo Example\n\temail = demo@example.com\n' >"$S/second/home/.config/git/profiles/demo.gitconfig"
printf '0 12 * * 2 $HOME/bin/acme-task\n' >"$S/second/home/.config/crontab"
git -C "$S/second" init -q -b main
git -C "$S/second" config uploadpack.allowFilter true
git -C "$S/second" add -A
git -C "$S/second" -c user.name=Fixture -c user.email=fixture@example.com commit -q -m fixture
dot install "$S/second" -p demo >"$S/out" 2>&1 || { cat "$S/out"; exit 1; }
cat >"$S/expected" <<'CRON'
# foreign
0 7 * * * $HOME/bin/acme-local
# >>> dot >>>
0 11 * * 1 $HOME/bin/acme-task
0 12 * * 2 $HOME/bin/acme-task
# <<< dot <<<
CRON
cmp -s "$S/expected" "$S/crontab.txt"
[ ! -e "$HOME/.config/crontab" ]
dot install -p demo >"$S/out" 2>&1
cmp -s "$S/expected" "$S/crontab.txt"
[ "$(wc -l <"$S/writes")" -eq 4 ]
echo 'OK   profiles merge in registry order, including on a targeted reinstall'

touch "$S/read-error"
code=0
dot install >"$S/out" 2>"$S/err" || code=$?
[ "$code" -eq 1 ]
cmp -s "$S/expected" "$S/crontab.txt"
[ "$(wc -l <"$S/writes")" -eq 4 ]
echo 'OK   module failure exits 1 and preserves the crontab'
echo '== 8 OK, 0 FAIL'
