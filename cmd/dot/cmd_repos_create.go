package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"github.com/fmatsos/dot/internal/clone"
	"github.com/spf13/cobra"
)

const reposCreateUsage = "usage: dot repos create [name] [--provider github|gitlab] [--visibility private|public|internal] [--owner owner]"

// forgeCLI maps a provider to the CLI that creates and describes its repositories.
var forgeCLI = map[string]string{"github": "gh", "gitlab": "glab"}

// forgeOutput runs a forge CLI and returns its trimmed stdout; swapped by tests.
var forgeOutput = func(env *Env, name string, args ...string) (string, error) {
	c := exec.Command(name, args...)
	c.Stdin, c.Stderr = env.Stdin, env.Stderr
	out, err := c.Output()
	return strings.TrimSpace(string(out)), err
}

// askMissing asks on the terminal until the answer is one of choices (any non-empty answer when nil).
func askMissing(value *string, label string, choices []string) error {
	for *value == "" {
		prompt := label + ": "
		if choices != nil {
			prompt = fmt.Sprintf("%s (%s): ", label, strings.Join(choices, "|"))
		}
		answer, err := promptVisible(prompt)
		if err != nil {
			return usagef("%s missing and no terminal to ask it\n%s", label, reposCreateUsage)
		}
		if answer = strings.TrimSpace(answer); choices == nil || slices.Contains(choices, answer) {
			*value = answer
		}
	}
	return nil
}

func newReposCreateCmd(env *Env) *cobra.Command {
	var provider, visibility, owner string
	cmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create a repository on GitHub or GitLab, then clone it under the source root",
		Long: "Create a repository on GitHub (gh) or GitLab (glab), then clone it like dot clone.\n\n" +
			"The name, --provider and --visibility are required: those missing are asked one by one " +
			"on the terminal, none when all are given. --owner is an organization or group, else " +
			"the authenticated account. internal is GitLab only.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 1 {
				return usagef(reposCreateUsage)
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			var name string
			if len(args) > 0 {
				name = args[0]
			}
			if err := askMissing(&name, "name", nil); err != nil {
				return err
			}
			if err := askMissing(&provider, "provider", []string{"github", "gitlab"}); err != nil {
				return err
			}
			visibilities := []string{"private", "public"}
			if provider == "gitlab" {
				visibilities = append(visibilities, "internal")
			}
			if err := askMissing(&visibility, "visibility", visibilities); err != nil {
				return err
			}
			bin, ok := forgeCLI[provider]
			if !ok || !slices.Contains(visibilities, visibility) || strings.ContainsAny(name, "/ \t\n") {
				return usagef(reposCreateUsage)
			}
			full := name
			if owner != "" {
				full = owner + "/" + name
			}
			if _, err := forgeOutput(env, bin, "repo", "create", full, "--"+visibility); err != nil {
				return fmt.Errorf("repos create: %s repo create failed: %w", bin, err)
			}
			var url string
			var err error
			if provider == "github" {
				url, err = forgeOutput(env, bin, "repo", "view", full, "--json", "sshUrl", "-q", ".sshUrl")
			} else {
				url, err = forgeOutput(env, bin, "repo", "view", full, "-F", "json")
				if err == nil {
					var p struct {
						SSHURL string `json:"ssh_url_to_repo"`
					}
					err = json.Unmarshal([]byte(url), &p)
					url = p.SSHURL
				}
			}
			if err != nil {
				return fmt.Errorf("repos create: created, but its URL is unknown: %w", err)
			}
			target, _, err := clone.Clone(url, clone.Options{
				Root: srcRoot(env), Stdin: env.Stdin, Stdout: env.Stdout, Stderr: env.Stderr,
			})
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				return exitError{code: max(ee.ExitCode(), 1)}
			} else if err != nil {
				return err
			}
			_, err = fmt.Fprintln(env.Stdout, target)
			return err
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "github or gitlab")
	cmd.Flags().StringVar(&visibility, "visibility", "", "private, public or internal (GitLab)")
	cmd.Flags().StringVar(&owner, "owner", "", "organization or group (default: the authenticated account)")
	return cmd
}
