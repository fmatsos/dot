# dot

[![ci](https://github.com/fmatsos/dot/actions/workflows/ci.yml/badge.svg)](https://github.com/fmatsos/dot/actions/workflows/ci.yml)

`dot` installs and maintains one or more dotfiles profiles. A profile is a data repository with a
`dot.json` manifest, cloned into `~/.dot/<key>`. `dot` brings the rest: links in `~`, a leak guard
(forbidden terms, secrets), secrets fetched on demand from `bw` and `pass-cli`, and merged MCP
servers and settings for Claude Code, Codex and OpenCode.

`dot` is a single static Go binary for Linux and macOS. It holds no personal value: everything
specific to a user lives in their profile repositories. Its messages and help are in French.

## Status

**Plan phases 1 to 3 are implemented, along with the signed release chain. Phase 4 (switching
the real machine over) and phase 5 (work profile) have not started, and there is no release
yet.** The binary covers every command of the former bash CLI, plus multi-profile support, and
passes the black-box tests (`tests/*.sh`) and the Go tests on Linux and macOS.

Left before a first `v0.1.0` release:

- **Release signing key.** It does not exist yet. `scripts/gen-release-key.sh` generates it,
  `internal/selfupdate/release.pub` (empty for now) receives the public key, and the private key
  becomes the `RELEASE_SIGNING_KEY` repository secret (OpenSSL 3 required; macOS's stock
  LibreSSL cannot sign raw Ed25519). Until then the release workflow fails and
  `dot self-update` refuses, on purpose.
- **Bootstrap `install.sh`.** It is written but not pinned: after the first release,
  `scripts/pin-install.sh vX.Y.Z` writes the version and checksums into it, after verifying
  the signature.
- **Machine switch (phase 4).** Move `~/.config/dotfiles` to `~/.dot/perso` and remove the bash
  CLI from the data repository. It touches the real machine and is dry-run first;
  `dot backups restore` brings back files that were replaced by links.
- The data repository's hooks become `exec dot guard staged`, `exec dot guard msg "$1"` and
  `exec dot guard push "$@"`.

Decisions, architecture and phases are detailed in [PLAN.md](PLAN.md) (French).

## Commands

```text
dot install <url> [-p <key>] [-n]       clone a profile into ~/.dot/<key>, register it, install it
dot install [-n]                        reinstall every registered profile
dot pull                                update profiles (git pull --rebase, then install)
dot push [message]                      commit the profiles' modified tracked files, then push
dot status                              clone changes and detached files (alias st)
dot uninstall -p <key> [--purge]        remove the profile's links and its registry entry
dot adopt [-p <key>] [-n] [--os|--host] <file>...
                                        move a file of ~ into the profile, guard checks first
dot backups list|restore [-n]           files of ~ backed up by install, and their restoration
dot self-update [--version vX.Y.Z] [-n] update dot from a signed release
dot doctor                              read-only health check
dot config list|get|set|unset           read and edit the registry ~/.dot/profiles.json
dot whoami | profile | terms | clone | repos
dot secrets add|get|run|unlock|lock|status
dot guard staged|msg|push|all           git hook guard (forbidden terms, secrets)
dot settings [-n] | dot mcp [-n]        merge Claude settings and MCP servers
dot <cmd>                               run dot-<cmd> (profiles' bin/, then PATH), else git on the clone
```

### Targeting a profile

- The profile is chosen with `-p/--profile <key>`, then `DOT_PROFILE`, then the registry's
  default profile.
- Without `-p`, `pull`, `push`, `status`, `doctor`, `settings` and `mcp` act on every
  registered profile, with a `==> <key>` header for each; a failure does not stop the others.
  The other commands act on the default profile.
- `DOTFILES_DEPLOY=<dir>` points directly at a profile directory (transition compatibility and
  test entry point).
- Without `-p`, the key of `dot install <url>` is the repository name from the URL.
- Shell completion offers the registry keys for `-p` and the keys of `dot config get|unset`.

### Extensions

`dot <cmd>` runs an executable `dot-<cmd>` found in a profile's `bin/`, then in the `PATH`, and
otherwise runs `git <cmd>` on the profile clone. Without `-p` and with several profiles
registered, every profile's `bin/` is searched: a name claimed by two profiles is refused, and
you must pass `-p <key>`.

### Adopting a file

`dot adopt ~/.foo` moves an existing file of `~` into the target profile's `home/` and replaces
it with a link. Before anything moves, the file name and content go through the target
profile's forbidden-terms list and the secret scanner. A hit, a missing or invalid term list or
an unavailable scanner refuses the file, and only its name is reported. Symlinks, files outside
`~`, paths already provided by another profile and a sparse checkout without `home` are refused
too. Nothing is committed: `dot push` does that.

### Backups

When `dot install` replaces an existing file of `~` with a link, the file is moved to
`~/.local/state/dotfiles/backup/<timestamp>/`. `dot backups list` shows the runs, and
`dot backups restore [<run>] [<path>...]` moves files back. A file is restored only when its
place in `~` is empty or holds a link managed by dot; a real file is never overwritten.
`dot install` recreates the links, so uninstall the profile or remove the file from it for a
restore to stick.

## Per-OS and per-host variants

Besides `home/`, a profile may carry `home@darwin/`, `home@linux/` and `home@<host>/`, where
`<host>` is the machine's short hostname in lower case. For a given path of `~`,
`home@<host>` wins over `home@<os>`, which wins over `home/`. Variants for another system or
another machine are ignored.

- There is no templating engine: files in `~` stay links into the clone.
- Conflicts between profiles are computed after this resolution.
- `dot install -n` shows the winning layer when it is not `home`.
- `dot adopt --os|--host` moves a file into the matching variant.
- A `deploy.sparse` list must name each wanted variant (`home@darwin`...).
- `DOT_HOSTNAME` replaces the hostname, for tests only.

Current limitation: `dot settings`, `dot mcp` and the mise configuration still read `home/`
only.

## Leak guard

`dot guard staged | msg <file> | push | all` blocks commits and pushes that contain a forbidden
term (case-insensitive RE2 patterns from the profile's `forbidden.local`) or a secret found by
betterleaks.

- It fails closed: a missing, empty or invalid list, a failed git command or an unavailable
  scanner blocks.
- It reports file names or commits, never the matched text.
- A term that matches the empty string (`a*`, `^`) is refused, since it would match everything.
- betterleaks is pinned to 1.9.0 and downloaded into `~/.cache/dot/`, its sha256 compiled into
  the binary. `DOT_BETTERLEAKS=<path>` replaces it, for tests only.
- Known blind spot: there is no Unicode normalization, so a term written in NFC does not match a
  file name in NFD, which macOS can produce without `core.precomposeunicode`.

## Installation

The `install.sh` bootstrap downloads the release pinned in the script, checks its sha256
against the checksum it contains, installs it into `~/.local/bin/dot`, then runs
`dot install <first profile url>` if a URL is given. Releases ship static binaries for Linux and
macOS on x64 and arm64 (`dot-linux-x64`, `dot-linux-arm64`, `dot-macos-x64`,
`dot-macos-arm64`), with `SHA256SUMS` and its Ed25519 signature `SHA256SUMS.sig`.

After that, `dot self-update` verifies the signature of `SHA256SUMS` with the public key compiled
into the binary, then the binary's checksum, before replacing the executable atomically. A
checksum alone would not be enough: published with the release, it does not protect against a
compromised release. Verification only uses Go's `crypto/ed25519`; signing happens in CI with
OpenSSL 3 (`openssl pkeyutl -sign -rawin`).

## Development

Go, with [cobra](https://github.com/spf13/cobra) for the command line. `make test` runs
`go vet` and `go test -race`; `make build` produces the static `./dot` binary.

A command lives in its own file `cmd/dot/cmd_<name>.go` and registers itself with
`func init() { register(newXxxCmd) }`, so adding one touches no shared file. Black-box tests are
bash scripts `tests/<name>.sh` that `source tests/lib.sh` and call `dot` (the `$DOT_BIN`
binary, built when needed). They must run with GNU and BSD tools alike: `tests/lib.sh`
provides portable helpers (`stat_inode_mtime`, `in_pty`).

- `ci.yml` (push, pull request) runs the guard first: forbidden terms in files and commit
  metadata, then a secret scan. It then runs `go vet`, `go test -race`, the static build and
  the black-box tests, on Linux and macOS (Homebrew's bash 4+ on macOS).
- `release.yml` (tag `vX.Y.Z`) builds the four static binaries, computes `SHA256SUMS`, signs it
  with the `RELEASE_SIGNING_KEY` secret, checks the signature against the committed public key,
  and publishes the release.
- `dependabot.yml` updates the actions and Go modules weekly.
