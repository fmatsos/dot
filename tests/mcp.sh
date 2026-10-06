#!/usr/bin/env bash
# Shared MCP rendering with fictional sources, isolated homes and stateful CLI stubs.
set -Eeuo pipefail
trap 'echo "FAIL mcp line $LINENO" >&2' ERR
root=$(cd "$(dirname "$0")/.." && pwd)
. "$root/tests/lib.sh" # builds DOT_BIN before HOME is redirected
S=$(mktemp -d); trap 'rm -rf "$S"' EXIT
export HOME=$S/home CODEX_HOME=$S/home/.codex XDG_CONFIG_HOME=$S/home/.config
export DOTFILES_DEPLOY=$S/deploy FAKE_CALLS=$S/calls FAKE_WRITES=$S/writes FAKE_READ=$S/secret-read
mkdir -p "$S/bin" "$HOME/.config/mcp" "$HOME/.config/opencode" "$HOME/.local/bin" "$CODEX_HOME" "$DOTFILES_DEPLOY"
export PATH=$S/bin:$PATH
: >"$FAKE_CALLS"; : >"$FAKE_WRITES"
cat >"$S/bin/claude" <<'BASH'
#!/usr/bin/env bash
set -euo pipefail
printf 'claude' >>"$FAKE_CALLS"; printf ' <%s>' "$@" >>"$FAKE_CALLS"; printf '\n' >>"$FAKE_CALLS"
[ "$1" = mcp ] && [ "$3" = -s ] && [ "$4" = user ] || exit 1
printf 'claude %s %s\n' "$2" "$5" >>"$FAKE_WRITES"
case $2 in
remove) jq --arg n "$5" 'del(.mcpServers[$n])' "$HOME/.claude.json" >"$HOME/.claude.tmp" ;;
add-json) jq --arg n "$5" --argjson s "$6" '.mcpServers[$n]=$s' "$HOME/.claude.json" >"$HOME/.claude.tmp" ;;
*) exit 1 ;;
esac
mv "$HOME/.claude.tmp" "$HOME/.claude.json"
BASH
cat >"$S/bin/codex" <<'BASH'
#!/usr/bin/env bash
set -euo pipefail
printf 'codex' >>"$FAKE_CALLS"; printf ' <%s>' "$@" >>"$FAKE_CALLS"; printf '\n' >>"$FAKE_CALLS"
[ "$1" = mcp ] || exit 1
state=$CODEX_HOME/state.json
if [ "$2" = list ]; then [ "$3" = --json ]; cat "$state"; exit; fi
printf 'codex %s %s\n' "$2" "$3" >>"$FAKE_WRITES"
name=$3
case $2 in
remove) jq --arg n "$name" 'map(select(.name != $n))' "$state" >"$state.tmp" ;;
add)
  shift 3
  env_json='{}'
  if [ "$1" = --url ]; then
    transport=$(jq -cn --arg url "$2" '{type:"streamable_http",url:$url,bearer_token_env_var:null,http_headers:null,env_http_headers:null}')
  else
    while [ "$1" = --env ]; do
      env_json=$(printf '%s' "$env_json" | jq -c --arg k "${2%%=*}" --arg v "${2#*=}" '.[$k]=$v')
      shift 2
    done
    [ "$1" = -- ]; shift
    cmd=$1; shift
    args=$(jq -cn --args '$ARGS.positional' -- "$@")
    transport=$(jq -cn --arg command "$cmd" --argjson args "$args" --argjson env "$env_json" '{type:"stdio",command:$command,args:$args,env:$env,env_vars:[],cwd:null}')
  fi
  jq --arg n "$name" --argjson t "$transport" '. + [{name:$n,enabled:true,disabled_reason:null,transport:$t,startup_timeout_sec:null,tool_timeout_sec:null,auth_status:"unsupported"}]' "$state" >"$state.tmp" ;;
*) exit 1 ;;
esac
mv "$state.tmp" "$state"
BASH
cat >"$S/bin/opencode" <<'BASH'
#!/usr/bin/env bash
printf 'opencode called\n' >>"$FAKE_CALLS"
exit 99
BASH
cat >"$HOME/.local/bin/dot" <<'BASH'
#!/usr/bin/env bash
printf 'dot secrets called\n' >>"$FAKE_READ"
exit 99
BASH
for cli in bw pass-cli; do cp "$HOME/.local/bin/dot" "$S/bin/$cli"; done
chmod +x "$S/bin/"* "$HOME/.local/bin/dot"
cp "$root/tests/fixtures/mcp/servers.json" "$HOME/.config/mcp/servers.json"
printf '%s\n' 'ACME_TOKEN=bw:ACME' >"$DOTFILES_DEPLOY/secrets.local"
printf '%s\n' fake-never-resolve-789 >"$S/value"
export ACME_TOKEN=fake-never-resolve-789
cat >"$HOME/.claude.json" <<'JSON'
{"extra":true,"mcpServers":{"foreign":{"command":"fictional","args":["keep"],"extra":"stay"}}}
JSON
cat >"$CODEX_HOME/state.json" <<'JSON'
[{"name":"node_repl","transport":{"type":"stdio","command":"node","args":["repl"],"env":null,"env_vars":["KEEP"],"cwd":"/tmp"},"enabled":true,"extra":{"stay":true},"startup_timeout_sec":123}]
JSON
cat >"$HOME/.config/opencode/config.json" <<'JSON'
{"theme":"demo","mcp":{"foreign":{"type":"local","command":["fictional"],"extra":true}}}
JSON
cp "$root/tests/fixtures/mcp/opencode.json" "$S/versioned.json"
cp "$root/tests/fixtures/mcp/opencode.jsonc" "$S/versioned.jsonc"
ln -s "$S/versioned.json" "$HOME/.config/opencode/opencode.json"
ln -s "$S/versioned.jsonc" "$HOME/.config/opencode/opencode.jsonc"
mcp() { "$DOT_BIN" mcp "$@" >"$S/out" 2>"$S/err"; }
reset_calls() { : >"$FAKE_CALLS"; : >"$FAKE_WRITES"; }
foreign() {
  jq -Sc '.mcpServers.foreign' "$HOME/.claude.json" >"$S/claude-foreign.$1"
  jq -Sc '.[] | select(.name == "node_repl")' "$CODEX_HOME/state.json" >"$S/codex-foreign.$1"
  jq -Sc '[.theme,.mcp.foreign]' "$HOME/.config/opencode/config.json" >"$S/open-foreign.$1"
}
foreign before
cp "$HOME/.config/opencode/config.json" "$S/open-dry-before"
dry_inode=$(stat_inode_mtime "$HOME/.config/opencode/config.json")
mcp -n
[ ! -s "$FAKE_WRITES" ]; grep -Fxq 'claude : ajout codegraph' "$S/out"
cmp -s "$S/open-dry-before" "$HOME/.config/opencode/config.json"
[ "$dry_inode" = "$(stat_inode_mtime "$HOME/.config/opencode/config.json")" ]
foreign dry
for tool in claude codex open; do cmp -s "$S/$tool-foreign.before" "$S/$tool-foreign.dry"; done
echo 'OK   dry run plans changes without CLI writes or config changes'
reset_calls
mcp
[ "$(wc -l <"$FAKE_WRITES")" -eq 4 ]
jq -e '.mcpServers.codegraph == {type:"stdio",command:"codegraph",args:["serve","--mcp"],env:{}}' "$HOME/.claude.json" >/dev/null
jq -e '.mcp["notebooklm-mcp"] == {type:"local",command:["notebooklm-mcp"],environment:{},enabled:true}' "$HOME/.config/opencode/config.json" >/dev/null
echo 'OK   first run adds the shared servers to all installed tools'
cp "$HOME/.claude.json" "$S/claude-before"
cp "$CODEX_HOME/state.json" "$S/codex-before"
cp "$HOME/.config/opencode/config.json" "$S/open-before"
inode=$(stat_inode_mtime "$HOME/.config/opencode/config.json")
reset_calls
mcp
[ ! -s "$FAKE_WRITES" ]
cmp -s "$S/claude-before" "$HOME/.claude.json"
cmp -s "$S/codex-before" "$CODEX_HOME/state.json"
cmp -s "$S/open-before" "$HOME/.config/opencode/config.json"
[ "$inode" = "$(stat_inode_mtime "$HOME/.config/opencode/config.json")" ]
for tool in claude codex opencode; do grep -Fxq "$tool : à jour" "$S/out"; done
echo 'OK   second run is a true no-op, including OpenCode inode and content'
jq 'map(if .name == "codegraph" then .enabled=false else . end)' "$CODEX_HOME/state.json" >"$S/disabled"
cp "$S/disabled" "$CODEX_HOME/state.json"
reset_calls; mcp
[ ! -s "$FAKE_WRITES" ]; cmp -s "$S/disabled" "$CODEX_HOME/state.json"
echo 'OK   F13: disabled Codex server with identical config stays disabled without writes'
cat >"$HOME/.config/mcp/servers.local.json" <<'JSON'
{"codegraph":{"command":"codegraph","args":["serve","--mcp","","two words","line\narg"],"env":{"DEMO_MODE":"two words"}}}
JSON
reset_calls; mcp
[ "$(wc -l <"$FAKE_WRITES")" -eq 4 ]; ! grep -q notebooklm "$FAKE_WRITES" || exit 1
grep -Fxq 'claude remove codegraph' "$FAKE_WRITES"; grep -Fxq 'codex remove codegraph' "$FAKE_WRITES"
jq -e '.[] | select(.name == "codegraph") | .transport.args == ["serve","--mcp","","two words","line\narg"] and .transport.env == {DEMO_MODE:"two words"}' "$CODEX_HOME/state.json" >/dev/null
reset_calls; mcp; [ ! -s "$FAKE_WRITES" ]
echo 'OK   local overrides replace whole entries; changed args update only that server'
printf '%s\n' '{"codegraph":null}' >"$HOME/.config/mcp/servers.local.json"
reset_calls; mcp
[ "$(wc -l <"$FAKE_WRITES")" -eq 2 ]; ! grep -q add "$FAKE_WRITES" || exit 1
jq -e '.mcp | has("codegraph") | not' "$HOME/.config/opencode/config.json" >/dev/null
reset_calls; mcp; [ ! -s "$FAKE_WRITES" ]
echo 'OK   explicit null removes only that name, once'
cat >"$HOME/.config/mcp/servers.local.json" <<'JSON'
{"codegraph":null,"acme":{"command":"fictional-mcp","args":["demo"],"secrets":["ACME_TOKEN"]},"remote":{"url":"https://example.com/mcp"}}
JSON
reset_calls; mcp
jq -e --arg dot "$HOME/.local/bin/dot" '.mcpServers.acme.command == $dot and .mcpServers.acme.args == ["secrets","run","ACME_TOKEN","--","fictional-mcp","demo"] and .mcpServers.remote == {type:"http",url:"https://example.com/mcp"}' "$HOME/.claude.json" >/dev/null
jq -e '.[] | select(.name == "remote") | .transport.type == "streamable_http" and .transport.url == "https://example.com/mcp"' "$CODEX_HOME/state.json" >/dev/null
jq -e '.mcp.remote == {type:"remote",url:"https://example.com/mcp",enabled:true}' "$HOME/.config/opencode/config.json" >/dev/null
[ ! -e "$FAKE_READ" ]
! grep -Fq -f "$S/value" "$FAKE_CALLS" "$S/out" "$S/err" "$HOME/.claude.json" "$CODEX_HOME/state.json" "$HOME/.config/opencode/config.json" || exit 1
reset_calls; mcp; [ ! -s "$FAKE_WRITES" ]
echo 'OK   secrets wrap the absolute dot path without resolving values; HTTP renders correctly'
foreign after
for tool in claude codex open; do cmp -s "$S/$tool-foreign.before" "$S/$tool-foreign.after"; done
cmp -s "$S/versioned.json" "$root/tests/fixtures/mcp/opencode.json"
cmp -s "$S/versioned.jsonc" "$root/tests/fixtures/mcp/opencode.jsonc"
echo 'OK   foreign servers with extra keys and versioned OpenCode files survive unchanged'
cp "$HOME/.config/mcp/servers.local.json" "$S/valid-local"
cp "$HOME/.config/opencode/config.json" "$S/open-before"
cp "$HOME/.claude.json" "$S/claude-before"
cp "$CODEX_HOME/state.json" "$S/codex-before"
for invalid in 'broken JSON' '[]' '{"bad":{"command":"x","url":"https://example.com"}}' '{"bad":{}}' '{"bad":{"command":"x","args":"bad"}}' '{"bad":{"command":"x","secrets":["UNKNOWN"]}}' '{"bad":{"command":"x","env":{"INVALID-NAME":"demo"}}}'; do
  printf '%s\n' "$invalid" >"$HOME/.config/mcp/servers.local.json"
  reset_calls
  if mcp; then exit 1; else [ "$?" -eq 1 ]; fi
  [ ! -s "$FAKE_CALLS" ]
  cmp -s "$S/open-before" "$HOME/.config/opencode/config.json"
  cmp -s "$S/claude-before" "$HOME/.claude.json"
  cmp -s "$S/codex-before" "$CODEX_HOME/state.json"
done
echo 'OK   invalid sources and unknown secrets fail before any CLI call or config write'
cp "$S/valid-local" "$HOME/.config/mcp/servers.local.json"
cp "$HOME/.config/mcp/servers.json" "$S/valid-shared"
printf '%s\n' '{"codegraph":{"command":"x","url":"https://example.com"}}' >"$HOME/.config/mcp/servers.json"
reset_calls
if mcp; then exit 1; else [ "$?" -eq 1 ]; fi
[ ! -s "$FAKE_CALLS" ]
cp "$S/valid-shared" "$HOME/.config/mcp/servers.json"
echo 'OK   invalid shared entry is rejected even when disabled locally'
mkdir -p "$S/minbin"
# no tool at all in PATH: dot needs neither jq nor bash
reset_calls
PATH=$S/minbin mcp
[ ! -s "$FAKE_CALLS" ]; [ ! -s "$S/out" ]
echo 'OK   missing CLIs are silently skipped'
# Multi-profile: without DOTFILES_DEPLOY, an unlinked servers.json is the merge of every profile's.
unset DOTFILES_DEPLOY
export HOME=$S/home2 CODEX_HOME=$S/home2/.codex XDG_CONFIG_HOME=$S/home2/.config
mkdir -p "$HOME/.dot/perso/home/.config/mcp" "$HOME/.dot/acme/home/.config/mcp" "$HOME/.config" "$CODEX_HOME"
printf '%s\n' '{"default":"perso","profiles":{"perso":{"repo":"https://example.com/p.git"},"acme":{"repo":"https://example.com/a.git"}}}' >"$HOME/.dot/profiles.json"
printf '%s\n' '{"alpha":{"command":"alpha-mcp","secrets":["PERSO_TOKEN"]},"shared":{"command":"old"}}' >"$HOME/.dot/perso/home/.config/mcp/servers.json"
printf '%s\n' 'PERSO_TOKEN=bw:PERSO' >"$HOME/.dot/perso/secrets.local"
printf '%s\n' '{"shared":{"command":"new"},"beta":{"command":"beta-mcp","secrets":["ACME_TOKEN"]}}' >"$HOME/.dot/acme/home/.config/mcp/servers.json"
printf '%s\n' 'ACME_TOKEN=bw:ACME' >"$HOME/.dot/acme/secrets.local"
printf '%s\n' '{}' >"$HOME/.claude.json"; printf '%s\n' '[]' >"$CODEX_HOME/state.json"
reset_calls; mcp -n
[ ! -s "$FAKE_WRITES" ]
for name in alpha beta shared; do grep -Fxq "claude : ajout $name" "$S/out"; done
reset_calls; mcp
[ "$(wc -l <"$FAKE_WRITES")" -eq 6 ]
jq -e --arg dot "$HOME/.local/bin/dot" '.mcpServers.shared.command == "new" and .mcpServers.alpha.args == ["-p","perso","secrets","run","PERSO_TOKEN","--","alpha-mcp"] and .mcpServers.beta.command == $dot and .mcpServers.beta.args == ["-p","acme","secrets","run","ACME_TOKEN","--","beta-mcp"]' "$HOME/.claude.json" >/dev/null
reset_calls; mcp; [ ! -s "$FAKE_WRITES" ]
echo 'OK   multi-profile: shared servers merge in registry order (last wins); a server using secrets runs `dot -p <its profile> secrets run`'
# A name declared only by the other profile is refused (it would not resolve at server start).
cp "$HOME/.dot/perso/home/.config/mcp/servers.json" "$S/perso-servers"
printf '%s\n' '{"alpha":{"command":"alpha-mcp","secrets":["ACME_TOKEN"]}}' >"$HOME/.dot/perso/home/.config/mcp/servers.json"
reset_calls
if mcp; then exit 1; else [ "$?" -eq 1 ]; fi
[ ! -s "$FAKE_WRITES" ]
cp "$S/perso-servers" "$HOME/.dot/perso/home/.config/mcp/servers.json"
echo 'OK   multi-profile: a secret name declared only by the other profile is refused before any write'
mkdir -p "$HOME/.config/mcp"
printf '%s\n' '{"only":{"command":"linked-mcp"}}' >"$HOME/.config/mcp/servers.json"
reset_calls; mcp -n
grep -Fxq 'claude : ajout only' "$S/out"; ! grep -q 'alpha' "$S/out" || exit 1
echo 'OK   a linked servers.json takes precedence over the profiles'
echo '== mcp OK'
