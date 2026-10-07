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
for args in "" help -h --help; do
  out=$(dot $args)
  grep -q '^usage : dot <commande>' <<<"$out" && grep -q '^  clone ' <<<"$out" || ko "dot $args : aide complète"
  grep -q '^  repos ' <<<"$out" && grep -q '^  status ' <<<"$out" && grep -q '^  terms ' <<<"$out" || ko "dot $args : commandes absentes"
done
ok "dot, dot help, -h, --help : aide complète"
out=$(dot clone --help)
grep -q '^usage : dot clone \[--path\]' <<<"$out" && grep -q 'sous-groupes' <<<"$out" &&
  ! grep -q 'dot send' <<<"$out" || ko "dot clone --help"
[ "$(dot help clone)" = "$out" ] || ko "dot help clone = dot clone --help"
ok "dot <cmd> --help et dot help <cmd> : cette commande seule, avec ses détails"
[ "$(dot help status)" = "$(dot status --help)" ] || ko "alias status"
ok "alias : dot help status = dot status --help"
if dot help nope >/dev/null 2>"$S/err"; then ko "commande inconnue acceptée"; else [ $? -eq 2 ] || ko "code"; fi
grep -q 'commande inconnue : nope' "$S/err" || ko "message de commande inconnue"
ok "dot help <inconnue> : code 2"
! grep -q $'\e\\[' <<<"$(dot)" || ko "couleurs hors terminal"
! grep -q $'\e\\[' <<<"$(dot clone --help)" || ko "couleurs hors terminal (clone)"
ok "pas de couleur hors terminal"
git init -q "$DOTFILES_DEPLOY"
git -C "$DOTFILES_DEPLOY" remote add origin https://git.example.com/acme/dots.git
for state in absent comments; do
  if [ "$state" = comments ]; then printf ' # commentaire\n\n \t\n' >"$DOTFILES_DEPLOY/forbidden.local"; fi
  code=0; dot terms >"$S/out" 2>"$S/err" || code=$?
  [ "$code" -eq 1 ] && [ ! -e "$FAKE_ARGV" ] && [ ! -e "$FAKE_INPUT" ] || ko "F5 : $state"
done
printf '# fictional terms\nacmecorp\n\nglobex\n' >"$DOTFILES_DEPLOY/forbidden.local"
dot terms >"$S/out" 2>"$S/err"
printf 'acmecorp\nglobex\n' >"$S/expected"
cmp -s "$S/expected" "$FAKE_INPUT" || ko 'F5 : stdin'
printf 'secret\nset\nFORBIDDEN_TERMS\n-R\ngit.example.com/acme/dots\n' >"$S/expected"
cmp -s "$S/expected" "$FAKE_ARGV" || ko 'F5 : argv'
grep -q 'secret FORBIDDEN_TERMS mis à jour' "$S/out" || ko 'F5 : message'
ok 'F5 : terms refuse les listes absentes ou vides ; termes transmis sur stdin'
rm "$FAKE_ARGV" "$FAKE_INPUT"
git -C "$DOTFILES_DEPLOY" remote remove origin
if dot terms >"$S/out" 2>"$S/err"; then ko 'origin absent'; fi
grep -q 'origine absente' "$S/err" && [ ! -e "$FAKE_ARGV" ] || ko 'gh called without origin'
git -C "$DOTFILES_DEPLOY" remote add origin invalid
if dot terms >"$S/out" 2>"$S/err"; then ko 'invalid origin'; fi
[ ! -e "$FAKE_ARGV" ] || ko 'gh called with invalid origin'
ok 'M6 : origine absente ou invalide refuse gh'
echo "== $pass OK, 0 FAIL"
