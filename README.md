# dot

[![ci](https://github.com/fmatsos/dot/actions/workflows/ci.yml/badge.svg)](https://github.com/fmatsos/dot/actions/workflows/ci.yml)

`dot` installe et entretient un ou plusieurs profils de dotfiles. Un profil est un dépôt de
données avec un manifeste `dot.json`, cloné dans `~/.dot/<clé>`. `dot` apporte le reste : les
liens dans `~`, un garde-fou contre les fuites (termes interdits, secrets), les secrets via
`bw` et `pass-cli`, et la fusion des serveurs MCP et des réglages pour Claude Code, Codex et
OpenCode.

## État

**En conception : rien n'est utilisable pour l'instant.** Ce dépôt ne contient que le plan et
la CI ; aucun code n'est écrit. L'implémentation actuelle est un CLI bash qui vit dans un dépôt
de dotfiles ; cette réécriture en Go la remplacera, sans régression. Le détail des décisions, de
l'architecture et des phases est dans [PLAN.md](PLAN.md).

## Usage prévu

Rien de ce qui suit n'existe encore.

```text
dot install <url> [-p <clé>]      clone un profil dans ~/.dot/<clé>, l'inscrit, l'installe
dot install                       réinstalle tous les profils inscrits
dot pull                          met à jour les profils
dot doctor                        bilan en lecture seule
dot config list|get|set|unset     lit et modifie le registre ~/.dot/profiles.json
```

Le profil visé se choisit avec `-p/--profile <clé>`, puis `DOT_PROFILE`, puis le profil par
défaut du registre. Sans `-p`, `pull` et `doctor` agissent sur tous les profils, les autres
commandes sur le profil par défaut.

## Installation prévue

Un petit script d'amorce téléchargera la release GitHub figée dans le script, vérifiera son
sha256 contre la somme qu'il contient, l'installera dans `~/.local/bin/dot`, puis lancera
`dot install <url du premier profil>`. Binaires statiques pour Linux et macOS, en x64 et arm64
(`dot-linux-x64`, `dot-linux-arm64`, `dot-macos-x64`, `dot-macos-arm64`), avec un fichier
`SHA256SUMS` joint à chaque release.

## Développement

Go, avec [cobra](https://github.com/spf13/cobra) pour la ligne de commande. Aucun code n'est
écrit tant que la phase 0 du plan n'est pas close.

- `ci.yml` (push, pull request) : garde-fou (termes interdits dans les fichiers et les
  métadonnées de commit, scan de secrets), puis `go vet`, `go test -race` et le build statique
  dès qu'un `go.mod` existe.
- `release.yml` (tag `vX.Y.Z`) : construit les quatre binaires statiques, calcule `SHA256SUMS`
  et publie la release.
- `dependabot.yml` : mises à jour hebdomadaires des actions et des modules Go.
