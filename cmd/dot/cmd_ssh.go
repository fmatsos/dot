package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/fmatsos/dot/internal/secrets"
	"github.com/spf13/cobra"
)

func init() { register(newSSHCmd) }

const sshUsage = "usage: dot ssh [<name> [ssh arguments…]]"

const sshFile = "ssh.conf"

// placeholder is a {…} of an alias line, replaced at connection time by a vault value.
var placeholder = regexp.MustCompile(`\{([^{}]+)\}`)

// secretArg is password={X} or key={X}; field is a word of the line, a {…} counting as one (it may hold spaces).
var (
	secretArg = regexp.MustCompile(`^(password|key)=\{([^{}]+)\}$`)
	field     = regexp.MustCompile(`(?:\{[^{}]*\}|\S)+`)
)

// promptVisible asks a non-password question (host key, one-time code) on /dev/tty; swapped by tests.
var promptVisible = func(prompt string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	defer tty.Close()
	fmt.Fprint(tty, prompt)
	line, err := bufio.NewReader(tty).ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
}

// sshAliases reads <profile>/ssh.conf (versioned, so it holds no secret): one "<name> <ssh arguments…>"
// per line, # for comments.
// ponytail: whitespace split outside {…}, no quoting; a later line wins. Use ~/.ssh/config for anything fancier.
func sshAliases(file string) (names []string, args map[string][]string, err error) {
	f, err := os.Open(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	args = map[string][]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := field.FindAllString(sc.Text(), -1)
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if _, dup := args[fields[0]]; !dup {
			names = append(names, fields[0])
		}
		args[fields[0]] = fields[1:]
	}
	return names, args, sc.Err()
}

// sshValue reads one {…}: a secrets.local name, or a bw:/pass: reference used as is (nothing to
// configure on a new machine, only the vault to unlock).
func sshValue(ctx context.Context, st *secrets.Store, key string) (string, error) {
	if strings.HasPrefix(key, "bw:") || strings.HasPrefix(key, "pass:") {
		if !secrets.Valid("REF", key) {
			return "", fmt.Errorf("invalid reference: %s", key)
		}
		return st.Read(ctx, secrets.Mapping{Name: key, Ref: key})
	}
	return st.Get(ctx, key)
}

// sshKeyValue reads the private key of a key={X}: an inline pass: reference without #field reads
// the private_key of its SSH key item.
func sshKeyValue(ctx context.Context, st *secrets.Store, key string) (string, error) {
	if strings.HasPrefix(key, "pass:") && !strings.Contains(key, "#") {
		key += "#private_key"
	}
	return sshValue(ctx, st, key)
}

// startAgent runs a private ssh-agent (socket in a 0700 directory of $XDG_RUNTIME_DIR, so in memory)
// holding only this key, fed through ssh-add's stdin: the key is never on disk, in an argument or in
// the environment. stop kills the agent and removes the directory.
// ponytail: a killed dot leaves the agent behind; its key then expires after ssh-add's -t.
func startAgent(key string) (sock string, stop func(), err error) {
	dir, err := os.MkdirTemp(os.Getenv("XDG_RUNTIME_DIR"), "dot-ssh-")
	if err != nil {
		return "", nil, err
	}
	sock = filepath.Join(dir, "a.sock")
	agent := exec.Command("ssh-agent", "-D", "-a", sock)
	// Own process group: a ctrl-C at the terminal reaches ssh and dot, not the agent ssh still needs.
	agent.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stop = func() {
		if agent.Process != nil {
			agent.Process.Kill()
			agent.Wait()
		}
		os.RemoveAll(dir)
	}
	if err = agent.Start(); err != nil {
		stop()
		return "", nil, fmt.Errorf("ssh-agent: %w", err)
	}
	for i := 0; i < 100; i++ {
		if _, err = os.Stat(sock); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	add := exec.Command("ssh-add", "-t", "600", "-")
	add.Env = secrets.OverrideEnv(os.Environ(), []string{"SSH_AUTH_SOCK=" + sock, "SSH_ASKPASS=/bin/false", "SSH_ASKPASS_REQUIRE=force"})
	add.Stdin = strings.NewReader(strings.TrimSuffix(key, "\n") + "\n")
	if err := add.Run(); err != nil {
		stop()
		return "", nil, errors.New("ssh-add: key refused by the agent")
	}
	return sock, stop, nil
}

// sshExpand replaces every {…} of s. A value is never scanned again for placeholders.
func sshExpand(ctx context.Context, st *secrets.Store, s string) (string, error) {
	var err error
	out := placeholder.ReplaceAllStringFunc(s, func(m string) string {
		v, e := sshValue(ctx, st, m[1:len(m)-1])
		if e != nil && err == nil {
			err = e
		}
		return v
	})
	return out, err
}

// sshAskpass answers ssh when it runs `$SSH_ASKPASS <invite>` with DOT_SSH_ASKPASS set by `dot ssh`:
// the password is read from the vault at that moment and goes to ssh through our stdout, never through
// an argument or an environment variable. Any other question (host key, one-time code) goes to the terminal.
// ponytail: also answers the password prompt of a ProxyJump hop, with this password.
func sshAskpass(env *Env, args []string) (code int, ok bool) {
	key := env.Getenv("DOT_SSH_ASKPASS")
	if key == "" || len(args) != 1 {
		return 0, false
	}
	var v string
	var err error
	if strings.Contains(strings.ToLower(args[0]), "password") {
		var dir string
		if dir, err = env.ProfileDir(); err == nil {
			v, err = sshValue(context.Background(), secrets.New(dir), key)
		}
	} else {
		v, err = promptVisible(args[0])
	}
	if err != nil {
		var x exitError
		if errors.As(secretFail(env, err), &x) {
			return x.code, true
		}
		return 1, true
	}
	fmt.Fprintln(env.Stdout, v)
	return 0, true
}

func newSSHCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "ssh [<name> [ssh arguments…]]",
		Short: "Connect over SSH to a profile alias",
		Long: "Connect over SSH to a profile alias.\n\n" +
			"Without arguments: list the aliases. The aliases live in <profile directory>/ssh.conf, versioned: one " +
			"\"<name> <ssh arguments…>\" line each, # for a comment. Arguments after the name " +
			"are appended after the alias ones, so a remote command works: dot ssh prod uptime.\n\n" +
			"Nothing secret in this file: {X} is replaced at connection time by the secret X of secrets.local, " +
			"or by {bw:<item>[#field]} / {pass:<vault>/<item>[#field]} read straight from the vault " +
			"(e.g. \"pve root@{pass:Infra/pve#host}\"). \"password={X}\" reads the password from the vault " +
			"when ssh asks for it (SSH_ASKPASS, OpenSSH >= 8.4): it goes neither in an argument nor in " +
			"the environment, and a single attempt is made. \"key={pass:<vault>/<item>}\" loads the private key of a " +
			"Proton Pass SSH key item (or {X} with X=pass:…#private_key) into a private ephemeral agent, " +
			"destroyed at the end: the key is never on disk. Otherwise keys stay ssh's own " +
			"(-i <path>).",
		DisableFlagsInUseLine: true,
		// Everything after the name belongs to ssh, flags included.
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
				return cmd.Help()
			}
			dir, err := env.ProfileDir()
			if err != nil {
				return err
			}
			names, aliases, err := sshAliases(filepath.Join(dir, sshFile))
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			if len(args) == 0 {
				for _, n := range names {
					fmt.Fprintf(env.Stdout, "%s\t%s\n", n, strings.Join(aliases[n], " "))
				}
				return nil
			}
			target, ok := aliases[args[0]]
			if !ok {
				return usagef("unknown ssh alias: %s (see dot ssh)", args[0])
			}
			bin, err := exec.LookPath("ssh")
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			st := secrets.New(dir)
			var argv, extra []string
			var password, key string
			for _, a := range target {
				if kind, _, ok := strings.Cut(a, "="); ok && (kind == "password" || kind == "key") {
					m := secretArg.FindStringSubmatch(a)
					if m == nil {
						return usagef("%s: %s= expects {X}", args[0], kind)
					}
					if kind == "key" {
						key = m[2]
					} else {
						password = m[2]
					}
					continue
				}
				a, err := sshExpand(cmd.Context(), st, a)
				if err != nil {
					return secretFail(env, err)
				}
				argv = append(argv, a)
			}
			if key != "" {
				pk, err := sshKeyValue(cmd.Context(), st, key)
				if err != nil {
					return secretFail(env, err)
				}
				sock, stop, err := startAgent(pk)
				if err != nil {
					return err
				}
				defer stop()
				// ssh and the terminal's ctrl-C: dot waits for ssh, then cleans up.
				sig := make(chan os.Signal, 1)
				signal.Notify(sig, os.Interrupt)
				defer signal.Stop(sig)
				extra = append(extra, "SSH_AUTH_SOCK="+sock)
			}
			if password != "" {
				self, err := os.Executable()
				if err != nil {
					return err
				}
				argv = append([]string{"-o", "PreferredAuthentications=keyboard-interactive,password", "-o", "NumberOfPasswordPrompts=1"}, argv...)
				extra = append(extra, "SSH_ASKPASS="+self, "SSH_ASKPASS_REQUIRE=force", "DOT_SSH_ASKPASS="+password, "DOTFILES_DEPLOY="+dir)
			}
			c := exec.Command(bin)
			if len(extra) > 0 {
				c.Env = secrets.OverrideEnv(os.Environ(), extra)
			}
			c.Args = append(append(c.Args, argv...), args[1:]...)
			return runWithStdio(env, c)
		},
		ValidArgsFunction: func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if dir, err := env.ProfileDir(); err == nil && len(args) == 0 {
				names, _, _ := sshAliases(filepath.Join(dir, sshFile))
				return names, cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
	}
}
