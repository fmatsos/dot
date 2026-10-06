# dot

[![ci](https://github.com/fmatsos/dot/actions/workflows/ci.yml/badge.svg)](https://github.com/fmatsos/dot/actions/workflows/ci.yml)

`dot` installe et entretient un ou plusieurs profils de dotfiles. Un profil est un dépôt de
données avec un manifeste `dot.json`, cloné dans `~/.dot/<clé>`. `dot` apporte le reste : les
liens dans `~`, un garde-fou contre les fuites (termes interdits, secrets), les secrets via
`bw` et `pass-cli`, et la fusion des serveurs MCP et des réglages pour Claude Code, Codex et
OpenCode.

## État

**Phases 1 à 3 du plan implémentées, ainsi que la chaîne de release signée ; phases 4 (bascule de
la machine) et 5 (profil travail) non commencées, et pas encore de release.** Le binaire Go couvre
toutes les commandes du CLI bash, plus le multi-profil, et passe les tests boîte noire
(`tests/*.sh`) et les tests Go, sur Linux et macOS.

Reste avant une première release `v0.1.0` (le scanner betterleaks est épinglé en 1.9.0, sha256 compilé
dans le binaire ; `DOT_BETTERLEAKS=<chemin>` le remplace, pour les tests uniquement) :

- **La clé de signature des releases** n'existe pas encore : `scripts/gen-release-key.sh` la
  génère, `internal/selfupdate/release.pub` (vide pour l'instant) reçoit la clé publique, et la
  clé privée devient le secret `RELEASE_SIGNING_KEY`. D'ici là, le workflow de release échoue et
  `dot self-update` refuse, volontairement.
- **L'amorce `install.sh`** est écrite mais pas figée : après la première release,
  `scripts/pin-install.sh vX.Y.Z` y inscrit la version et les sommes (signature vérifiée d'abord).
- **Bascule de la machine** (phase 4) : déplacer `~/.config/dotfiles` vers `~/.dot/perso`, retirer
  le bash du dépôt de données. Elle touche la machine réelle et se fait à blanc d'abord ;
  `dot backups restore` permet de revenir sur les fichiers remplacés par des liens.
- Les hooks du dépôt de données deviennent `exec dot guard staged`, `exec dot guard msg "$1"` et
  `exec dot guard push "$@"`.

Le détail des décisions, de l'architecture et des phases est dans [PLAN.md](PLAN.md).

## Commandes

```text
dot install <url> [-p <clé>] [-n]   clone un profil dans ~/.dot/<clé>, l'inscrit, l'installe
dot install [-n]                    réinstalle tous les profils inscrits
dot pull                            met à jour les profils (git pull --rebase, puis installation)
dot push [message]                  commite les fichiers suivis modifiés des profils, puis pousse
dot status                          changements des clones et fichiers détachés (alias st)
dot uninstall -p <clé> [--purge]    retire les liens et l'entrée du registre
dot adopt [-p <clé>] [-n] <fichier>  range un fichier de ~ dans home/ du profil (garde-fou d'abord)
dot backups list|restore [-n]       fichiers de ~ sauvegardés par install, et leur restauration
dot self-update [--version v] [-n]  met dot à jour depuis une release signée
dot doctor                          bilan en lecture seule
dot config list|get|set|unset       lit et modifie le registre ~/.dot/profiles.json
dot whoami | profile | terms | clone | repos
dot secrets add|get|run|unlock|lock|status
dot guard staged|msg|push|all       garde-fou des hooks git (termes interdits, secrets)
dot settings [-n] | dot mcp [-n]   fusion des réglages Claude et des serveurs MCP
dot <cmd>                           lance dot-<cmd> (bin/ des profils, puis PATH), sinon git sur le clone
```

Sans `-p` et avec plusieurs profils inscrits, `dot <cmd>` cherche `bin/dot-<cmd>` dans tous les
profils : un nom revendiqué par deux profils est refusé (`précisez -p <clé>`). La complétion
propose les clés du registre pour `-p` et les clés de `dot config get|unset`.

Le profil visé se choisit avec `-p/--profile <clé>`, puis `DOT_PROFILE`, puis le profil par
défaut du registre. Sans `-p`, `pull`, `push`, `status`, `doctor`, `settings` et `mcp` agissent sur tous les
profils inscrits, les autres commandes sur le profil par défaut. `DOTFILES_DEPLOY=<dossier>` désigne
directement le dossier d'un profil (compatibilité de transition et point d'entrée des tests).
La clé d'un `dot install <url>` sans `-p` est le nom du dépôt de l'URL.

## Variantes par système et par machine

À côté de `home/`, un profil peut porter `home@darwin/`, `home@linux/` et `home@<machine>/` (nom
court de la machine, en minuscules). Pour un même chemin de `~`, `home@<machine>` l'emporte sur
`home@<os>`, qui l'emporte sur `home/` ; les variantes d'un autre système ou d'une autre machine
sont ignorées. Il n'y a pas de moteur de modèles : les fichiers de `~` restent des liens vers le
clone. Les conflits entre profils se calculent après cette résolution, `dot install -n` affiche la
couche retenue quand ce n'est pas `home`, et `dot adopt --os|--host` range un fichier dans la
variante. Un `deploy.sparse` doit citer chaque variante voulue (`home@darwin`…).
`DOT_HOSTNAME` remplace le nom de la machine, pour les tests uniquement.

Limite actuelle : `dot settings`, `dot mcp` et la configuration mise lisent encore `home/` seul.

## Installation

L'amorce `install.sh` télécharge la release figée dans le script, vérifie son sha256 contre la
somme qu'il contient, l'installe dans `~/.local/bin/dot`, puis lance
`dot install <url du premier profil>` si une URL est donnée. Binaires statiques pour Linux et
macOS, en x64 et arm64 (`dot-linux-x64`, `dot-linux-arm64`, `dot-macos-x64`, `dot-macos-arm64`),
avec `SHA256SUMS` et sa signature Ed25519 `SHA256SUMS.sig` joints à chaque release.

Ensuite, `dot self-update` vérifie la signature de `SHA256SUMS` avec la clé publique compilée
dans le binaire, puis la somme du binaire, avant de remplacer l'exécutable de façon atomique.
La somme seule ne suffirait pas : publiée avec la release, elle ne protège pas d'une release
compromise. La vérification n'utilise que `crypto/ed25519` ; la signature se fait en CI avec
OpenSSL 3 (`openssl pkeyutl -sign -rawin`).

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
- `ci.yml` fait tourner le job Go sur Linux et macOS (bash 4+ de Homebrew pour les tests boîte noire).
- `release.yml` (tag `vX.Y.Z`) : construit les quatre binaires statiques, calcule `SHA256SUMS`,
  le signe avec le secret `RELEASE_SIGNING_KEY`, vérifie la signature contre la clé publique
  versionnée, et publie la release.
- `dependabot.yml` : mises à jour hebdomadaires des actions et des modules Go.
