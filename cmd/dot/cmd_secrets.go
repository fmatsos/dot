package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/fmatsos/dot/internal/secrets"
	"github.com/spf13/cobra"
)

func init() { register(newSecretsCmd) }

// Swapped by tests: the real ones replace the process and read /dev/tty.
var (
	execProcess  = syscall.Exec
	promptHidden = secrets.PromptHidden
)

// secretFail reports err the way dot secrets always did: the sentinel messages (locked vault,
// logged-out Proton Pass) as is with exit code 3, anything else as "secret: …" with exit code 1.
// The text never holds a value.
func secretFail(env *Env, err error) error {
	if errors.Is(err, secrets.ErrLocked) || errors.Is(err, secrets.ErrPassLoggedOut) {
		fmt.Fprintln(env.Stderr, err)
		return exitError{code: 3}
	}
	fmt.Fprintf(env.Stderr, "secret: %v\n", err)
	return exitError{code: 1}
}

func secretFailf(env *Env, format string, a ...any) error {
	return secretFail(env, fmt.Errorf(format, a...))
}

func newSecretsCmd(env *Env) *cobra.Command {
	store := func() (*secrets.Store, error) {
		dir, err := env.ProfileDir()
		if err != nil {
			return nil, err
		}
		return secrets.New(dir), nil
	}
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Read secrets when a command needs them",
		Long: "dot secrets — reads secrets when a command needs them, never in the shell.\n\n" +
			"Names come from secrets.local, in the profile directory: NAME=bw:<item> or\n" +
			"NAME=pass:<vault>/<item>[#field] (#field: a custom field, e.g. an API key;\n" +
			"otherwise the password). Values are never passed as process arguments.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 || args[0] == "help" && len(args) == 1 {
				return nil
			}
			return usagef("unknown command: secrets %s", args[0])
		},
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	var field string
	addCmd := &cobra.Command{
		Use:   "add NAME <ref>",
		Short: "Create a secret or reuse an existing item",
		Long: "Create a secret or reuse an existing item.\n\n" +
			"Reference: bw:<item> or pass:<vault>/<item>[#field]. Hidden input asked twice on the terminal,\n" +
			"otherwise stdin (one trailing newline removed). The value is checked before it is added to\n" +
			"secrets.local (600); an existing item asks for no input.",
		Args: exactArgs(2, "dot secrets add NAME <ref>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := args[1]
			if field != "" {
				if !strings.HasPrefix(ref, "pass:") || strings.Contains(ref, "#") || strings.Contains(field, "#") {
					return usagef("--field: only with pass:<vault>/<item>, with no # in the reference or the field")
				}
				ref += "#" + field
			}
			st, err := store()
			if err != nil {
				return err
			}
			created, err := st.Add(cmd.Context(), args[0], ref, func() (string, error) { return readNewValue(env) })
			if err != nil {
				return secretFail(env, err)
			}
			if created {
				fmt.Fprintf(env.Stdout, "%s: item created and reference saved\n", args[0])
			} else {
				fmt.Fprintf(env.Stdout, "%s: existing item reused, reference saved\n", args[0])
			}
			return nil
		},
	}
	addCmd.Flags().StringVar(&field, "field", "", "custom field to read (pass), same as #field in the reference")
	cmd.AddCommand(
		addCmd,
		&cobra.Command{
			Use:   "get NAME",
			Short: "Print the value, without a trailing newline",
			Long: "Print the value, without a trailing newline.\n\n" +
				"For a substitution: VAR=$(dot secrets get NAME). Bitwarden locked: exit code 3.",
			Args: exactArgs(1, "dot secrets get NAME"),
			RunE: func(cmd *cobra.Command, args []string) error {
				st, err := store()
				if err != nil {
					return err
				}
				v, err := st.Get(cmd.Context(), args[0])
				if err != nil {
					return secretFail(env, err)
				}
				_, err = io.WriteString(env.Stdout, v)
				return err
			},
		},
		&cobra.Command{
			Use:   "run NAME... -- CMD...",
			Short: "Run CMD with these variables, and only these",
			Long: "Run CMD with these variables, and only these.\n\n" +
				"All values are read before launching; CMD's exit code is kept.",
			Args: func(cmd *cobra.Command, args []string) error {
				if d := cmd.ArgsLenAtDash(); d < 1 || len(args) <= d {
					return usagef("usage: dot secrets run NAME... -- CMD...")
				}
				return nil
			},
			RunE: func(cmd *cobra.Command, args []string) error {
				st, err := store()
				if err != nil {
					return err
				}
				d := cmd.ArgsLenAtDash()
				// Resolve everything first: a manager never inherits the other fetched values.
				kv, err := st.Resolve(cmd.Context(), args[:d])
				if err != nil {
					return secretFail(env, err)
				}
				argv := args[d:]
				path, err := exec.LookPath(argv[0])
				if err != nil {
					code := 126
					if errors.Is(err, exec.ErrNotFound) {
						code = 127
					}
					return exitError{code, fmt.Sprintf("%s: command not found", argv[0])}
				}
				err = execProcess(path, argv, secrets.OverrideEnv(os.Environ(), kv))
				return exitError{126, fmt.Sprintf("%s: cannot run (%v)", argv[0], err)}
			},
		},
		&cobra.Command{
			Use:   "unlock",
			Short: "Unlock Bitwarden and keep the session (file mode 600)",
			Long: "Unlock Bitwarden and keep the session (file mode 600).\n\n" +
				"The master password is asked on the terminal and passed to bw through the environment.",
			Args: exactArgs(0, "dot secrets unlock"),
			RunE: func(cmd *cobra.Command, _ []string) error {
				st, err := store()
				if err != nil {
					return err
				}
				pw, err := promptHidden("Bitwarden master password: ")
				if err != nil {
					return secretFailf(env, "Bitwarden: input cancelled")
				}
				if err := st.Unlock(cmd.Context(), pw); err != nil {
					return secretFail(env, err)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "lock",
			Short: "Lock Bitwarden and clear the kept session",
			Args:  exactArgs(0, "dot secrets lock"),
			RunE: func(cmd *cobra.Command, _ []string) error {
				st, err := store()
				if err != nil {
					return err
				}
				if err := st.Lock(cmd.Context()); err != nil {
					return secretFail(env, err)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Show which secrets are readable, without printing values",
			Long: "Show which secrets are readable, without printing values.\n\n" +
				"One state per secret: readable, locked or unavailable.",
			Args: exactArgs(0, "dot secrets status"),
			RunE: func(cmd *cobra.Command, _ []string) error {
				st, err := store()
				if err != nil {
					return err
				}
				entries, err := st.Status(cmd.Context())
				if err != nil {
					return secretFail(env, err)
				}
				for _, e := range entries {
					fmt.Fprintln(env.Stdout, e)
				}
				return nil
			},
		},
	)
	return cmd
}

// readNewValue asks for the value twice on the terminal, else reads stdin minus one final newline.
func readNewValue(env *Env) (string, error) {
	if secrets.IsTerminal(env.Stdin) {
		v, err := promptHidden("Secret value: ")
		if err != nil {
			return "", errors.New("input cancelled")
		}
		c, err := promptHidden("Confirm the value: ")
		if err != nil {
			return "", errors.New("input cancelled")
		}
		if v != c {
			return "", errors.New("the values do not match")
		}
		return v, nil
	}
	b, err := io.ReadAll(env.Stdin)
	if err != nil {
		return "", errors.New("cannot read stdin")
	}
	return strings.TrimSuffix(string(b), "\n"), nil
}
