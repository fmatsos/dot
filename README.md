<p align="center">
  <img src=".github/assets/banner.webp" alt="dot: a ladybug with four arms holding a magnifying glass with a padlock and a golden key, in a twilight garden where glowing green lines link young shoots. Install and maintain your dotfiles profiles." width="100%">
</p>

<h1 align="center">dot</h1>

<p align="center">
  <strong>Install and maintain your dotfiles profiles.</strong>
</p>

<p align="center">
  <a href="https://github.com/fmatsos/dot/actions/workflows/ci.yml"><img src="https://github.com/fmatsos/dot/actions/workflows/ci.yml/badge.svg" alt="ci"></a>
  <img src="https://img.shields.io/badge/Linux-supported-success?logo=linux&logoColor=white" alt="Linux: supported">
  <img src="https://img.shields.io/badge/macOS-supported-success?logo=apple&logoColor=white" alt="macOS: supported">
  <img src="https://img.shields.io/badge/Go-static%20binary-00ADD8?logo=go&logoColor=white" alt="Go: static binary">
</p>

<p align="center">
  <a href="PLAN.md"><b>Plan</b></a> ·
  <a href="#commands"><b>Commands</b></a> ·
  <a href="#status"><b>Status</b></a> ·
  <a href="#development"><b>Development</b></a>
</p>

`dot` installs and maintains one or more dotfiles profiles. A profile is a data repository with
a `dot.json` manifest, cloned into `~/.dot/<key>`. `dot` brings the rest: the links in `~`, a
guard against leaks (forbidden terms, secrets), secrets through `bw` and `pass-cli`, and the
merge of MCP servers and settings for Claude Code, Codex and OpenCode.

## Highlights

- **Several profiles, one tool.** Each profile lives in its own clone (`~/.dot/<key>`) and is
  listed in a registry (`~/.dot/profiles.json`). Personal and work setups stay apart, and
  `pull`, `push`, `status` and `doctor` act on all of them at once.
- **Links, not copies.** `dot install` links the profile's files into `~`, and `dot uninstall`
  takes them out again. `-n` shows what would change without writing anything.
- **A guard against leaks.** `dot guard` checks the staged files, the commit message and the
  push for forbidden terms and secrets, from git hooks that are one line each.
- **Secrets stay in your vault.** `dot secrets` works with `bw` and `pass-cli` (`add`, `get`,
  `run`, `unlock`, `lock`, `status`), so a profile never has to hold them.
- **One config for your coding agents.** `dot settings` and `dot mcp` merge the settings and
  the MCP servers of a profile into Claude Code, Codex and OpenCode.
- **Extensible.** `dot <cmd>` runs `dot-<cmd>`, found in the profile's `bin/` and then on your
  `PATH`, and falls back to `git` on the clone.
- **A static binary.** Linux and macOS, x64 and arm64, no runtime to install.

## Commands

```text
dot install <url> [-p <key>] [-n]   clone a profile into ~/.dot/<key>, register it, install it
dot install [-n]                    reinstall every registered profile
dot pull                            update the profiles (git pull --rebase, then install)
dot push [message]                  commit the tracked files that changed in the profiles, then push
dot status                          changes in the clones and detached files (alias st)
dot uninstall -p <key> [--purge]    remove the links and the registry entry
dot doctor                          read-only report
dot config list|get|set|unset       read and edit the ~/.dot/profiles.json registry
dot whoami | profile | terms | clone | repos
dot secrets add|get|run|unlock|lock|status
dot guard staged|msg|push|all       leak guard for git hooks (forbidden terms, secrets)
dot settings [-n] | dot mcp [-n]   merge Claude settings and MCP servers
dot <cmd>                           run dot-<cmd> (profile bin/, then PATH), otherwise git on the clone
```

The target profile is chosen with `-p/--profile <key>`, then `DOT_PROFILE`, then the registry's
default profile. Without `-p`, `pull`, `push`, `status`, `doctor`, `settings` and `mcp` act on
every registered profile, and the other commands on the default profile. `DOTFILES_DEPLOY=<folder>`
points straight at a profile's folder (a transition aid and the entry point of the tests).
Without `-p`, the key of a `dot install <url>` is the repository name in the URL.

## Quick start

There is no release yet, so build it from source ([Go](https://go.dev/dl/) is the only
requirement):

```sh
git clone https://github.com/fmatsos/dot.git
cd dot
make build                                    # static binary ./dot
./dot install https://github.com/you/dotfiles.git -n   # dry run: shows what would be linked
./dot install https://github.com/you/dotfiles.git      # clone, register and link the profile
```

### Planned installation

A small bootstrap script will download the GitHub release pinned inside the script, check its
sha256 against the sum it contains, install it in `~/.local/bin/dot`, then run
`dot install <url of the first profile>`. Static binaries for Linux and macOS, x64 and arm64
(`dot-linux-x64`, `dot-linux-arm64`, `dot-macos-x64`, `dot-macos-arm64`), with a `SHA256SUMS`
file attached to each release.

## Status

**Phases 1 to 3 of the plan are implemented; phases 4 (switching the machine over) and 5 (work
profile) have not started, and there is no release yet.** The Go binary covers every command of
the bash CLI, plus multi-profile support, and passes the ported black-box tests (`tests/*.sh`)
and the Go tests.

What is left before a first `v0.1.0` release (the betterleaks scanner is pinned to 1.9.0, its
sha256 compiled into the binary; `DOT_BETTERLEAKS=<path>` replaces it, for tests only):

- [ ] **The `install.sh` bootstrap** (verified download of the release) is not written: it needs
  a published release whose sum is known.
- [ ] **Switching the machine** (phase 4): move `~/.config/dotfiles` to `~/.dot/perso` and
  remove the bash from the data repository. It touches the real machine, so it is rehearsed
  with a dry run first.
- [ ] The data repository's hooks become `exec dot guard staged`, `exec dot guard msg "$1"` and
  `exec dot guard push "$@"`.

The details of the decisions, the architecture and the phases are in [PLAN.md](PLAN.md).

## Development

Go, with [cobra](https://github.com/spf13/cobra) for the command line. `make test` runs
`go vet` and `go test -race`; `make build` produces the static binary `./dot`.

A command lives in its own file `cmd/dot/cmd_<name>.go` and registers itself with
`func init() { register(newXxxCmd) }`: no shared file is modified. The black-box tests are bash
scripts `tests/<name>.sh` that `source tests/lib.sh` and call `dot` (the binary in `$DOT_BIN`,
built if needed).

| Workflow | Runs on | What it does |
| --- | --- | --- |
| `ci.yml` | push, pull request | The guard (forbidden terms in files and commit metadata, secret scan), then `go vet`, `go test -race`, the static build and the black-box tests in `tests/*.sh`, as soon as a `go.mod` exists. |
| `release.yml` | tag `vX.Y.Z` | Builds the four static binaries, computes `SHA256SUMS` and publishes the release. |
| `dependabot.yml` | weekly | Updates the actions and the Go modules. |
