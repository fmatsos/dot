#!/usr/bin/env bash
# Self-check with fictional values: never invoke the installed password managers.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
. "$root/tests/lib.sh"
S=$(mktemp -d "$root/tests/.secret.XXXXXX"); trap 'rm -rf "$S"' EXIT
export HOME=$S/home DOTFILES_DEPLOY=$S/deploy FAKE_LOG=$S/argv
export CODEX_HOME=$HOME/.codex XDG_CONFIG_HOME=$HOME/.config FAKE_STORE=$S/store FAKE_STDIN=$S/stdin
export BW_SESSION='' FAKE_BW_STATUS=unlocked
mkdir -p "$S/bin" "$DOTFILES_DEPLOY" "$HOME" "$FAKE_STORE" "$FAKE_STDIN"
export PATH=$S/bin:$PATH
: >"$FAKE_LOG"; : >"$S/stderr"
cat >"$S/bin/bw" <<'BASH'
#!/usr/bin/env bash
set -euo pipefail
[ "${DOT_SECRET_VALUE+x}" != x ] || exit 99
printf 'bw' >>"$FAKE_LOG"; printf ' <%s>' "$@" >>"$FAKE_LOG"; printf '\n' >>"$FAKE_LOG"
case $1 in
unlock)
  # The master password must come through --passwordenv, never argv.
  [ "${2:-}" = --passwordenv ] && [ "${!3:-}" = fake-master-789 ] || { echo 'bad password' >&2; exit 1; }
  printf 'fake-session-123'; echo fake-session-123 >&2 ;;
status)
  [ "${BW_SESSION:-}" = fake-session-123 ] || exit 1
  printf '{ "status": "%s" }\n' "$FAKE_BW_STATUS" ;;
get)
  if [ "$2" = template ] && [ "$3" = item ]; then echo '{"type":null,"name":null,"notes":"Some notes about this item.","login":null}'; exit; fi
  [ "$2" = password ] || exit 1
  if [ -f "$FAKE_STORE/bw-$3" ]; then
    case ${FAKE_READBACK:-match} in mismatch) echo different ;; fail) exit 1 ;; *) cat "$FAKE_STORE/bw-$3" ;; esac
    exit
  fi
  case $3 in
  'Chat login') printf 'fake-xoxc-123\n'; echo fake-xoxc-123 >&2 ;;
  Newlines) printf 'fake-newlines\n\n\n' ;;
  Multiline) printf 'fake-line1\r\nfake-line2\n' ;;
  Empty) ;;
  Fail) echo fake-xoxc-123; echo fake-xoxc-123 >&2; exit 1 ;;
  *) exit 1 ;;
  esac ;;
list)
  [ "$*" = "list items --search $4" ] || exit 1
  case ${FAKE_BW_SEARCH:-exact} in
  ambiguous) printf '[{"name":"ACME"},{"name":"ACME staging"}]\n' ;;
  near) printf '[{"name":"ACME staging"}]\n' ;;
  *) jq -cn --arg n "$4" --argjson count "${FAKE_BW_MATCHES:-0}" '[range($count) | {name:$n}]' ;;
  esac ;;
encode)
  cat >"$FAKE_STDIN/bw.encode"
  base64 <"$FAKE_STDIN/bw.encode" ;;
create)
  [ "$2" = item ] || exit 1
  cat >"$FAKE_STDIN/bw.create"
  base64 -d <"$FAKE_STDIN/bw.create" >"$FAKE_STDIN/bw.json"
  name=$(jq -r '.name' "$FAKE_STDIN/bw.json")
  jq -e '.type == 1' "$FAKE_STDIN/bw.json" >/dev/null
  jq -r '.login.password' "$FAKE_STDIN/bw.json" >"$FAKE_STORE/bw-$name" ;;
lock) ;;
*) exit 1 ;;
esac
BASH
cat >"$S/bin/pass-cli" <<'BASH'
#!/usr/bin/env bash
set -euo pipefail
[ "${DOT_SECRET_VALUE+x}" != x ] || exit 99
printf 'pass-cli' >>"$FAKE_LOG"; printf ' <%s>' "$@" >>"$FAKE_LOG"; printf '\n' >>"$FAKE_LOG"
if [ "$*" = info ]; then [ "${FAKE_PASS_STATUS:-connected}" = connected ]; exit; fi
if [ "$*" = 'item create login --get-template' ]; then
  echo '{"title":"","password":"","username":"","urls":[]}'
  exit
fi
if [ "${1:-} ${2:-} ${3:-}" = 'item create login' ]; then
  [ "$4" = --vault-name ] && [ "$5" = Personal ] && [ "$6" = --from-template ] && [ "$7" = - ] || exit 1
  cat >"$FAKE_STDIN/pass.create"
  name=$(jq -r '.title' "$FAKE_STDIN/pass.create")
  jq -r '.password' "$FAKE_STDIN/pass.create" >"$FAKE_STORE/pass-$name"
  exit
fi
if [ "${4:-}" = Personal ] && [ -f "$FAKE_STORE/pass-${6:-}" ]; then
  case ${FAKE_READBACK:-match} in mismatch) echo different ;; fail) exit 1 ;; *) cat "$FAKE_STORE/pass-$6" ;; esac
  exit
fi
[ "$#" -eq 8 ] && [ "$1" = item ] && [ "$2" = view ] &&
  [ "$3" = --vault-name ] && [ "$4" = Développement ] &&
  [ "$5" = --item-title ] && [ "$7" = --field ] && [ "$8" = password ] || exit 1
case $6 in
'API login with spaces') printf 'fake-api-456\n'; echo fake-api-456 >&2 ;;
Empty) ;;
Fail) echo fake-api-456; echo fake-api-456 >&2; exit 1 ;;
*) exit 1 ;;
esac
BASH
chmod +x "$S/bin/bw" "$S/bin/pass-cli"
cat >"$DOTFILES_DEPLOY/secrets.local" <<'MAP'
# Fictional mappings, parsed as data
CHAT_TOKEN=bw:Chat login
API_TOKEN=pass:Développement/API login with spaces
EMPTY_BW=bw:Empty
EMPTY_PASS=pass:Développement/Empty
FAIL_BW=bw:Fail
FAIL_PASS=pass:Développement/Fail
NEWLINE_TOKEN=bw:Newlines
MULTI_TOKEN=bw:Multiline
MAP
printf '%s' fake-session-123 >"$DOTFILES_DEPLOY/bw-session.local"
secret() { "$DOT_BIN" secrets "$@"; }
ok=0; ko=0
check() {
  local label=$1; shift
  if "$@"; then ok=$((ok+1)); echo "OK   $label"
  else ko=$((ko+1)); echo "FAIL $label"; fi
}
invoke() {
  if secret "$@" >"$S/out" 2>"$S/err"; then rc=0; else rc=$?; fi
  cat "$S/err" >>"$S/stderr"
}
get_is() {
  invoke get "$1"
  printf '%s' "$2" >"$S/expected"
  [ "$rc" -eq 0 ] && cmp -s "$S/expected" "$S/out"
}
unknown_fails() { [ "$rc" -ne 0 ] && grep -q 'UNKNOWN : NAME inconnu' "$S/err"; }
locked_status() {
  [ "$rc" -eq 0 ] && grep -Fxq 'CHAT_TOKEN (bw) : verrouillé' "$S/out" &&
    grep -Fxq 'API_TOKEN (pass) : lisible' "$S/out"
}
no_values() { ! grep -Eq 'fake-(xoxc|api|session)-' "$1"; }
printf 'fake-master-789\n' >"$S/master"
no_master() { ! grep -Fq -f "$S/master" "$1"; }
lock_clean() { [ "$rc" -eq 0 ] && [ ! -e "$DOTFILES_DEPLOY/bw-session.local" ]; }
if command -v script >/dev/null; then
  # Give unlock a terminal while still using only our fake bw.
  printf -v unlock_cmd '%q secrets unlock' "$DOT_BIN"
  # The master password is typed into the terminal, as a user would.
  unlock_terminal() { { sleep 1; printf 'fake-master-789\n'; sleep 1; } |
    in_pty "$S/tty" "$unlock_cmd" >"$S/out" 2>"$S/err"; }
  chmod 644 "$DOTFILES_DEPLOY/bw-session.local"
  check 'unlock from terminal succeeds' unlock_terminal
  cat "$S/err" >>"$S/stderr"
  printf '%s' fake-session-123 >"$S/expected"
  check 'unlock caches only the session' cmp -s "$S/expected" "$DOTFILES_DEPLOY/bw-session.local"
  check 'unlock replaces old cache with private permissions' test "$(stat -c '%a' "$DOTFILES_DEPLOY/bw-session.local" 2>/dev/null || stat -f %Lp "$DOTFILES_DEPLOY/bw-session.local")" = 600
  check 'unlock never prints the session' no_values "$S/tty"
  check 'unlock prompts for the master password' grep -q 'Mot de passe maître Bitwarden' "$S/tty"
  check 'unlock never echoes the master password' no_master "$S/tty"
  check 'master password never in argv' no_master "$FAKE_LOG"
fi
check 'get bw, no trailing newline' get_is CHAT_TOKEN fake-xoxc-123
check 'get pass, accented vault and spaced title' get_is API_TOKEN fake-api-456
check 'pass uses explicit options' grep -Fxq 'pass-cli <item> <view> <--vault-name> <Développement> <--item-title> <API login with spaces> <--field> <password>' "$FAKE_LOG"

cat >"$S/child" <<'BASH'
#!/usr/bin/env bash
set -euo pipefail
[ "$CHAT_TOKEN" = fake-xoxc-123 ] && [ "$API_TOKEN" = fake-api-456 ]
BASH
export CHAT_TOKEN=parent-only
unset API_TOKEN
invoke run CHAT_TOKEN API_TOKEN -- bash "$S/child"
check 'run: child receives both values' test "$rc" -eq 0
check 'run: parent environment unchanged' test "$CHAT_TOKEN/${API_TOKEN-unset}" = parent-only/unset
invoke run CHAT_TOKEN -- bash -c 'exit 42'
check 'run propagates exit code' test "$rc" -eq 42
cat >"$S/newlines" <<'BASH'
#!/usr/bin/env bash
set -euo pipefail
printf 'fake-newlines\n\n' | od -c >"$FAKE_STDIN/newlines.expected"
printf '%s' "$NEWLINE_TOKEN" | od -c >"$FAKE_STDIN/newlines.actual"
cmp -s "$FAKE_STDIN/newlines.expected" "$FAKE_STDIN/newlines.actual"
BASH
invoke run NEWLINE_TOKEN -- bash "$S/newlines"
check 'F9: run preserves both secret trailing newlines' test "$rc" -eq 0
cat >"$S/multi" <<'BASH'
#!/usr/bin/env bash
set -euo pipefail
printf 'fake-line1\r\nfake-line2' | cmp -s - <(printf '%s' "$MULTI_TOKEN")
BASH
invoke run MULTI_TOKEN -- bash "$S/multi"
check 'run keeps inner newlines and CR of a multi-line value, strips one final newline' test "$rc" -eq 0
check 'multi-line value never in argv' no_values "$FAKE_LOG"
invoke get UNKNOWN
check 'unknown NAME fails clearly' unknown_fails

export FAKE_BW_STATUS=locked
invoke get CHAT_TOKEN
check 'locked bw: exit 3' test "$rc" -eq 3
check 'locked bw: unlock instruction' grep -Fxq 'Bitwarden verrouillé : lance « dot secrets unlock »' "$S/err"
invoke status
check 'status reports locked bw and readable pass' locked_status
export FAKE_BW_STATUS=unauthenticated
invoke get CHAT_TOKEN
check 'unauthenticated bw: exit 3' test "$rc" -eq 3
export FAKE_BW_STATUS=unlocked
rm "$DOTFILES_DEPLOY/bw-session.local"
invoke get CHAT_TOKEN
check 'absent session: exit 3' test "$rc" -eq 3
printf '%s' fake-session-123 >"$DOTFILES_DEPLOY/bw-session.local"

for name in EMPTY_BW EMPTY_PASS FAIL_BW FAIL_PASS; do
  invoke get "$name"
  check "$name rejected" test "$rc" -ne 0
  check "$name has no stdout" test ! -s "$S/out"
done
invoke run CHAT_TOKEN EMPTY_PASS -- bash -c 'exit 42'
check 'run aborts when a value is empty' test "$rc" -eq 1
invoke status
check 'status succeeds even for unreadable items' test "$rc" -eq 0
check 'status lists backend/readability' grep -Fxq 'CHAT_TOKEN (bw) : lisible' "$S/out"
check 'status never prints a value' no_values "$S/out"

cp "$DOTFILES_DEPLOY/secrets.local" "$S/mapping"
for line in 'BAD=bw:' 'BAD=pass:/Title' 'BAD=pass:Vault/' 'BAD=other:Title' 'not a mapping' 'CHAT_TOKEN=bw:Duplicate'; do
  cp "$S/mapping" "$DOTFILES_DEPLOY/secrets.local"
  printf '%s\n' "$line" >>"$DOTFILES_DEPLOY/secrets.local"
  invoke get CHAT_TOKEN
  check 'malformed/duplicate mapping fails' test "$rc" -ne 0
  check 'malformed mapping does not echo contents' grep -q '^secret : ligne [0-9]' "$S/err"
done
for bad in $'CRLF_TOKEN=bw:Chat login\r' $'\xef\xbb\xbfBOM_TOKEN=bw:Chat login' 'BAD-NAME=bw:Chat login' '1BAD=bw:Chat login' 'SECRET_IN_REF=bw:' 'CHAT_TOKEN=pass:Vault/Dup'; do
  cp "$S/mapping" "$DOTFILES_DEPLOY/secrets.local"
  printf '%s\n' "$bad" >>"$DOTFILES_DEPLOY/secrets.local"
  invoke get CHAT_TOKEN
  check 'CRLF/BOM/invalid name/duplicate: refused, line number only' grep -Eq '^secret : ligne [0-9]+ ' "$S/err"
  check 'edge mapping: no value or item name echoed' test "$(grep -c 'Chat login\|Vault/Dup' "$S/err")" -eq 0
done
cp "$S/mapping" "$DOTFILES_DEPLOY/secrets.local"
rm "$DOTFILES_DEPLOY/secrets.local"
invoke get CHAT_TOKEN
check 'missing mapping file fails' test "$rc" -ne 0
invoke lock
check 'lock works without mapping file and removes cache' lock_clean
invoke
check 'no arguments prints usage' grep -q '^dot secrets — ' "$S/out"

# Creation uses only stdin/scoped jq env. Assertions never echo fixture values on failure.
printf '%s' fake-session-123 >"$DOTFILES_DEPLOY/bw-session.local"
fixture='fake-new-value-789'
printf '%s\n' "$fixture" >"$S/value"
no_fixture() { ! grep -Fq -f "$S/value" "$@"; }
invoke add ACME_TOKEN 'bw:Created BW' <"$S/value"
check 'add bw from non-tty stdin succeeds without an existing mapping file' test "$rc" -eq 0
check 'add bw creates a private mapping' test "$(stat -c '%a' "$DOTFILES_DEPLOY/secrets.local" 2>/dev/null || stat -f %Lp "$DOTFILES_DEPLOY/secrets.local")" = 600
check 'add bw registers the reference' grep -Fxq 'ACME_TOKEN=bw:Created BW' "$DOTFILES_DEPLOY/secrets.local"
check 'bw password goes in JSON before encoding, not argv' cmp -s "$S/value" "$FAKE_STORE/bw-Created BW"
check 'bw create receives the encoded JSON on stdin' test -s "$FAKE_STDIN/bw.create"
notes_null() { jq -e 'has("notes") and .notes == null' "$FAKE_STDIN/bw.json" >/dev/null; }
check 'F7: bw creation clears template notes' notes_null
check 'bw raw creation JSON contains the value' grep -Fq -f "$S/value" "$FAKE_STDIN/bw.encode"
cp "$DOTFILES_DEPLOY/secrets.local" "$S/before-add"
invoke add ACME_TOKEN 'bw:Another BW' <"$S/value"
check 'duplicate NAME refused' test "$rc" -eq 1
check 'duplicate NAME leaves mapping unchanged' cmp -s "$S/before-add" "$DOTFILES_DEPLOY/secrets.local"
for ref in 'bw:' 'pass:/Title' 'pass:Personal/' 'other:Title' $'bw:Title\nOTHER=bw:Other'; do
  invoke add BAD_TOKEN "$ref" <"$S/value"
  check 'bad add reference refused' test "$rc" -eq 1
done
invoke add 1_BAD 'bw:Another BW' <"$S/value"
check 'invalid NAME refused' test "$rc" -eq 1
export FAKE_BW_STATUS=locked
invoke add LOCKED_TOKEN 'bw:Locked BW' <"$S/value"
check 'add locked bw exits 3 before creation' test "$rc" -eq 3
export FAKE_BW_STATUS=unlocked FAKE_BW_MATCHES=2
invoke add AMBIGUOUS_TOKEN 'bw:Ambiguous BW' </dev/null
check 'multiple exact bw names refused' test "$rc" -eq 1
export FAKE_BW_MATCHES=1
rm "$FAKE_STDIN/bw.create"
invoke add EXISTING_TOKEN 'bw:Existing BW' </dev/null
check 'one exact bw match reused without input' test "$rc" -eq 0
check 'existing bw item never created' test ! -e "$FAKE_STDIN/bw.create"
check 'reuse reported' grep -q 'élément existant réutilisé' "$S/out"
export FAKE_BW_MATCHES=0
for mode in ambiguous near; do
  export FAKE_BW_SEARCH=$mode
  cp "$DOTFILES_DEPLOY/secrets.local" "$S/before-add"
  invoke add AMBIGUOUS_TOKEN bw:ACME </dev/null
  check "F6: $mode bw search refused before input" test "$rc" -eq 1
  check 'ambiguous search explains bw get and specific naming' grep -q 'recherche ambiguë pour bw get.*nom plus précis' "$S/err"
  check 'ambiguous search never creates' test ! -e "$FAKE_STDIN/bw.create"
  check 'ambiguous search leaves mapping unchanged' cmp -s "$S/before-add" "$DOTFILES_DEPLOY/secrets.local"
done
unset FAKE_BW_SEARCH
export FAKE_PASS_STATUS=logged-out
: >"$FAKE_LOG"
invoke add LOGGED_OUT_TOKEN 'pass:Personal/Logged out' </dev/null
check 'F8: logged-out pass exits 3 before input' test "$rc" -eq 3
check 'pass login instruction' grep -Fxq 'Proton Pass : non connecté (pass-cli login)' "$S/err"
check 'pass probe precedes any item or create call' test "$(cat "$FAKE_LOG")" = 'pass-cli <info>'
check 'logged-out pass leaves mapping unchanged' cmp -s "$S/before-add" "$DOTFILES_DEPLOY/secrets.local"
unset FAKE_PASS_STATUS
invoke add PASS_TOKEN 'pass:Personal/Created Pass' <"$S/value"
check 'add pass with JSON stdin succeeds' test "$rc" -eq 0
check 'pass template receives the value' cmp -s "$S/value" "$FAKE_STORE/pass-Created Pass"
check 'pass creation stdin contains the value' grep -Fq -f "$S/value" "$FAKE_STDIN/pass.create"
rm "$FAKE_STDIN/pass.create"
invoke add PASS_REUSED 'pass:Personal/Created Pass' </dev/null
check 'existing pass item reused without input' test "$rc" -eq 0
check 'existing pass item never created' test ! -e "$FAKE_STDIN/pass.create"
for backend in bw pass; do
  for mode in mismatch fail; do
    export FAKE_READBACK=$mode
    cp "$DOTFILES_DEPLOY/secrets.local" "$S/before-add"
    if [ "$backend" = bw ]; then ref="bw:Unregistered $mode"; else ref="pass:Personal/Unregistered $mode"; fi
    invoke add UNREGISTERED_TOKEN "$ref" <"$S/value"
    check 'failed/differing readback exits 1' test "$rc" -eq 1
    check 'failed/differing readback leaves mapping unchanged' cmp -s "$S/before-add" "$DOTFILES_DEPLOY/secrets.local"
    check 'created but not registered reported' grep -q 'élément créé mais non enregistré' "$S/err"
  done
done
unset FAKE_READBACK
invoke add EMPTY_TOKEN 'bw:Empty new BW' </dev/null
check 'empty stdin rejected' test "$rc" -eq 1
# Two trailing newlines: remove exactly one, keep the other in the manager/readback.
printf '%s\n\n' "$fixture" >"$S/multiline"
invoke add MULTILINE_TOKEN 'bw:Multiline BW' <"$S/multiline"
check 'stdin and readback preserve extra trailing newline' test "$rc" -eq 0
check 'only one stdin newline stripped' cmp -s "$S/multiline" "$FAKE_STORE/bw-Multiline BW"
if command -v script >/dev/null; then
  printf -v add_cmd '%q secrets add TERMINAL_TOKEN %q' "$DOT_BIN" 'bw:Terminal BW'
  add_terminal() { { sleep 1; printf '%s\n' "$fixture"; sleep 1; printf '%s\n' "$fixture"; } |
    in_pty "$S/tty-add" "$add_cmd" >"$S/out" 2>"$S/err"; }
  check 'add terminal hidden double prompt succeeds' add_terminal
  check 'terminal never echoes value' no_fixture "$S/tty-add"
  cp "$DOTFILES_DEPLOY/secrets.local" "$S/before-add"
  printf -v add_cmd '%q secrets add MISMATCH_TOKEN %q' "$DOT_BIN" 'bw:Terminal mismatch'
  if { sleep 1; printf '%s\n' "$fixture"; sleep 1; printf 'different\n'; } |
    in_pty "$S/tty-mismatch" "$add_cmd" >"$S/out" 2>"$S/err"; then rc=0; else rc=$?; fi
  check 'terminal mismatching confirmation refused' test "$rc" -eq 1
  check 'terminal mismatch never registers a mapping' cmp -s "$S/before-add" "$DOTFILES_DEPLOY/secrets.local"
  check 'terminal mismatch never echoes value' no_fixture "$S/tty-mismatch"
fi
check 'new value never in argv' no_fixture "$FAKE_LOG"
check 'new value never in stderr' no_fixture "$S/stderr"
check 'no values in manager argv' no_values "$FAKE_LOG"
check 'no values in stderr, including backend failures' no_values "$S/stderr"
echo "== $ok OK, $ko FAIL"; [ "$ko" -eq 0 ]
