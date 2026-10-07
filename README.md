<p align="center">
  <img src=".github/assets/banner.webp" alt="dot: a ladybug with four arms holding a magnifying glass with a padlock and a golden key, standing among young shoots in a twilight garden where glowing green lines link the shoots. Install and maintain your dotfiles profiles." width="100%">
</p>

<h1 align="center">dot</h1>

<p align="center">
  <strong>One tool for all your dotfiles, without leaking a thing.</strong>
</p>

<p align="center">
  <a href="https://github.com/fmatsos/dot/actions/workflows/ci.yml"><img src="https://github.com/fmatsos/dot/actions/workflows/ci.yml/badge.svg" alt="ci"></a>
  <img src="https://img.shields.io/badge/Linux-supported-success?logo=linux&logoColor=white" alt="Linux: supported">
  <img src="https://img.shields.io/badge/macOS-supported-success?logo=apple&logoColor=white" alt="macOS: supported">
  <img src="https://img.shields.io/badge/Go-static%20binary-00ADD8?logo=go&logoColor=white" alt="Go: static binary">
</p>

<p align="center">
  <a href="#why-dot"><b>Why dot?</b></a> ·
  <a href="#quick-start"><b>Quick start</b></a> ·
  <a href="#a-profile"><b>A profile</b></a> ·
  <a href="#commands"><b>Commands</b></a> ·
  <a href="#development"><b>Development</b></a>
</p>

`dot` installs and maintains one or more dotfiles profiles, from a single static binary for Linux
and macOS. A profile is a data repository with a `dot.json` manifest, cloned into
`~/.dot/<key>`. `dot` brings the rest: the links in `~`, a guard against leaks (forbidden terms,
secrets), secrets read from your vault (`bw` and `pass-cli`), and the merge of MCP servers and
settings for Claude Code, Codex and OpenCode.

Several profiles can live side by side on one machine, a personal one and a work one for
instance, each with its own git identity, and `dot` treats them all the same way.

## Why dot?

**Dotfiles** are the configuration files that live in your home directory: `~/.zshrc`,
`~/.gitconfig`, `~/.config/…`, `~/.claude/settings.json`. They make a machine yours, so people
keep them in a git repository to rebuild a machine, or to share one setup between several.

A plain repository is a good start, and for one machine, one identity and nothing secret near your
config, it is all you need. The questions come later, and each one is usually answered with a
script of your own. `dot` is that layer, written once and tested:

### Put the files in place, without breaking anything

A profile keeps its files under `home/` (linked into `~`) and `bin/` (linked into
`~/.local/bin`):

```text
dotfiles/                      ~ after `dot install`
├── dot.json
├── home/
│   ├── .zshrc            →    ~/.zshrc       (a symbolic link to the file in ~/.dot/dotfiles)
│   └── .gitconfig        →    ~/.gitconfig
└── bin/
    └── backup            →    ~/.local/bin/backup
```

Editing `~/.zshrc` edits the file in the repository, so `dot push` has something to commit. A file
that was already there is moved to `~/.local/state/dotfiles/backup/<date>/` (kept 30 days),
running `dot install` twice changes nothing, and `dot install -n` shows what would happen first.

### Keep a personal and a work setup on the same machine

```sh
dot install https://github.com/you/dotfiles-perso.git
dot install https://github.com/you/dotfiles-work.git -p work

dot profile work ~/src/github.com/acme/    # applies the manifest's `work` git identity to this folder
dot whoami                                 # which git identity applies here, and the file that sets it
dot repos --problems                       # repositories with the wrong identity, or a bypassed guard
```

Both profiles are updated together with `dot pull`. If they both try to link the same file
(say `~/.zshrc`), `dot install` refuses before it writes anything, instead of silently letting
one win.

### Do not leak your employer, or a token, into a public repository

Dotfiles repositories are often public, and they are easy to pollute: a token pasted in a
snippet, an internal hostname in a comment. Git hooks that are one line each run `dot guard` on
every commit and push:

```text
# ~/.dot/work/forbidden.local     (one case-insensitive regular expression per line, never committed)
acme
internal\.example\.com
```

A commit that stages a secret, or a file that matches one of those terms, is refused. The guard
reports file names, branches and commits, never the text it found, and it fails closed: a missing
list or an unavailable scanner blocks instead of letting everything through. `dot terms` copies
the list to a CI secret, so the same check runs on the server.

### Keep secrets out of your config

Instead of `export GITHUB_TOKEN=…` in `.zshrc`, you keep a reference to your vault
(Bitwarden or Proton Pass), and the value is read only when a command needs it:

```sh
dot secrets add GITHUB_TOKEN bw:github-token     # or pass:<vault>/<item>
dot secrets run GITHUB_TOKEN -- gh api user      # the token exists for this one command only
```

The names live in a local file, `secrets.local` (mode 600), in the profile folder. A value is
never passed as a process argument and never sits in your shell.

### Configure your coding agents once

Claude Code, Codex and OpenCode each want their own MCP servers and settings. `dot` keeps one
shared list and applies it to all three:

```sh
dot mcp -n          # preview the plan
dot mcp             # ~/.config/mcp/servers.json → Claude Code, Codex and OpenCode
dot settings -n     # preview the diff of ~/.claude/settings.json
dot settings        # merge home/.claude/settings.base.json into it
```

For settings, the versioned base wins and the extra local keys are kept, and the old file is
backed up. For MCP, a local `servers.local.json` can replace a server by name or remove it, and
the secrets of a server stay names that `dot secrets run` resolves when the server starts.

### Notice when something drifts, and rebuild a machine

```sh
dot status          # clones: changes, ahead / behind; and files that are no longer links
dot doctor          # read-only report of every profile
```

Some applications rewrite a config file instead of editing it through the link. That file is then
"detached": its changes would never reach the repository. `dot status` lists it; once you have
carried its content back into the profile, `dot install` puts the link back.

On a new machine, `dot install <url>` brings the profiles back, and `dot repos export` /
`dot repos clone` bring back the repositories you work on, under `~/src/<host>/<owner>/<repo>`.

## Highlights

- **Several profiles, one tool.** Each profile lives in its own clone (`~/.dot/<key>`) and is
  listed in a registry (`~/.dot/profiles.json`). `pull`, `push`, `status` and `doctor` act on all
  of them at once, or on a single one with `-p`.
- **Links, not copies.** `dot install` links the profile's files into `~`, and `dot uninstall`
  takes them out again. `-n` shows what would change without writing anything.
- **A guard against leaks.** `dot guard` checks the staged files, the commit message and the
  push for forbidden terms and secrets, from git hooks that are one line each. The secret
  scanner (betterleaks) is pinned to one version and its sha256 is compiled into the binary.
- **Secrets stay in your vault.** `dot secrets` works with `bw` and `pass-cli` (`add`, `get`,
  `run`, `unlock`, `lock`, `status`), so a profile never has to hold them.
- **One config for your coding agents.** `dot settings` and `dot mcp` merge the settings and the
  MCP servers of a profile into Claude Code, Codex and OpenCode.
- **Strict manifests.** `dot.json` is validated and an unknown key is refused, so a typo is an
  error and not a silent no-op.
- **Extensible.** `dot <cmd>` runs `dot-<cmd>`, found in the profile's `bin/` and then on your
  `PATH`, and falls back to `git` on the clone.
- **A health check.** `dot doctor` reports the state of every profile without changing anything.

## Quick start

There is no release to download yet, so build it from source. [Go](https://go.dev/dl/) is the
only requirement:

```sh
git clone https://github.com/fmatsos/dot.git
cd dot
make build                                              # static binary ./dot
./dot install https://github.com/you/dotfiles.git -n    # dry run: shows what would be linked
./dot install https://github.com/you/dotfiles.git       # clone, register and link the profile
./dot doctor                                            # read-only report
```

Then keep it up to date:

```sh
dot pull          # git pull --rebase on every profile, then install
dot push "tidy"   # commit the tracked files that changed, then push
dot status        # changes in the clones and detached files
```

Release binaries (`dot-linux-x64`, `dot-linux-arm64`, `dot-macos-x64`, `dot-macos-arm64`, with a
`SHA256SUMS` file) are built and published by the `release.yml` workflow when a `vX.Y.Z` tag is
pushed.

## A profile

A profile is a repository with a `dot.json` at its root. The manifest says which folders to
deploy, which git identities the profile has, and which optional modules to enable:

```json
{
  "repo": "https://github.com/you/dotfiles.git",
  "deploy": { "sparse": ["home", "bin", ".githooks"] },
  "profiles": { "perso": "home/.config/git/profiles/perso.gitconfig" },
  "deployProfile": "perso",
  "marketplace": { "name": "acme", "plugins": ["acme"] },
  "nvm": { "node": "22.11.0", "packages": ["@scope/tool@1.2.3"] },
  "modules": ["settings", "mcp"]
}
```

| Key | Meaning |
| --- | --- |
| `repo` | URL of the profile's repository. |
| `deploy.sparse` | The top-level folders to check out and link. |
| `profiles` | Named git identities, each one a `.gitconfig` under `home/`. |
| `deployProfile` | The identity to deploy on this machine. It must be one of `profiles`. |
| `marketplace` | Optional: a plugin marketplace and its plugins. |
| `nvm` | Optional: a pinned Node version (`x.y.z`) and the packages to install with it. |
| `modules` | Optional: `settings` and/or `mcp`, to merge agent settings and MCP servers. |

The git hooks of the data repository stay one line each:

```sh
exec dot guard staged        # pre-commit
exec dot guard msg "$1"      # commit-msg
exec dot guard push "$@"     # pre-push
```

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

### Choosing a profile

The target profile is chosen with `-p/--profile <key>`, then `DOT_PROFILE`, then the registry's
default profile. Without `-p`, `pull`, `push`, `status`, `doctor`, `settings` and `mcp` act on
every registered profile, and the other commands on the default profile. Without `-p`, the key
of a `dot install <url>` is the repository name in the URL.

| Variable | Effect |
| --- | --- |
| `DOT_PROFILE` | The profile to target when `-p` is not given. |
| `DOTFILES_DEPLOY` | The folder of a profile, used directly (a transition aid and the entry point of the tests). |
| `DOT_BETTERLEAKS` | Path of a scanner binary that replaces the pinned betterleaks, for tests only. |

### Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Success. |
| `1` | The command failed. |
| `2` | Wrong arguments or flags. |
| `3` | The secrets vault is locked or logged out (`dot secrets`). |

`dot secrets run` exits with the code of the command it wraps.

## Development

Go, with [cobra](https://github.com/spf13/cobra) for the command line. `make test` runs
`go vet` and `go test -race`; `make build` produces the static binary `./dot`.

A command lives in its own file `cmd/dot/cmd_<name>.go` and registers itself with
`func init() { register(newXxxCmd) }`: no shared file is modified. The black-box tests are bash
scripts `tests/<name>.sh` that `source tests/lib.sh` and call `dot` (the binary in `$DOT_BIN`,
built if needed).

| Workflow | Runs on | What it does |
| --- | --- | --- |
| `ci.yml` | push, pull request | The guard (forbidden terms in files and commit metadata, secret scan), then `go vet`, `go test -race`, the static build and the black-box tests in `tests/*.sh`. |
| `release.yml` | tag `vX.Y.Z` | Builds the four static binaries, computes `SHA256SUMS` and publishes the release. |
| `dependabot.yml` | weekly | Updates the actions and the Go modules. |
