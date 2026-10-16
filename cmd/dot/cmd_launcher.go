package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/godbus/dbus/v5"
	"github.com/spf13/cobra"
)

func init() { register(newLauncherCmd) }

const (
	launcherDesktopID = "io.github.fmatsos.dot.desktop"
	launcherIni       = "[Shell Search Provider]\nDesktopId=" + launcherDesktopID + "\nBusName=" + launcherBusName + "\nObjectPath=" + launcherPath + "\nVersion=2\n"
)

// launcherSystemDir: GNOME Shell reads search providers from the system data dirs only, never ~/.local/share.
var launcherSystemDir = "/usr/local/share/gnome-shell/search-providers"

// launcherSelf is the binary the shell and the terminal will run; swapped by tests.
var launcherSelf = os.Executable

func sudo(env *Env, args ...string) error {
	c := exec.Command("sudo", args...)
	c.Stdin, c.Stdout, c.Stderr = env.Stdin, env.Stdout, env.Stderr
	return c.Run()
}

// installSystemFile writes dest, through sudo when the directory is not ours; swapped by tests.
var installSystemFile = func(env *Env, dest string, data []byte) error {
	err := os.MkdirAll(filepath.Dir(dest), 0o755)
	if err == nil {
		err = os.WriteFile(dest, data, 0o644)
	}
	if !errors.Is(err, fs.ErrPermission) {
		return err
	}
	tmp, err := os.CreateTemp("", "dot-launcher-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	fmt.Fprintf(env.Stderr, "sudo: writing %s\n", dest)
	return sudo(env, "install", "-D", "-m", "644", tmp.Name(), dest)
}

// removeSystemFile removes dest, through sudo when it is not ours; a missing file is fine; swapped by tests.
var removeSystemFile = func(env *Env, dest string) error {
	err := os.Remove(dest)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if !errors.Is(err, fs.ErrPermission) {
		return err
	}
	fmt.Fprintf(env.Stderr, "sudo: removing %s\n", dest)
	return sudo(env, "rm", "-f", dest)
}

// reloadBus asks the session bus to rescan its .service files: dbus-broker does not watch them. A
// failure only warns, since dbus-daemon has already noticed; swapped by tests.
var reloadBus = func(env *Env) {
	conn, err := dbus.ConnectSessionBus()
	if err == nil {
		defer conn.Close()
		err = conn.BusObject().Call("org.freedesktop.DBus.ReloadConfig", 0).Err
	}
	if err != nil {
		fmt.Fprintf(env.Stderr, "dot: the session bus did not reload its services (%v)\n", err)
	}
}

func launcherFiles(env *Env) (desktop, service, ini string) {
	data := env.Getenv("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(env.Home, ".local", "share")
	}
	return filepath.Join(data, "applications", launcherDesktopID),
		filepath.Join(data, "dbus-1", "services", launcherBusName+".service"),
		filepath.Join(launcherSystemDir, launcherBusName+".search-provider.ini")
}

func writeUserFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// launcherInstall puts the three files in place. The visible .desktop comes last: the shell reloads its
// providers when an application appears, and by then the other two are there.
func launcherInstall(env *Env) error {
	self, err := launcherSelf()
	if err != nil {
		return err
	}
	// ponytail: the .service and .desktop Exec lines are unquoted, so no exotic path.
	if strings.Contains(self, "/go-build") || strings.ContainsAny(self, " \t\"'\\$`;&|<>*?#()~") {
		return fmt.Errorf("unusable binary: %s (install dot, then run dot launcher --install)", self)
	}
	desktop, service, ini := launcherFiles(env)
	if err := installSystemFile(env, ini, []byte(launcherIni)); err != nil {
		return fmt.Errorf("launcher: %w", err)
	}
	if err := writeUserFile(service, "[D-BUS Service]\nName="+launcherBusName+"\nExec="+self+" launcher\n"); err != nil {
		return fmt.Errorf("launcher: %w", err)
	}
	reloadBus(env)
	entry := "[Desktop Entry]\nType=Application\nName=dot\nComment=SSH aliases in GNOME search\n" +
		"Exec=bash -c \"" + self + " status; read -rp 'Press Enter to close ' _\"\nTerminal=true\nIcon=utilities-terminal\nCategories=Utility;\n"
	if err := writeUserFile(desktop, entry); err != nil {
		return fmt.Errorf("launcher: %w", err)
	}
	for _, f := range []string{ini, service, desktop} {
		fmt.Fprintf(env.Stdout, "wrote: %s\n", f)
	}
	fmt.Fprintln(env.Stdout, "The ssh.conf aliases are now in GNOME search (Activities overview, Search Light).")
	return nil
}

func launcherUninstall(env *Env) error {
	desktop, service, ini := launcherFiles(env)
	for _, f := range []string{desktop, service} {
		if err := os.Remove(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("launcher: %w", err)
		}
		fmt.Fprintf(env.Stdout, "removed: %s\n", f)
	}
	reloadBus(env)
	if err := removeSystemFile(env, ini); err != nil {
		return fmt.Errorf("launcher: %w", err)
	}
	fmt.Fprintf(env.Stdout, "removed: %s\n", ini)
	return nil
}

func newLauncherCmd(env *Env) *cobra.Command {
	var install, uninstall bool
	cmd := &cobra.Command{
		Use:   "launcher [--install | --uninstall | add|update|rm|get|list …]",
		Short: "Put aliases in GNOME search",
		Long: "Provides aliases to GNOME Shell search: the Activities overview and the extensions that " +
			"reuse it (Search Light). Two sources: <profile>/launcher.conf, one \"<alias> <command>\" line " +
			"(add, update, rm, get and list edit it), whose command is run by sh -c; and <profile>/ssh.conf: " +
			"typing \"ssh\", or the start of an alias, offers \"ssh <alias>\", and choosing that result opens a " +
			"terminal running dot ssh <alias>. The {…} of an ssh alias are hidden.\n\n" +
			"--install writes the file the shell reads (" + launcherSystemDir + ", with sudo if needed), the " +
			"D-Bus service and a \"dot\" entry in ~/.local/share; --uninstall removes all three. " +
			"Without options, dot serves the provider on the session bus, started by the shell on demand; " +
			"it exits after a few minutes without a search. It reads the default profile (or $DOT_PROFILE).",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usagef("unknown command: launcher %s", args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch {
			case install && uninstall:
				return usagef("usage: dot launcher [--install | --uninstall]")
			case install:
				return launcherInstall(env)
			case uninstall:
				return launcherUninstall(env)
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return serveProvider(ctx, env)
		},
	}
	cmd.Flags().BoolVar(&install, "install", false, "install the provider (sudo for the file the shell reads)")
	cmd.Flags().BoolVar(&uninstall, "uninstall", false, "remove what --install wrote")
	cmd.AddCommand(newLauncherAliasCmds(env)...)
	return cmd
}
