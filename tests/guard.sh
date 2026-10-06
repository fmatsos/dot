#!/usr/bin/env bash
# Self-check of `dot guard` against fictional terms: every case must pass, or block for the
# expected reason. betterleaks is replaced by a fake scanner. Runs locally and in CI: bash tests/guard.sh
set -u
root=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=tests/lib.sh
. "$root/tests/lib.sh"
fx=$root/tests/fixtures/guard
S=$(mktemp -d); trap 'rm -rf "$S"' EXIT
# The repo hooks call `dot` through the PATH.
mkdir "$S/pathbin"; ln -s "$DOT_BIN" "$S/pathbin/dot"; PATH=$S/pathbin:$PATH
printf '%s\n' '# fictional terms' acmecorp 'globex-?inc' '(^|[^a-z0-9])zed([^a-z0-9]|$)' >"$S/terms"
export HOME=$S/home DOTFILES_FORBIDDEN=$S/terms GIT_CONFIG_GLOBAL=$S/gitconfig GIT_CONFIG_NOSYSTEM=1
export DOT_BETTERLEAKS=$fx/fake-betterleaks FAKE_BETTERLEAKS_LOG=$S/scanner.log
unset DOTFILES_DEPLOY DOTFILES_GUARD DOT_PROFILE
mkdir -p "$HOME"
git config --global init.defaultBranch main
git config --global user.name T; git config --global user.email t@example.org

git init -q --bare "$S/remote.git"
mkrepo() { git clone -q "$S/remote.git" "$1" 2>/dev/null || git init -q "$1"
  cp -r "$fx/githooks" "$1/.githooks"; git -C "$1" config core.hooksPath .githooks
  git -C "$1" remote get-url origin >/dev/null 2>&1 || git -C "$1" remote add origin "$S/remote.git"; }
R=$S/a; mkrepo "$R"
ok=0; ko=0
expect() { # expect pass|block <label> <reason-regex> <cmd...>
  local want=$1 label=$2 why=$3; shift 3
  if "$@" >"$S/out" 2>&1; then got=pass; else got=block; fi
  if [ "$got" = "$want" ] && { [ "$want" = pass ] || grep -qE "$why" "$S/out"; }; then
    ok=$((ok+1)); echo "OK   $label"
  else ko=$((ko+1)); echo "FAIL $label → $got (attendu $want /$why/)"; tail -5 "$S/out"; fi
}
c() { git -C "$R" "$@"; }
reset() { c reset -q --hard; c clean -qfd; }
check() { # check <label> <test-command...>: a plain assertion
  local label=$1; shift
  if "$@"; then ok=$((ok+1)); echo "OK   $label"; else ko=$((ko+1)); echo "FAIL $label"; fi
}
c_alias() { c -c alias.check="!dot guard $1" check; } # c_alias 'staged' | 'msg /dev/null'

echo "items and systems, zedd" >"$R/a.txt"; c add a.txt .githooks
expect pass  "commit propre (sous-chaînes anodines)" "" c commit -qm init
expect pass  "push propre" "" c push -q origin main
printf '%s\n' acmecorp 'broken(' >"$S/invalid"
echo 'tenant AcmeCorp' >"$R/invalid.txt"; c add invalid.txt
expect block "F1 : regex invalide bloque staged" "$S/invalid" env DOTFILES_FORBIDDEN="$S/invalid" git -C "$R" -c alias.check='!dot guard staged' check
! grep -Fq 'broken(' "$S/out" || ko=$((ko+1))
printf '%s\n' acmecorp '(a)b)(c' >"$S/invalid2"
expect block "F1 : parenthèses déséquilibrées refusées" "invalide ou illisible" env DOTFILES_FORBIDDEN="$S/invalid2" git -C "$R" -c alias.check='!dot guard staged' check
printf '%s\n' '# rien que des commentaires' '' '   ' >"$S/empty"
expect block "F1 : liste sans terme bloque" "absente ou vide" env DOTFILES_FORBIDDEN="$S/empty" git -C "$R" -c alias.check='!dot guard staged' check
reset
echo propre >"$R/café.txt"; c add café.txt
expect pass "F2 : nom accentué propre" "" c commit -qm accent
printf '%s\n' acmé >"$S/accent"
echo propre >"$R/acmé.txt"; c add acmé.txt
expect block "F2 : terme accentué dans le nom" 'acmé.txt' env DOTFILES_FORBIDDEN="$S/accent" git -C "$R" commit -qm accent
reset
mkdir -p "$S/deploy"
printf '%s\n' acmecorp >"$S/deploy/forbidden.local"
echo AcmeCorp >"$R/deploy.txt"; c add deploy.txt
expect block "F3 : liste dans DOTFILES_DEPLOY" 'deploy.txt' env -u DOTFILES_FORBIDDEN DOTFILES_DEPLOY="$S/deploy" git -C "$R" commit -qm deploy
reset

# Which list applies: git config dotfiles.profile, then the registry default; the list of a
# profile is <home>/.dot/<profile>/forbidden.local.
mkdir -p "$HOME/.dot/work" "$HOME/.dot/perso"
printf '%s\n' acmecorp >"$HOME/.dot/work/forbidden.local"
printf '%s\n' globex-inc >"$HOME/.dot/perso/forbidden.local"
echo AcmeCorp >"$R/profile.txt"; c add profile.txt
c config dotfiles.profile work
expect block "P1 : dotfiles.profile choisit la liste du profil" 'profile.txt' env -u DOTFILES_FORBIDDEN git -C "$R" commit -qm profile
c config dotfiles.profile perso
expect pass  "P1 : la liste d'un autre profil ne s'applique pas" "" env -u DOTFILES_FORBIDDEN git -C "$R" commit -qm profile
c reset -q --hard HEAD~1
c config --unset dotfiles.profile
echo AcmeCorp >"$R/profile.txt"; c add profile.txt
expect block "P2 : sans profil, registre absent" 'aucun profil inscrit' env -u DOTFILES_FORBIDDEN git -C "$R" commit -qm profile
printf '{"default":"work","profiles":{"work":{"repo":"https://example.com/w.git"},"perso":{"repo":"https://example.com/p.git"}}}\n' >"$HOME/.dot/profiles.json"
expect block "P2 : profil par défaut du registre" 'profile.txt' env -u DOTFILES_FORBIDDEN git -C "$R" commit -qm profile
expect pass  "P2 : DOT_PROFILE prime sur le défaut" "" env -u DOTFILES_FORBIDDEN DOT_PROFILE=perso git -C "$R" commit -qm profile
c reset -q --hard HEAD~1
echo AcmeCorp >"$R/profile.txt"; c add profile.txt
c config dotfiles.profile perso
expect block "P3 : DOTFILES_DEPLOY prime sur dotfiles.profile" 'profile.txt' env -u DOTFILES_FORBIDDEN DOTFILES_DEPLOY="$S/deploy" git -C "$R" commit -qm profile
c config dotfiles.profile ../escape
expect block "P3 : clé de profil invalide refusée" 'dotfiles.profile invalide' env -u DOTFILES_FORBIDDEN git -C "$R" commit -qm profile
c config --unset dotfiles.profile
rm -rf "$HOME/.dot"
reset
expect block "P4 : ni liste ni profil sans env" 'aucun profil inscrit' env -u DOTFILES_FORBIDDEN git -C "$R" -c alias.check='!dot guard msg /dev/null' check

# A changed dot.json is validated from the index, with the manifest validator built in.
valid_manifest='{"repo":"https://git.example.com/acme/dots.git","deploy":{"sparse":["home","bin",".githooks"]},"profiles":{"demo":"home/.config/git/profiles/demo.gitconfig"},"deployProfile":"demo","modules":[]}'
printf '%s\n' "$valid_manifest" >"$S/valid-manifest"
printf '%s\n' "${valid_manifest%\}},\"unexpected\":true}" >"$S/invalid-manifest"
cp "$S/valid-manifest" "$R/dot.json"; c add dot.json
cp "$S/invalid-manifest" "$R/dot.json"
expect pass  "L4 : manifeste indexé valide, working tree invalide" '' c_alias staged
c add dot.json
cp "$S/valid-manifest" "$R/dot.json"
expect block "L4 : manifeste indexé invalide, working tree valide" 'clé unexpected' c_alias staged
printf '{"deploy":{"path":42}}\n' >"$R/dot.json"; c add dot.json
expect block "L4 : manifeste incomplet indexé" 'dot.json invalide' c_alias staged
printf '%s\n' '{"acmecorp-key":1}' >"$R/dot.json"; c add dot.json
expect block "L4 : clé du manifeste interdite non affichée" 'clé masquée' c_alias staged
! grep -qi acmecorp "$S/out" || ko=$((ko+1))
reset

echo "tenant = AcmeCorp" >"$R/b.txt"; c add b.txt
expect block "contenu (casse ignorée)" "b.txt" c commit -qm b; reset
echo x >"$R/notes-acmecorp.txt"; c add -A
expect block "nom de fichier" "notes-" c commit -qm c; reset
echo "repo: zed_tickets" >"$R/c.txt"; c add c.txt
expect block "mot entier dans un identifiant" "c.txt" c commit -qm d; reset
echo ok >"$R/d.txt"; c add d.txt
expect block "message de commit" "message de commit" c commit -qm "fix for Globex-Inc"; reset
printf '# Globex-Inc dans un commentaire git\n' >"$S/msg"
expect pass  "message : les lignes # sont ignorées" "" dot guard msg "$S/msg"
printf 'GITHUB_TOKEN=ghp_aB3dE5fG7hI9jK1mN3pQ5rS7tU9vW1xY3zA5\n' >"$R/e.txt"; c add e.txt # betterleaks:allow (fixture)
expect block "secret" "betterleaks" c commit -qm e
! grep -Fq ghp_ "$S/out" || ko=$((ko+1))
reset
: >"$S/scanner.log"
echo ok >"$R/d.txt"; c add d.txt; c commit -qm scanargs
check "scanner appelé comme betterleaks (staged)" grep -Fxq -- '--no-banner --redact git --staged .' "$S/scanner.log"
echo ok >"$R/f.txt"; c add f.txt
expect block "identité" "identité" c -c user.email=me@acmecorp.example commit -qm f; reset
echo ok >"$R/g.txt"; c add g.txt
expect block "liste absente" "absente ou vide" env DOTFILES_FORBIDDEN=/nonexistent git -C "$R" commit -qm g
expect block "scanner sans empreinte figée" "empreinte non figée" env -u DOT_BETTERLEAKS git -C "$R" commit -qm g
expect block "scanner introuvable" "lancement impossible" env DOT_BETTERLEAKS="$S/missing" git -C "$R" commit -qm g
reset
echo "AcmeCorp" >"$R/s.txt"; c add s.txt
expect pass  "mode secrets : termes ignorés, liste inutile" "" env DOTFILES_GUARD=secrets DOTFILES_FORBIDDEN=/nonexistent git -C "$R" commit -qm s
c reset -q --hard HEAD~1
printf 'GITHUB_TOKEN=ghp_aB3dE5fG7hI9jK1mN3pQ5rS7tU9vW1xY3zA5\n' >"$R/s.txt"; c add s.txt # betterleaks:allow (fixture)
expect block "mode secrets : secret toujours bloqué" "betterleaks" env DOTFILES_GUARD=secrets git -C "$R" commit -qm s; reset
echo "client zed" >"$R/h.txt"; c add h.txt; c commit -q --no-verify -m h
: >"$S/scanner.log"
expect block "push après --no-verify" "contenu" c push -q origin main
c reset -q --hard HEAD~1; echo ok >"$R/i.txt"; c add i.txt; c commit -qm i
expect block "nom de branche" "branche" c push -q origin HEAD:acmecorp-sync
expect pass  "push propre après" "" c push -q origin main
check "scanner appelé comme betterleaks (push)" grep -Eq -- '^--no-banner --redact git --log-opts=[0-9a-f]{40}\.\.[0-9a-f]{40} \.$' "$S/scanner.log"
# Removing a term that slipped in earlier must be allowed (only added lines are checked).
echo "zed here" >"$R/l.txt"; c add l.txt; c commit -q --no-verify -m l; c push -q --no-verify origin main
c rm -q l.txt
expect pass  "suppression d'un terme (commit)" "" c commit -qm "remove leftover"
expect pass  "suppression d'un terme (push)" "" c push -q origin main
# Stale clone: B pushes, A (not fetched) force-pushes a bad commit with --no-verify'd history.
B=$S/b; mkrepo "$B"; echo b >"$B/j.txt"; git -C "$B" add j.txt; git -C "$B" commit -qm j; git -C "$B" push -q origin main
echo "zed" >"$R/k.txt"; c add k.txt; c commit -q --no-verify -m k
expect block "push forcé depuis un clone périmé" "contenu" c push -q --force origin main
# A push that deletes a remote ref is ignored; a bad ref name on a new ref is refused.
printf 'refs/heads/gone %040d refs/heads/gone %s\n' 0 "$(c rev-parse HEAD)" | dot guard push >"$S/out" 2>&1
check "suppression d'une ref distante ignorée" test $? -eq 0
printf 'refs/heads/globex-inc %s refs/heads/globex-inc %040d\n' "$(c rev-parse HEAD)" 0 >"$S/refs"
expect block "nom de ref interdit (stdin)" "nom de branche ou de tag interdit" git -C "$R" -c alias.check='!dot guard push <'"$S/refs" check
printf 'n importe quoi\n' >"$S/refs"
expect block "entrée push invalide" "invalide" git -C "$R" -c alias.check='!dot guard push <'"$S/refs" check

# dot guard all: refs, whole history and secrets.
c reset -q --hard origin/main
expect block "all : l'historique contient un terme" "historique" git -C "$R" -c alias.check='!dot guard all' check
G=$S/g; git init -q "$G"; echo clean >"$G/x.txt"; git -C "$G" add x.txt; git -C "$G" commit -qm clean
: >"$S/scanner.log"
expect pass  "all : dépôt propre" "" git -C "$G" -c alias.check='!dot guard all' check
check "scanner appelé comme betterleaks (all)" grep -Fxq -- '--no-banner --redact git .' "$S/scanner.log"
git -C "$G" branch globex-inc
expect block "all : nom de branche interdit" "branche" git -C "$G" -c alias.check='!dot guard all' check
git -C "$G" branch -q -D globex-inc
printf 'GITHUB_TOKEN=ghp_aB3dE5fG7hI9jK1mN3pQ5rS7tU9vW1xY3zA5\n' >"$G/t.txt"; git -C "$G" add t.txt; git -C "$G" commit -qm tok # betterleaks:allow (fixture)
expect block "all : secret dans l'historique" "betterleaks" git -C "$G" -c alias.check='!dot guard all' check
expect block "usage invalide" "usage" dot guard msg

echo "== $ok OK, $ko FAIL"; [ "$ko" = 0 ]
