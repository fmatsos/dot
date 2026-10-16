package main

import (
	"path/filepath"

	"github.com/fmatsos/dot/internal/doctor"
	"github.com/spf13/cobra"
)

func init() { register(newDoctorCmd) }

func newDoctorCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Local health check of the dotfiles, without writes or fetch",
		Long: "dot doctor — local health check of the dotfiles, without writes, fetch or interactive input.\n\n" +
			"Checks deployment, links, guard, tools, secrets, plugins and git identities:\n" +
			"✓ healthy, ! warning with a hint to fix it, ✗ failure (exit code 1).\n" +
			"Git ahead and behind counts come from the last fetch. No secret value is shown.\n" +
			"Without -p, every registered profile is checked in registry order, each under\n" +
			"a \"profile <key>\" line; the machine checks run only once.",
		DisableFlagsInUseLine: true,
		Args:                  exactArgs(0, "dot doctor"),
		RunE: func(*cobra.Command, []string) error {
			dirs, multi, err := targetProfileDirs(env)
			o := doctor.Options{
				Home: env.Home, Err: err, Headers: multi,
				SrcRoot: srcRoot(env), SandboxRoot: sandboxRoot(env),
			}
			for _, d := range dirs {
				if err != nil {
					break // an unresolved target leaves nothing to check but the machine
				}
				o.Profiles = append(o.Profiles, doctor.Profile{Key: filepath.Base(d), Dir: d})
			}
			lines := doctor.Run(o)
			if err := doctor.Write(env.Stdout, lines, useColor(env, env.Stdout)); err != nil {
				return err
			}
			if doctor.HasFail(lines) {
				return exitError{code: 1}
			}
			return nil
		},
	}
}
