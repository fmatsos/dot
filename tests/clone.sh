#!/usr/bin/env bash
# Self-check of dot clone with fictional remotes and an isolated HOME.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
source "$root/tests/lib.sh"
S=$(mktemp -d); trap 'rm -rf "$S"' EXIT
export HOME=$S/home DOT_SRC="$S/source repos" GIT_CONFIG_NOSYSTEM=1
export GIT_CONFIG_GLOBAL=$S/gitconfig
mkdir -p "$HOME"
clone() { "$DOT_BIN" clone "$@"; }
reject() {
  local code=0
  clone "$@" >"$S/out" 2>"$S/err" || code=$?
  [ "$code" -eq 2 ]; [ ! -s "$S/out" ]; grep -q 'usage :' "$S/err"
}

[ "$(clone --path https://github.com/alice/tool.git)" = "$DOT_SRC/github.com/alice/tool" ]
[ "$(clone --path git@git.example.com:team/sub/deep/project.git)" = "$DOT_SRC/git.example.com/team/project" ]
[ "$(clone --path ssh://git@git.example.com:2222/team/sub/project)" = "$DOT_SRC/git.example.com/team/project" ]
[ "$(clone --path https://user@git.example.com/team/project/)" = "$DOT_SRC/git.example.com/team/project" ]
[ "$(clone --path https://user@GIT.EXAMPLE.COM:443/team/project.git/)" = "$DOT_SRC/git.example.com/team/project" ]
[ ! -e "$DOT_SRC" ]
echo 'OK   URL mappings, normalized host and dry run without writes'
unset DOT_SRC
[ "$(clone --path https://github.com/alice/tool.git)" = "$HOME/src/github.com/alice/tool" ]
export DOT_SRC="$S/source repos"
echo 'OK   default root is HOME/src'

for url in '' project /team/project file:///team/project ftp://git.example.com/team/project \
  http://git.example.com/team/project https://git.example.com/team \
  git@git.example.com:project ssh://git@git.example.com/team \
  https:///team/project https://git.example.com/team//project \
  https://git.example.com/../project https://git.example.com/team/.git \
  https://git.example.com/team/project?query; do
  reject --path "$url"
done
reject
reject --path
reject --path https://github.com/alice/tool.git extra
[ ! -e "$DOT_SRC" ]
echo 'OK   unsupported URLs, unsafe paths and invalid usage rejected with exit 2'

target=$(clone --path https://git.example.com/team/project.git)
git init -q "$target"
git -C "$target" remote add origin ssh://git@GIT.EXAMPLE.COM:2222/team/sub/project
[ "$(clone https://git.example.com/team/project.git)" = "déjà cloné : $target" ]
echo 'OK   existing repository with equivalent origin is reused'
git -C "$target" remote set-url origin https://git.example.com/other/project.git
cp "$target/.git/config" "$S/before"
code=0
clone https://git.example.com/team/project.git >"$S/out" 2>"$S/err" || code=$?
[ "$code" -eq 1 ]; [ ! -s "$S/out" ]; [ -s "$S/err" ]
cmp -s "$S/before" "$target/.git/config"
echo 'OK   different origin rejected without changes'
git -C "$target" remote remove origin
code=0
clone https://git.example.com/team/project.git >"$S/out" 2>"$S/err" || code=$?
[ "$code" -eq 1 ]; [ ! -s "$S/out" ]; [ -s "$S/err" ]
[ -z "$(git -C "$target" remote)" ]
echo 'OK   repository without origin rejected'

target=$(clone --path https://git.example.com/team/plain.git)
mkdir -p "$target"
printf 'keep\n' >"$target/marker"
code=0
clone https://git.example.com/team/plain.git >"$S/out" 2>"$S/err" || code=$?
[ "$code" -eq 1 ]; [ ! -s "$S/out" ]; [ -s "$S/err" ]
[ "$(cat "$target/marker")" = keep ]; [ ! -e "$target/.git" ]
echo 'OK   existing non-repository rejected without changes'

git init -q "$S/seed"
printf 'fixture\n' >"$S/seed/marker"
git -C "$S/seed" add marker
git -C "$S/seed" -c user.name=Tester -c user.email=tester@example.com commit -qm fixture
git clone -q --bare "$S/seed" "$S/fixture.git"
git config --global url."file://$S/fixture.git".insteadOf https://git.example.com/team/fresh.git
target=$(clone --path https://git.example.com/team/fresh.git)
[ "$(clone https://git.example.com/team/fresh.git --quiet --no-hardlinks)" = "$target" ]
[ "$(cat "$target/marker")" = fixture ]
[ "$(git -C "$target" remote get-url origin)" = "file://$S/fixture.git" ]
[ "$(git -C "$target" config remote.origin.url)" = https://git.example.com/team/fresh.git ]
[ "$(clone https://git.example.com/team/fresh.git)" = "déjà cloné : $target" ]
echo 'OK   real local clone, extra arguments, parents and printed target'
git config --global url."file://$S/fixture.git".insteadOf https://git.example.com/team/bare.git
target=$(clone --path https://git.example.com/team/bare.git)
[ "$(clone https://git.example.com/team/bare.git --quiet --bare)" = "$target" ]
[ "$(git -C "$target" rev-parse --is-bare-repository)" = true ]
[ "$(clone https://git.example.com/team/bare.git)" = "déjà cloné : $target" ]
echo 'OK   bare clone can also be reused'
mkdir -p "$HOME/.config/git/profiles" "$S/project"
printf '# fictional profile\n' >"$HOME/.config/git/profiles/perso.gitconfig"
for _ in 1 2; do
  (cd "$S" && CDPATH="$S" "$DOT_BIN" profile perso project) >"$S/out" 2>"$S/err"
done
[ "$(grep -Fxc "[includeIf \"gitdir:$S/project/\"]" "$HOME/.config/git/profiles.local")" -eq 1 ]
[ "$(wc -l <"$HOME/.config/git/profiles.local")" -eq 2 ]
grep -q 'règle déjà présente' "$S/out"
cp "$HOME/.config/git/profiles.local" "$S/profiles-before"
for name in ../x acme..demo; do
  code=0; "$DOT_BIN" profile "$name" "$S/project" >"$S/out" 2>"$S/err" || code=$?
  [ "$code" -eq 1 ]; grep -q 'profil : nom invalide' "$S/err"
  cmp -s "$S/profiles-before" "$HOME/.config/git/profiles.local"
done
echo 'OK   F10: profile idempotent, CDPATH ignored, unsafe names rejected'
echo '== 10 OK, 0 FAIL'
