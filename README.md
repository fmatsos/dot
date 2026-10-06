# dot

[![ci](https://github.com/fmatsos/dot/actions/workflows/ci.yml/badge.svg)](https://github.com/fmatsos/dot/actions/workflows/ci.yml)

`dot` installe et entretient un ou plusieurs profils de dotfiles. Un profil est un dépôt de
données avec un manifeste `dot.json`, cloné dans `~/.dot/<clé>`. `dot` apporte le reste : les
liens dans `~`, un garde-fou contre les fuites (termes interdits, secrets), les secrets via
`bw` et `pass-cli`, et la fusion des serveurs MCP et des réglages pour Claude Code, Codex et
OpenCode.

## État

**Phases 1 à 3 du plan implémentées ; phases 4 (bascule de la machine) et 5 (profil travail)
non commencées, et pas encore de release.** Le binaire Go couvre toutes les commandes du CLI bash,
plus le multi-profil, et passe les tests boîte noire portés (`tests/*.sh`) et les tests Go.

Reste avant une première release `v0.1.0` :

- **Sommes betterleaks à figer** : la table de `internal/guard/betterleaks.go` a des empreintes
  vides, donc `dot guard` échoue fermé (voir `scripts/pin-betterleaks.sh`). Tant qu'elles sont
  vides, `DOT_BETTERLEAKS=<chemin>` permet de pointer un scanner existant.
- **L'amorce `install.sh`** (téléchargement vérifié de la release) n'est pas écrite : elle
  suppose une release publiée dont on connaît la somme.
- **Bascule de la machine** (phase 4) : déplacer `~/.config/dotfiles` vers `~/.dot/perso`, retirer
  le bash du dépôt de données. Elle touche la machine réelle et se fait à blanc d'abord.
- Les hooks du dépôt de données deviennent `exec dot guard staged`, `exec dot guard msg "$1"` et
  `exec dot guard push "$@"`.

Le détail des décisions, de l'architecture et des phases est dans [PLAN.md](PLAN.md).

## Commandes

```text
dot install <url> [-p <clé>] [-n]   clone un profil dans ~/.dot/<clé>, l'inscrit, l'installe
dot install [-n]                    réinstalle tous les profils inscrits
dot pull                            met à jour les profils (git pull --rebase, puis installation)
dot uninstall -p <clé> [--purge]    retire les liens et l'entrée du registre
dot doctor                          bilan en lecture seule
dot config list|get|set|unset       lit et modifie le registre ~/.dot/profiles.json
dot st | send | whoami | profile | terms | clone | repos
dot secrets add|get|run|unlock|lock|status
dot guard staged|msg|push|all       garde-fou des hooks git (termes interdits, secrets)
dot settings [-n] | dot mcp [-n]   fusion des réglages Claude et des serveurs MCP
dot <cmd>                           lance dot-<cmd> (bin/ du profil, puis PATH), sinon git sur le clone
```

Le profil visé se choisit avec `-p/--profile <clé>`, puis `DOT_PROFILE`, puis le profil par
défaut du registre. Sans `-p`, `pull`, `doctor`, `settings` et `mcp` agissent sur tous les
profils, les autres commandes sur le profil par défaut. `DOTFILES_DEPLOY=<dossier>` désigne
directement le dossier d'un profil (compatibilité de transition et point d'entrée des tests).
La clé d'un `dot install <url>` sans `-p` est le nom du dépôt de l'URL.

## Installation prévue

Un petit script d'amorce téléchargera la release GitHub figée dans le script, vérifiera son
sha256 contre la somme qu'il contient, l'installera dans `~/.local/bin/dot`, puis lancera
`dot install <url du premier profil>`. Binaires statiques pour Linux et macOS, en x64 et arm64
(`dot-linux-x64`, `dot-linux-arm64`, `dot-macos-x64`, `dot-macos-arm64`), avec un fichier
`SHA256SUMS` joint à chaque release.

## Développement

Go, avec [cobra](https://github.com/spf13/cobra) pour la ligne de commande. `make test` lance
`go vet` et `go test -race` ; `make build` produit le binaire statique `./dot`.

Une commande vit dans son propre fichier `cmd/dot/cmd_<nom>.go` et s'enregistre avec
`func init() { register(newXxxCmd) }` : aucun fichier partagé n'est modifié. Les tests boîte
noire sont des scripts bash `tests/<nom>.sh` qui font `source tests/lib.sh` et appellent
`dot` (le binaire de `$DOT_BIN`, construit au besoin).

- `ci.yml` (push, pull request) : garde-fou (termes interdits dans les fichiers et les
  métadonnées de commit, scan de secrets), puis `go vet`, `go test -race`, le build statique et
  les tests boîte noire de `tests/*.sh`, dès qu'un `go.mod` existe.
- `release.yml` (tag `vX.Y.Z`) : construit les quatre binaires statiques, calcule `SHA256SUMS`
  et publie la release.
- `dependabot.yml` : mises à jour hebdomadaires des actions et des modules Go.
