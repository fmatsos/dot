package main

import (
	"path/filepath"

	"github.com/fmatsos/dot/internal/mcp"
	"github.com/spf13/cobra"
)

func init() { register(newMcpCmd) }

func newMcpCmd(env *Env) *cobra.Command {
	var dry bool
	cmd := &cobra.Command{
		Use:   "mcp [-n]",
		Short: "Partage les serveurs MCP entre Claude Code, Codex et OpenCode",
		Long: "Applique la liste partagée ~/.config/mcp/servers.json et les changements locaux (-n : aperçu).\n\n" +
			"~/.config/mcp/servers.local.json (facultatif) : un serveur local remplace celui du même nom ;\n" +
			"null le retire. Les secrets restent des noms : dot secrets run les lit seulement au démarrage\n" +
			"du serveur. OpenCode lit le fichier généré ~/.config/opencode/config.json, fusionné avec ses\n" +
			"fichiers versionnés. Tant que servers.json n'est pas lié dans ~, la source est celle des profils\n" +
			"(tous, dans l'ordre du registre, sans -p).",
		Args: exactArgs(0, "dot mcp [-n]"),
		RunE: func(*cobra.Command, []string) error {
			dirs, multi, err := targetProfileDirs(env)
			if err != nil {
				return reportErr(env, "mcp", err)
			}
			// With several registered profiles servers.json is no longer linked, even when -p targets one.
			keys, _ := env.ProfileKeys()
			o := mcp.Options{Home: env.Home, DryRun: dry, FallbackOnApply: multi || len(keys) > 1, Out: env.Stdout}
			for _, d := range dirs {
				o.Fallback = append(o.Fallback, filepath.Join(d, "home", ".config", "mcp", "servers.json"))
				o.SecretsFiles = append(o.SecretsFiles, filepath.Join(d, "secrets.local"))
			}
			if err := mcp.Run(o); err != nil {
				return reportErr(env, "mcp", err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&dry, "dry-run", "n", false, "affiche le plan sans rien écrire ni appeler d'outil")
	return cmd
}
