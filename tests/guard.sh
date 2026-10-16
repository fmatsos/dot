#!/usr/bin/env bash
# Self-check of `dot guard` against fictional terms: every case must pass, or block for the
# expected reason. betterleaks is replaced by a fake scanner. Runs locally and in CI: bash tests/guard.sh
set -u
# A secret-shaped value built at run time: no token-looking literal ever lands in the repository.
faketoken() { printf 'ghp_%s' "$(head -c 300 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 36)"; }
root=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=tests/lib.sh
. "$root/tests/lib.sh"
fx=$root/tests/fixtures/guard
S=$(cd "$(mktemp -d)" && pwd -P); trap 'rm -rf "$S"' EXIT
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
# expect pass|block <label> <name-regex> <cmd...>: block = non-zero exit status. <name-regex> is only ever a file,
# branch or ref name the guard prints on purpose ("" = none); its wording is never asserted.
expect() {
  local want=$1 label=$2 why=$3; shift 3
  if "$@" >"$S/out" 2>&1; then got=pass; else got=block; fi
  if [ "$got" = "$want" ] && { [ "$want" = pass ] || grep -qE "$why" "$S/out"; }; then
    ok=$((ok+1)); echo "OK   $label"
  else ko=$((ko+1)); echo "FAIL $label → $got (expected $want /$why/)"; tail -5 "$S/out"; fi
}
c() { git -C "$R" "$@"; }
reset() { c reset -q --hard; c clean -qfd; }
check() { # check <label> <test-command...>: a plain assertion
  local label=$1; shift
  if "$@"; then ok=$((ok+1)); echo "OK   $label"; else ko=$((ko+1)); echo "FAIL $label"; fi
}
c_alias() { c -c alias.check="!dot guard $1" check; } # c_alias 'staged' | 'msg /dev/null'

echo "items and systems, zedd" >"$R/a.txt"; c add a.txt .githooks
expect pass  "clean commit (harmless substrings)" "" c commit -qm init
expect pass  "clean push" "" c push -q origin main
printf '%s\n' acmecorp 'broken(' >"$S/invalid"
# Clean content: only an unusable term list can block these, never a term found in the file.
echo 'clean tenant' >"$R/invalid.txt"; c add invalid.txt
expect block "F1: invalid regex blocks staged" "" env DOTFILES_FORBIDDEN="$S/invalid" git -C "$R" -c alias.check='!dot guard staged' check
! grep -Fq 'broken(' "$S/out" || ko=$((ko+1))
printf '%s\n' acmecorp '(a)b)(c' >"$S/invalid2"
expect block "F1: unbalanced parentheses refused" "" env DOTFILES_FORBIDDEN="$S/invalid2" git -C "$R" -c alias.check='!dot guard staged' check
printf '%s\n' 'acmecorp\|globex' >"$S/invalid3"
expect block "F1: escaped GNU operator refused" "" env DOTFILES_FORBIDDEN="$S/invalid3" git -C "$R" -c alias.check='!dot guard staged' check
! grep -Fq 'globex' "$S/out" || ko=$((ko+1))
printf '%s\n' '(?-i)acmecorp' >"$S/invalid4"
expect block "F1: case-disabling flag refused" "" env DOTFILES_FORBIDDEN="$S/invalid4" git -C "$R" -c alias.check='!dot guard staged' check
printf '%s\n' '# comments only' '' '   ' >"$S/empty"
expect block "F1: list without a term blocks" "" env DOTFILES_FORBIDDEN="$S/empty" git -C "$R" -c alias.check='!dot guard staged' check
reset
echo clean >"$R/café.txt"; c add café.txt
expect pass "F2: clean accented name" "" c commit -qm accent
printf '%s\n' acmé >"$S/accent"
echo clean >"$R/acmé.txt"; c add acmé.txt
expect block "F2: accented term in the name (masked)" "" env DOTFILES_FORBIDDEN="$S/accent" git -C "$R" commit -qm accent
! grep -q 'acmé' "$S/out" || ko=$((ko+1))
reset
mkdir -p "$S/deploy"
printf '%s\n' acmecorp >"$S/deploy/forbidden.local"
echo AcmeCorp >"$R/deploy.txt"; c add deploy.txt
expect block "F3: list in DOTFILES_DEPLOY" 'deploy.txt' env -u DOTFILES_FORBIDDEN DOTFILES_DEPLOY="$S/deploy" git -C "$R" commit -qm deploy
reset

# Which list applies: git config dotfiles.profile, then the registry default; the list of a
# profile is <home>/.dot/<profile>/forbidden.local.
mkdir -p "$HOME/.dot/work" "$HOME/.dot/perso"
printf '%s\n' acmecorp >"$HOME/.dot/work/forbidden.local"
printf '%s\n' globex-inc >"$HOME/.dot/perso/forbidden.local"
echo AcmeCorp >"$R/profile.txt"; c add profile.txt
c config dotfiles.profile work
expect block "P1: dotfiles.profile picks the profile list" 'profile.txt' env -u DOTFILES_FORBIDDEN git -C "$R" commit -qm profile
c config dotfiles.profile perso
expect pass  "P1: the list of another profile does not apply" "" env -u DOTFILES_FORBIDDEN git -C "$R" commit -qm profile
c reset -q --hard HEAD~1
c config --unset dotfiles.profile
echo AcmeCorp >"$R/profile.txt"; c add profile.txt
expect block "P2: no profile, no registry" "" env -u DOTFILES_FORBIDDEN git -C "$R" commit -qm profile
printf '{"default":"work","profiles":{"work":{"repo":"https://example.com/w.git"},"perso":{"repo":"https://example.com/p.git"}}}\n' >"$HOME/.dot/profiles.json"
expect block "P2: registry default profile" 'profile.txt' env -u DOTFILES_FORBIDDEN git -C "$R" commit -qm profile
expect pass  "P2: DOT_PROFILE wins over the default" "" env -u DOTFILES_FORBIDDEN DOT_PROFILE=perso git -C "$R" commit -qm profile
c reset -q --hard HEAD~1
echo AcmeCorp >"$R/profile.txt"; c add profile.txt
c config dotfiles.profile perso
expect block "P3: DOTFILES_DEPLOY wins over dotfiles.profile" 'profile.txt' env -u DOTFILES_FORBIDDEN DOTFILES_DEPLOY="$S/deploy" git -C "$R" commit -qm profile
# A usable list sits where an unchecked key would lead: only the key check can block.
mkdir -p "$HOME/escape"; printf '%s\n' nothing-matches >"$HOME/escape/forbidden.local"
c config dotfiles.profile ../escape
expect block "P3: invalid profile key refused" "" env -u DOTFILES_FORBIDDEN git -C "$R" commit -qm profile
c config --unset dotfiles.profile
rm -rf "$HOME/.dot"
reset
expect block "P4: no list and no profile without env" "" env -u DOTFILES_FORBIDDEN git -C "$R" -c alias.check='!dot guard msg /dev/null' check

# A changed dot.json is validated from the index, with the manifest validator built in.
valid_manifest='{"repo":"https://git.example.com/acme/dots.git","deploy":{"sparse":["home","bin",".githooks"]},"profiles":{"demo":"home/.config/git/profiles/demo.gitconfig"},"deployProfile":"demo","modules":[]}'
printf '%s\n' "$valid_manifest" >"$S/valid-manifest"
printf '%s\n' "${valid_manifest%\}},\"unexpected\":true}" >"$S/invalid-manifest"
cp "$S/valid-manifest" "$R/dot.json"; c add dot.json
cp "$S/invalid-manifest" "$R/dot.json"
expect pass  "L4: valid indexed manifest, invalid working tree" '' c_alias staged
c add dot.json
cp "$S/valid-manifest" "$R/dot.json"
expect block "L4: invalid indexed manifest, valid working tree" "" c_alias staged
printf '{"deploy":{"path":42}}\n' >"$R/dot.json"; c add dot.json
expect block "L4: incomplete indexed manifest" "" c_alias staged
printf '%s\n' '{"acmecorp-key":1}' >"$R/dot.json"; c add dot.json
expect block "L4: forbidden manifest key not printed" "" c_alias staged
! grep -qi acmecorp "$S/out" || ko=$((ko+1))
reset

echo "tenant = AcmeCorp" >"$R/b.txt"; c add b.txt
expect block "content (case ignored)" "b.txt" c commit -qm b; reset
echo x >"$R/notes-acmecorp.txt"; c add -A
expect block "file name (masked)" "" c commit -qm c
! grep -qi 'acmecorp' "$S/out" || ko=$((ko+1))
reset
echo "client zed" >"$R/0:a.txt"; c add -- 0:a.txt
expect block "file named like an index stage" "  0:a.txt" c_alias staged; reset
echo "repo: zed_tickets" >"$R/c.txt"; c add c.txt
expect block "whole word in an identifier" "c.txt" c commit -qm d; reset
echo ok >"$R/d.txt"; c add d.txt
expect block "commit message" "" c commit -qm "fix for Globex-Inc"; reset
printf '# Globex-Inc in a git comment\n' >"$S/msg"
expect pass  "message: # lines are ignored" "" dot guard msg "$S/msg"
printf 'GITHUB_TOKEN=%s\n' "$(faketoken)" >"$R/e.txt"; c add e.txt
expect block "secret" "" c commit -qm e
! grep -Fq ghp_ "$S/out" || ko=$((ko+1))
reset
: >"$S/scanner.log"
echo ok >"$R/d.txt"; c add d.txt; c commit -qm scanargs
check "scanner called like betterleaks (staged)" grep -Fxq -- '--no-banner --redact git --staged .' "$S/scanner.log"
echo ok >"$R/f.txt"; c add f.txt
expect block "identity" "" c -c user.email=me@acmecorp.example commit -qm f; reset
echo ok >"$R/g.txt"; c add g.txt
expect block "missing list" "" env DOTFILES_FORBIDDEN=/nonexistent git -C "$R" commit -qm g
# No cached scanner and no way to download it: blocked, never silently skipped (offline by a dead proxy).
mkdir -p "$S/nocache"
expect block "scanner unavailable (empty cache, offline)" "" env -u DOT_BETTERLEAKS -u NO_PROXY -u no_proxy HOME="$S/nocache" HTTPS_PROXY=http://127.0.0.1:9 https_proxy=http://127.0.0.1:9 git -C "$R" commit -qm g
expect block "scanner not found" "" env DOT_BETTERLEAKS="$S/missing" git -C "$R" commit -qm g
reset
echo "AcmeCorp" >"$R/s.txt"; c add s.txt
expect pass  "secrets mode: terms ignored, list unused" "" env DOTFILES_GUARD=secrets DOTFILES_FORBIDDEN=/nonexistent git -C "$R" commit -qm s
c reset -q --hard HEAD~1
printf 'GITHUB_TOKEN=%s\n' "$(faketoken)" >"$R/s.txt"; c add s.txt
expect block "secrets mode: secret still blocked" "" env DOTFILES_GUARD=secrets git -C "$R" commit -qm s; reset
# The same mode, set per repository with `git config dotfiles.guard secrets`.
c config dotfiles.guard secrets
echo "AcmeCorp" >"$R/s.txt"; c add s.txt
expect pass  "dotfiles.guard secrets: forbidden term tolerated" "" git -C "$R" commit -qm s
expect pass  "dotfiles.guard secrets: push without a term list" "" c push -q origin HEAD:refs/heads/secrets-mode
c push -q origin --delete secrets-mode
c reset -q --hard HEAD~1
printf 'GITHUB_TOKEN=%s\n' "$(faketoken)" >"$R/s.txt"; c add s.txt
expect block "dotfiles.guard secrets: secret still blocked" "" git -C "$R" commit -qm s; reset
c config dotfiles.guard nope-mode
echo ok >"$R/s.txt"; c add s.txt
expect block "unknown dotfiles.guard blocks" "" git -C "$R" commit -qm s
! grep -Fq nope-mode "$S/out" || ko=$((ko+1))
expect block "unknown DOTFILES_GUARD blocks" "" env DOTFILES_GUARD=nope-env git -C "$R" commit -qm s
! grep -Fq nope-env "$S/out" || ko=$((ko+1))
expect pass  "DOTFILES_GUARD=secrets wins over dotfiles.guard" "" env DOTFILES_GUARD=secrets git -C "$R" commit -qm s
c reset -q --hard HEAD~1; reset
c config --unset dotfiles.guard
echo "client zed" >"$R/h.txt"; c add h.txt; c commit -q --no-verify -m h
: >"$S/scanner.log"
expect block "push after --no-verify" "" c push -q origin main
c reset -q --hard HEAD~1; echo ok >"$R/i.txt"; c add i.txt; c commit -qm i
expect block "branch name" "" c push -q origin HEAD:acmecorp-sync
expect pass  "clean push afterwards" "" c push -q origin main
check "scanner called like betterleaks (push)" grep -Eq -- '^--no-banner --redact git --log-opts=[0-9a-f]{40}\.\.[0-9a-f]{40} \.$' "$S/scanner.log"
# Removing a term that slipped in earlier must be allowed (only added lines are checked).
echo "zed here" >"$R/l.txt"; c add l.txt; c commit -q --no-verify -m l; c push -q --no-verify origin main
c rm -q l.txt
expect pass  "removing a term (commit)" "" c commit -qm "remove leftover"
expect pass  "removing a term (push)" "" c push -q origin main
# Stale clone: B pushes, A (not fetched) force-pushes a bad commit with --no-verify'd history.
B=$S/b; mkrepo "$B"; echo b >"$B/j.txt"; git -C "$B" add j.txt; git -C "$B" commit -qm j; git -C "$B" push -q origin main
echo "zed" >"$R/k.txt"; c add k.txt; c commit -q --no-verify -m k
expect block "force push from a stale clone" "" c push -q --force origin main
# A push that deletes a remote ref is ignored; a bad ref name on a new ref is refused.
printf 'refs/heads/gone %040d refs/heads/gone %s\n' 0 "$(c rev-parse HEAD)" | dot guard push >"$S/out" 2>&1
check "remote ref deletion ignored" test $? -eq 0
printf 'refs/heads/globex-inc %s refs/heads/globex-inc %040d\n' "$(c rev-parse HEAD)" 0 >"$S/refs"
expect block "forbidden ref name (stdin)" "" git -C "$R" -c alias.check='!dot guard push <'"$S/refs" check
! grep -qi 'globex' "$S/out" || ko=$((ko+1))
printf 'garbage\n' >"$S/refs"
expect block "invalid push input" "" git -C "$R" -c alias.check='!dot guard push <'"$S/refs" check

# Content git hides by default: binary files, -diff attributes, merge resolutions, annotated tags.
H=$S/h; git init -q "$H"; h() { git -C "$H" "$@"; }
echo clean >"$H/x.txt"; h add x.txt; h commit -qm clean
base=$(h rev-parse HEAD)
hpush() { printf '%s\n' "$1" >"$S/refs"; h -c alias.check='!dot guard push <'"$S/refs" check; } # hpush '<lref> <lsha> <rref> <rsha>'
hrange() { hpush "refs/heads/main $2 refs/heads/main $1"; }
hall() { h -c alias.check='!dot guard all' check; }
printf '\0client zed\0\n' >"$H/f.bin"; h add f.bin; h commit -qm bin
expect block "binary (NUL): push" "" hrange "$base" "$(h rev-parse HEAD)"
expect block "binary (NUL): all" "" hall
h reset -q --hard "$base"
# an image or a font is not text: a term matched in its bytes is chance (its name is still checked)
printf '\0client zed\0\n' >"$H/logo.PNG"; h add logo.PNG; h commit -qm media
expect pass "image (.PNG): push" "" hrange "$base" "$(h rev-parse HEAD)"
expect pass "image (.PNG): all" "" hall
h reset -q --hard "$base"
echo '* -diff' >"$H/.gitattributes"; h add .gitattributes; h commit -qm attrs
b2=$(h rev-parse HEAD); echo "client zed" >"$H/p.txt"; h add p.txt; h commit -qm nodiff
expect block "-diff attribute: push" "" hrange "$b2" "$(h rev-parse HEAD)"
expect block "-diff attribute: all" "" hall
h reset -q --hard "$base"
h checkout -q -b side; echo s >"$H/side.txt"; h add side.txt; h commit -qm side; h checkout -q main
echo m >"$H/main.txt"; h add main.txt; h commit -qm main; mbase=$(h rev-parse HEAD)
h merge -q --no-ff --no-commit side >/dev/null; echo "client zed" >"$H/m.txt"; h add m.txt; h commit -qm merge
expect block "merge whose resolution adds the term: push" "" hrange "$mbase" "$(h rev-parse HEAD)"
expect block "merge whose resolution adds the term: all" "" hall
h reset -q --hard "$base"; h branch -q -D side
h tag -a -m 'release for Globex-Inc' v1; tsha=$(h rev-parse v1)
expect block "annotated tag (forbidden message): push" "" hpush "refs/tags/v1 $tsha refs/tags/v1 $(printf '%040d' 0)"
! grep -qi 'globex' "$S/out" || ko=$((ko+1))
expect block "annotated tag (forbidden message): all" "" hall
h tag -d v1 >/dev/null
h -c user.name='Acmecorp Bot' tag -a -m clean v2
expect block "annotated tag (forbidden tagger): all" "" hall
h tag -d v2 >/dev/null
h tag -a -m clean v3
expect pass  "clean annotated tag: push" "" hpush "refs/tags/v3 $(h rev-parse v3) refs/tags/v3 $(printf '%040d' 0)"
expect pass  "clean annotated tag: all" "" hall

# dot guard all: refs, whole history and secrets.
c reset -q --hard origin/main
expect block "all: the history contains a term" "" git -C "$R" -c alias.check='!dot guard all' check
G=$S/g; git init -q "$G"; echo clean >"$G/x.txt"; git -C "$G" add x.txt; git -C "$G" commit -qm clean
: >"$S/scanner.log"
expect pass  "all: clean repository" "" git -C "$G" -c alias.check='!dot guard all' check
check "scanner called like betterleaks (all)" grep -Fxq -- '--no-banner --redact git .' "$S/scanner.log"
git -C "$G" branch globex-inc
expect block "all: forbidden branch name" "" git -C "$G" -c alias.check='!dot guard all' check
git -C "$G" branch -q -D globex-inc
printf 'GITHUB_TOKEN=%s\n' "$(faketoken)" >"$G/t.txt"; git -C "$G" add t.txt; git -C "$G" commit -qm tok
expect block "all: secret in the history" "" git -C "$G" -c alias.check='!dot guard all' check
if dot guard msg >/dev/null 2>&1; then rc=0; else rc=$?; fi; check "invalid usage: code 2" test "$rc" -eq 2

# Edge cases: awkward names, repository states, term-list oddities, submodules.
U=$S/u; git init -q "$U"; cp -r "$fx/githooks" "$U/.githooks"; git -C "$U" config core.hooksPath .githooks
u() { git -C "$U" "$@"; }
echo ok >"$U/ok.txt"; u add .githooks ok.txt
expect pass  "repository without a commit: clean index" "" u commit -qm first
git init -q "$S/u2"; echo "client zed" >"$S/u2/n.txt"; git -C "$S/u2" add n.txt
expect block "repository without a commit: forbidden content" "n.txt" git -C "$S/u2" -c alias.check='!dot guard staged' check
git -C "$S/u2" rm -q --cached -f n.txt
expect pass  "repository without a commit: empty index" "" git -C "$S/u2" -c alias.check='!dot guard staged' check
for n in 'sp ace.txt' '-dash.txt' 'qu"ote.txt' 'back\slash.txt' $'new\nline.txt'; do
  echo "client zed" >"$U/$n"; u add -f -- "$n"
  expect block "odd name with forbidden content: $(printf %q "$n")" "" git -C "$U" -c alias.check='!dot guard staged' check
  ! grep -qi 'zed' "$S/out" || ko=$((ko+1))
  u reset -q; rm -f -- "$U/$n"
done
printf 'clean\n' >"$U/café.txt"; u add café.txt
expect pass  "clean NFC name" "" git -C "$U" -c alias.check='!dot guard staged' check
nfd=$(printf 'cafe\xcc\x81-nfd.txt'); printf 'clean\n' >"$U/$nfd"; u add -- "$nfd"
expect pass  "clean NFD name" "" git -C "$U" -c alias.check='!dot guard staged' check
u reset -q; rm -f -- "$U/$nfd" "$U/café.txt"
echo "acme" >"$U/split.txt"; echo "corp" >>"$U/split.txt"; u add split.txt
expect pass  "term split over two lines: not detected (documented)" "" git -C "$U" -c alias.check='!dot guard staged' check
u reset -q
printf '\0\1client AcmeCorp\0\n' >"$U/blob.bin"; u add blob.bin
expect block "indexed binary: content scanned" "blob.bin" git -C "$U" -c alias.check='!dot guard staged' check
u reset -q; rm -f "$U/blob.bin"
{ head -c 4000000 /dev/zero | tr '\0' 'x'; echo; echo "end AcmeCorp"; } >"$U/big.txt"; u add big.txt
expect block "big file: term at the end" "big.txt" git -C "$U" -c alias.check='!dot guard staged' check
u reset -q; rm -f "$U/big.txt" "$U/split.txt"
# Deleting a file that holds a term, and renaming a clean file, are allowed.
echo "client zed" >"$U/old.txt"; u add old.txt; u commit -q --no-verify -m old
u rm -q old.txt
expect pass  "indexed deletion of a forbidden file" "" git -C "$U" -c alias.check='!dot guard staged' check
u reset -q --hard; u mv ok.txt renamed.txt
expect pass  "clean indexed rename" "" git -C "$U" -c alias.check='!dot guard staged' check
u reset -q --hard
# A gitlink (submodule): never opened, never blocks; its name is checked.
sub=$S/sub; git init -q "$sub"; echo "client zed" >"$sub/s.txt"; git -C "$sub" add s.txt; git -C "$sub" commit -q --no-verify -m s
u update-index --add --cacheinfo "160000,$(git -C "$sub" rev-parse HEAD),vendor/lib"
expect pass  "gitlink: neither opened nor blocking" "" git -C "$U" -c alias.check='!dot guard staged' check
u update-index --add --cacheinfo "160000,$(git -C "$sub" rev-parse HEAD),zed"
expect block "gitlink: the name is checked" "" git -C "$U" -c alias.check='!dot guard staged' check
u reset -q --hard
# Detached HEAD.
u checkout -q --detach; echo "client zed" >"$U/d.txt"; u add d.txt
expect block "detached HEAD: forbidden content" "d.txt" git -C "$U" -c alias.check='!dot guard staged' check
u reset -q --hard; rm -f "$U/d.txt"
# Push contract: new branch (zero remote sha) and deletion (zero local sha).
printf 'refs/heads/new %s refs/heads/new %040d\n' "$(u rev-list --max-parents=0 HEAD)" 0 >"$S/refs"
expect pass  "push of a clean new branch" "" git -C "$U" -c alias.check='!dot guard push <'"$S/refs" check
printf '(delete) %040d refs/heads/zed-old %s\n' 0 "$(u rev-parse HEAD)" >"$S/refs"
expect pass  "deletion push: nothing is sent" "" git -C "$U" -c alias.check='!dot guard push <'"$S/refs" check
printf '(delete) %040d refs/heads/old not-a-sha\n' 0 >"$S/refs"
expect pass  "deletion push: remote sha ignored" "" git -C "$U" -c alias.check='!dot guard push <'"$S/refs" check
printf 'refs/heads/new %s refs/heads/new %040d extra\n' "$(u rev-parse HEAD)" 0 >"$S/refs"
expect block "push: five fields refused" "" git -C "$U" -c alias.check='!dot guard push <'"$S/refs" check
# Commit messages: body, trailer, CRLF.
printf 'fix\n\nReviewed-by: Globex-Inc <x@example.org>\n' >"$S/msg"
expect block "message: term in a trailer" "" dot guard msg "$S/msg"
printf 'fix\r\n\r\nbody globex-inc\r\n' >"$S/msg"
expect block "message: CRLF, term in the body" "" dot guard msg "$S/msg"
! grep -qi 'globex' "$S/out" || ko=$((ko+1))
# Term lists: BOM, CRLF, empty-matching pattern.
printf '\xef\xbb\xbfacmecorp\r\n# note\r\nglobex\r\n' >"$S/bomlist"
echo AcmeCorp >"$U/bom.txt"; u add bom.txt
expect block "list with BOM and CRLF: the first term applies" "bom.txt" env DOTFILES_FORBIDDEN="$S/bomlist" git -C "$U" -c alias.check='!dot guard staged' check
u reset -q # an empty index: only a term list refused at load can block, since nothing is left to match
for p in 'a*' '^' 'x?' '(|a)'; do
  printf '%s\n' acmecorp "$p" >"$S/emptylist"
  expect block "empty-matching term refused ($p)" "" env DOTFILES_FORBIDDEN="$S/emptylist" git -C "$U" -c alias.check='!dot guard staged' check
  # message kept: an empty message is matched by an empty-matching term too, so only the wording tells a refused list from a match
  expect block "empty-matching term refused ($p): msg" "empty string" env DOTFILES_FORBIDDEN="$S/emptylist" git -C "$U" -c alias.check='!dot guard msg /dev/null' check
done
u reset -q --hard

echo "== $ok OK, $ko FAIL"; [ "$ko" = 0 ]
