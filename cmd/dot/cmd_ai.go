package main

import "github.com/spf13/cobra"

func init() { register(newAiCmd, newMovedCmd("settings"), newMovedCmd("mcp")) }

// aiCommands are the subcommands of dot ai, each from its own cmd_<name>.go file.
var aiCommands []func(*Env) *cobra.Command

func newAiCmd(env *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ai",
		Short: "Configure the coding agents: settings, MCP servers, skills",
		Long:  "Apply what the profiles version for Claude Code, Codex, OpenCode and the other coding agents.",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	for _, f := range aiCommands {
		cmd.AddCommand(f(env))
	}
	return cmd
}

// newMovedCmd keeps a former top-level command as a hidden pointer to dot ai <name>:
// without it, `dot <name>` would fall through to git on the profile clone.
func newMovedCmd(name string) func(*Env) *cobra.Command {
	return func(*Env) *cobra.Command {
		return &cobra.Command{
			Use:                name,
			Hidden:             true,
			DisableFlagParsing: true,
			RunE: func(*cobra.Command, []string) error {
				return usagef("dot %s moved to dot ai %s", name, name)
			},
		}
	}
}
