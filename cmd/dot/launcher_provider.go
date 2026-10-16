package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	launcherBusName = "io.github.fmatsos.dot.SearchProvider"
	launcherPath    = "/io/github/fmatsos/dot/SearchProvider"
	launcherIface   = "org.gnome.Shell.SearchProvider2"
	// The shell restarts the service on the next search, so it can leave when nobody asks.
	launcherIdle = 3 * time.Minute
)

// startTerminal opens argv in a new terminal window; swapped by tests.
var startTerminal = func(argv []string) error {
	c := exec.Command(argv[0], argv[1:]...)
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := c.Start(); err != nil {
		return err
	}
	return c.Process.Release()
}

// startShell runs an alias command line detached, with the directory of dot first on PATH (the shell
// starts the service with a short environment); swapped by tests.
var startShell = func(self, command string) error {
	c := exec.Command("sh", "-c", command)
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	c.Env = append(os.Environ(), "PATH="+filepath.Dir(self)+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := c.Start(); err != nil {
		return err
	}
	return c.Process.Release()
}

// terminalArgv wraps `dot ssh <alias>` for the first terminal launcher found on PATH.
// ponytail: fixed list, add the terminal you use when none matches.
func terminalArgv(self, alias string) []string {
	for _, t := range [][]string{{"xdg-terminal-exec", "--"}, {"ptyxis", "--"}, {"gnome-terminal", "--"}} {
		if bin, err := exec.LookPath(t[0]); err == nil {
			return append(append([]string{bin}, t[1:]...), self, "ssh", alias)
		}
	}
	return nil
}

// provider answers GNOME Shell's org.gnome.Shell.SearchProvider2 calls with the aliases of the profile:
// launcher.conf ones ("run:<alias>" ids) and ssh.conf ones ("ssh:<alias>"). Its exported methods are
// the D-Bus interface.
type provider struct {
	env  *Env
	self string // the dot binary the terminal runs
	idle *time.Timer
}

// aliases are the ssh.conf aliases a search may offer: a name starting with "-" would be read
// as a flag by `dot ssh`.
func (p *provider) aliases() (names []string, args map[string][]string) {
	dir, err := p.env.ProfileDir()
	if err != nil {
		return nil, nil
	}
	all, args, err := sshAliases(filepath.Join(dir, sshFile))
	if err != nil {
		return nil, nil
	}
	return slices.DeleteFunc(all, func(n string) bool { return strings.HasPrefix(n, "-") }), args
}

// launcher are the launcher.conf aliases, in file order.
func (p *provider) launcher() (names []string, cmds map[string]string) {
	dir, err := p.env.ProfileDir()
	if err != nil {
		return nil, nil
	}
	names, cmds, err = launcherAliases(filepath.Join(dir, launcherFile))
	if err != nil {
		return nil, nil
	}
	return names, cmds
}

// aliasMatches: each term is the start of the extra word (such as "ssh"), of the alias or of one of its
// dash, dot or underscore separated parts, so "ssh pr" and "pr" both find the ssh alias "prod-db".
func aliasMatches(name string, terms []string, extra ...string) bool {
	words := append(append([]string{name}, extra...), strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' || r == '.' })...)
	for _, t := range terms {
		t = strings.ToLower(t)
		if !slices.ContainsFunc(words, func(w string) bool { return strings.HasPrefix(strings.ToLower(w), t) }) {
			return false
		}
	}
	return len(terms) > 0
}

func (p *provider) find(terms []string) []string {
	p.idle.Reset(launcherIdle)
	found := []string{}
	run, _ := p.launcher()
	for _, n := range run {
		if aliasMatches(n, terms) {
			found = append(found, "run:"+n)
		}
	}
	names, _ := p.aliases()
	for _, n := range names {
		if aliasMatches(n, terms, "ssh") {
			found = append(found, "ssh:"+n)
		}
	}
	return found
}

func (p *provider) GetInitialResultSet(terms []string) ([]string, *dbus.Error) {
	return p.find(terms), nil
}

func (p *provider) GetSubsearchResultSet(_ []string, terms []string) ([]string, *dbus.Error) {
	return p.find(terms), nil
}

// GetResultMetas describes each alias; vault references in {…} of an ssh alias never reach the shell.
func (p *provider) GetResultMetas(ids []string) ([]map[string]dbus.Variant, *dbus.Error) {
	_, args := p.aliases()
	_, cmds := p.launcher()
	metas := []map[string]dbus.Variant{}
	for _, id := range ids {
		kind, name, _ := strings.Cut(id, ":")
		meta := map[string]dbus.Variant{"id": dbus.MakeVariant(id)}
		switch a, c := args[name], cmds[name]; {
		case kind == "ssh" && a != nil:
			meta["name"] = dbus.MakeVariant("ssh " + name)
			meta["description"] = dbus.MakeVariant(placeholder.ReplaceAllString(strings.Join(a, " "), "{…}"))
			meta["gicon"] = dbus.MakeVariant("utilities-terminal")
		case kind == "run" && c != "":
			meta["name"] = dbus.MakeVariant(name)
			meta["description"] = dbus.MakeVariant(c)
			meta["gicon"] = dbus.MakeVariant("system-run")
		default:
			continue
		}
		metas = append(metas, meta)
	}
	return metas, nil
}

// ActivateResult runs a launcher.conf alias, or opens `dot ssh <alias>` in a terminal. The id comes off
// the bus, so only an alias read from the profile files is used: the command line comes from the file
// and the ssh alias goes in an argv, never in a shell line.
func (p *provider) ActivateResult(id string, _ []string, _ uint32) *dbus.Error {
	p.idle.Reset(launcherIdle)
	kind, name, _ := strings.Cut(id, ":")
	if run, cmds := p.launcher(); kind == "run" && slices.Contains(run, name) {
		if err := startShell(p.self, cmds[name]); err != nil {
			return dbus.MakeFailedError(err)
		}
		return nil
	}
	names, _ := p.aliases()
	if kind != "ssh" || !slices.Contains(names, name) {
		return dbus.MakeFailedError(fmt.Errorf("unknown alias: %s", id))
	}
	argv := terminalArgv(p.self, name)
	if argv == nil {
		return dbus.MakeFailedError(errors.New("no terminal found (xdg-terminal-exec, ptyxis, gnome-terminal)"))
	}
	if err := startTerminal(argv); err != nil {
		return dbus.MakeFailedError(err)
	}
	return nil
}

func (p *provider) LaunchSearch(_ []string, _ uint32) *dbus.Error { return nil }

// serveProvider owns the bus name until ctx ends or launcherIdle passes without a call.
func serveProvider(ctx context.Context, env *Env) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer conn.Close()
	done := make(chan struct{})
	var once sync.Once
	p := &provider{env: env, self: self}
	p.idle = time.AfterFunc(launcherIdle, func() { once.Do(func() { close(done) }) })
	if err := conn.Export(p, launcherPath, launcherIface); err != nil {
		return err
	}
	reply, err := conn.RequestName(launcherBusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return errors.New("the search provider is already running")
	}
	select {
	case <-done:
	case <-ctx.Done():
	}
	return nil
}
