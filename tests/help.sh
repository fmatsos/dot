#!/usr/bin/env bash
# Help rendering by cobra (full help, one command's help, no colors off a terminal) and dot terms.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
source "$root/tests/lib.sh"
S=$(cd "$(mktemp -d)" && pwd -P); trap 'rm -rf "$S"' EXIT
export HOME=$S/home CODEX_HOME=$S/home/.codex XDG_CONFIG_HOME=$S/home/.config
export DOTFILES_DEPLOY=$S/deploy FAKE_ARGV=$S/argv FAKE_INPUT=$S/stdin
mkdir -p "$S/bin" "$HOME" "$DOTFILES_DEPLOY"
export PATH=$S/bin:$PATH
cat >"$S/bin/gh" <<'BASH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" >"$FAKE_ARGV"
cat >"$FAKE_INPUT"
BASH
chmod +x "$S/bin/gh"
pass=0
ok() { echo "OK   $1"; pass=$((pass + 1)); }
ko() { echo "FAIL $1"; exit 1; }
full=$(dot)
[ -n "$full" ] || ko "full help is empty"
for args in help -h --help; do
  [ "$(dot $args)" = "$full" ] || ko "dot $args: same help as dot"
done
# every registered command (cobra's own completion lists them) is in the command list, one per line
names=$(dot __complete '' 2>/dev/null | grep -v '^:' | cut -f1)
[ -n "$names" ] || ko "no registered command"
for name in $names; do
  grep -qE "^ +$name( |\$)" <<<"$full" || ko "dot: command $name missing from the help"
done
ok "dot, dot help, -h, --help: same full help, every command listed"
out=$(dot clone --help)
[ -n "$out" ] && [ "$out" != "$full" ] || ko "dot clone --help"
[ "$(dot help clone)" = "$out" ] || ko "dot help clone = dot clone --help"
ok "dot <cmd> --help and dot help <cmd>: identical, specific to the command"
[ "$(dot help status)" = "$(dot status --help)" ] || ko "status alias"
ok "alias: dot help status = dot status --help"
if dot help nope >/dev/null 2>"$S/err"; then ko "unknown command accepted"; else [ $? -eq 2 ] || ko "code"; fi
ok "dot help <unknown>: exit 2"
! grep -q $'\e\\[' <<<"$(dot)" || ko "colors off a terminal"
! grep -q $'\e\\[' <<<"$(dot clone --help)" || ko "colors off a terminal (clone)"
ok "no color off a terminal"
git init -q "$DOTFILES_DEPLOY"
git -C "$DOTFILES_DEPLOY" remote add origin https://git.example.com/acme/dots.git
for state in absent comments; do
  if [ "$state" = comments ]; then printf ' # comment\n\n \t\n' >"$DOTFILES_DEPLOY/forbidden.local"; fi
  code=0; dot terms >"$S/out" 2>"$S/err" || code=$?
  [ "$code" -eq 1 ] && [ ! -e "$FAKE_ARGV" ] && [ ! -e "$FAKE_INPUT" ] || ko "F5: $state"
done
printf '# fictional terms\nacmecorp\n\nglobex\n' >"$DOTFILES_DEPLOY/forbidden.local"
dot terms >"$S/out" 2>"$S/err"
printf 'acmecorp\nglobex\n' >"$S/expected"
cmp -s "$S/expected" "$FAKE_INPUT" || ko 'F5: stdin'
printf 'secret\nset\nFORBIDDEN_TERMS\n-R\ngit.example.com/acme/dots\n' >"$S/expected"
cmp -s "$S/expected" "$FAKE_ARGV" || ko 'F5: argv'
ok 'F5: terms refuses absent or empty lists; terms passed on stdin'
rm "$FAKE_ARGV" "$FAKE_INPUT"
git -C "$DOTFILES_DEPLOY" remote remove origin
if dot terms >"$S/out" 2>"$S/err"; then ko 'origin absent'; fi
[ ! -e "$FAKE_ARGV" ] || ko 'gh called without origin'
git -C "$DOTFILES_DEPLOY" remote add origin invalid
if dot terms >"$S/out" 2>"$S/err"; then ko 'invalid origin'; fi
[ ! -e "$FAKE_ARGV" ] || ko 'gh called with invalid origin'
ok 'M6: absent or invalid origin refuses gh'
echo "== $pass OK, 0 FAIL"
