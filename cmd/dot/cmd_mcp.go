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
		Short: "Share the MCP servers between Claude Code, Codex and OpenCode",
		Long: "Apply the shared list ~/.config/mcp/servers.json and the local changes (-n: preview).\n\n" +
			"~/.config/mcp/servers.local.json (optional): a local server replaces the one with the same name;\n" +
			"null removes it. Secrets stay names: dot secrets run reads them only when the server\n" +
			"starts. OpenCode reads the generated file ~/.config/opencode/config.json, merged with its\n" +
			"versioned files. As long as servers.json is not linked into ~, the source is the profiles'\n" +
			"(all of them, in registry order, without -p).",
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
				o.Profiles = append(o.Profiles, mcp.Profile{
					Key:     mcp.ProfileKey(env.Home, d),
					Servers: filepath.Join(d, "home", ".config", "mcp", "servers.json"),
					Secrets: filepath.Join(d, "secrets.local"),
				})
			}
			if err := mcp.Run(o); err != nil {
				return reportErr(env, "mcp", err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&dry, "dry-run", "n", false, "show the plan without writing anything or calling a tool")
	return cmd
}
