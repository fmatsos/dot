#!/usr/bin/env bash
# dot ai skills with fictional sources, an isolated HOME and a stateful npx stub.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
. "$root/tests/lib.sh" # builds DOT_BIN before HOME is redirected
S=$(cd "$(mktemp -d)" && pwd -P); trap 'rm -rf "$S"' EXIT
export HOME=$S/home DOTFILES_DEPLOY=$S/deploy STATE=$S/state.json CALLS=$S/calls
mkdir -p "$S/bin" "$HOME" "$DOTFILES_DEPLOY/home/.config/skills"
export PATH=$S/bin:$PATH
: >"$CALLS"
# npx -y skills@<pin> list|add|remove: the installed skills live in $STATE as [{name, source}].
cat >"$S/bin/npx" <<'BASH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$CALLS"
[ "$1" = -y ] && [[ $2 == skills@* ]] || exit 1
shift 2
case $1 in
list) [ "$2 $3" = "-g --json" ]; cat "$STATE" ;;
add)
  src=$2; shift 2
  [ "$1 $2 $3" = "-g -y -s" ]; shift 3
  for n in "$@"; do
    jq --arg n "$n" --arg s "${src%%#*}" 'map(select(.name != $n)) + [{name:$n,source:$s}]' "$STATE" >"$STATE.tmp"
    mv "$STATE.tmp" "$STATE"
  done ;;
update | find) ;;
remove)
  shift
  while [ "$1" != -g ]; do
    jq --arg n "$1" 'map(select(.name != $n))' "$STATE" >"$STATE.tmp"; mv "$STATE.tmp" "$STATE"; shift
  done
  [ "$*" = "-g -y" ] ;;
*) exit 1 ;;
esac
BASH
chmod +x "$S/bin/npx"
printf '[{"name":"hand","source":null},{"name":"old","source":"acme/old"}]\n' >"$STATE"
printf '{"acme/tools#v1": ["lint", "fmt"]}\n' >"$DOTFILES_DEPLOY/home/.config/skills/skills.json"

dot ai skills -n >"$S/out"
[ "$(cat "$S/out")" = "skills: add fmt (acme/tools#v1)
skills: add lint (acme/tools#v1)
skills: unmanaged hand
skills: remove old" ]
[ "$(grep -cv ' list ' "$CALLS")" = 0 ]
echo 'OK   dry run prints the plan and only lists'

dot ai skills >"$S/out"
[ "$(jq -c 'sort_by(.name)' "$STATE")" = '[{"name":"fmt","source":"acme/tools"},{"name":"hand","source":null},{"name":"lint","source":"acme/tools"}]' ]
grep -qx -- '-y skills@[0-9.]* add acme/tools#v1 -g -y -s fmt lint' "$CALLS"
echo 'OK   the list is applied through the pinned CLI; a skill without source stays'

: >"$CALLS"
dot ai skills >"$S/out"
[ "$(tail -1 "$S/out")" = 'skills: up to date' ] && [ "$(wc -l <"$CALLS")" -eq 1 ]
echo 'OK   second run changes nothing'

mkdir -p "$HOME/.config/skills"; printf '{"acme/tools": null}\n' >"$HOME/.config/skills/skills.local.json"
dot ai skills >"$S/out"
[ "$(jq -c 'map(.name)' "$STATE")" = '["hand"]' ]
echo 'OK   skills.local.json drops a source of the profile'

printf '{"acme/tools": "lint"}\n' >"$HOME/.config/skills/skills.local.json"; : >"$CALLS"
not dot ai skills 2>"$S/err"
[ ! -s "$CALLS" ] && grep -q 'skills.local.json' "$S/err"
echo 'OK   an invalid list stops before any call'

rm "$HOME/.config/skills/skills.local.json"
L=$DOTFILES_DEPLOY/home/.config/skills/skills.json
printf '[{"name":"hand","source":null},{"name":"x","source":"acme/extra"},{"name":"lint","source":"acme/tools"}]\n' >"$STATE"
printf '{"acme/tools#v1": ["fmt"]}\n' >"$L"
dot ai skills adopt -n >"$S/out"
[ "$(jq -c . "$S/out")" = '{"acme/extra":["x"],"acme/tools#v1":["fmt","lint"]}' ] && [ "$(jq -c . "$L")" = '{"acme/tools#v1":["fmt"]}' ]
dot ai skills adopt >"$S/out"
[ "$(jq -c . "$L")" = '{"acme/extra":["x"],"acme/tools#v1":["fmt","lint"]}' ]
echo 'OK   adopt writes the sourced skills into the list, the skill without source stays out'

dot ai skills add acme/new one >"$S/out"
[ "$(jq -c '.["acme/new"]' "$L")" = '["one"]' ] && jq -e 'any(.name == "one" and .source == "acme/new")' "$STATE" >/dev/null
dot ai skills rm one x >"$S/out"
[ "$(jq -c . "$L")" = '{"acme/tools#v1":["fmt","lint"]}' ] && [ "$(jq -c 'map(.name) | sort' "$STATE")" = '["fmt","hand","lint"]' ]
cp "$L" "$S/list"
not dot ai skills rm absent 2>"$S/err"; cmp -s "$L" "$S/list"
not dot ai skills add acme/new -bad 2>"$S/err"; cmp -s "$L" "$S/list"
echo 'OK   add and rm edit the list then apply it; a wrong name changes nothing'

: >"$CALLS"
dot ai skills update >"$S/out"; dot ai skills find lint --owner acme >"$S/out"
[ "$(cut -d' ' -f3- "$CALLS")" = $'update -g -y\nfind lint --owner acme' ]
echo 'OK   update and find run the pinned CLI'
