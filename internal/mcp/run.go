package mcp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
)

// Options drives Run.
type Options struct {
	Home string
	// Profiles lists the profiles in registry order. Their servers.json files, in merge order (the
	// last wins), are the shared source when ~/.config/mcp/servers.json does not exist: always when
	// FallbackOnApply, else only in a dry run (a first install -n has not linked home/ yet). Their
	// secrets.local files give the secret names a server may use.
	Profiles        []Profile
	FallbackOnApply bool
	DryRun          bool
	Out             io.Writer
	LookPath        func(string) (string, error)                      // defaults to exec.LookPath
	Exec            func(name string, args ...string) ([]byte, error) // stdout only; defaults to exec.Command
}

// Profile is one profile clone feeding the servers.
type Profile struct {
	Key     string // registry key, encoded as `dot -p <Key>` in wrapped servers; "" for no -p
	Servers string // its servers.json
	Secrets string // its secrets.local: only the NAME= prefixes are read, never a value
}

// ProfileKey is the registry key of the clone dir when it is <home>/.dot/<key>, else "": a clone
// elsewhere (DOTFILES_DEPLOY) cannot be selected with -p.
func ProfileKey(home, dir string) string {
	if filepath.Dir(dir) == filepath.Join(home, ".dot") {
		return filepath.Base(dir)
	}
	return ""
}

var secretNameRe = regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_]*)=`)

// KnownSecrets collects the secret names declared in the given secrets.local files.
func KnownSecrets(files []string) map[string]bool {
	known := map[string]bool{}
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(nil, 1<<20)
		for sc.Scan() {
			if m := secretNameRe.FindStringSubmatch(sc.Text()); m != nil {
				known[m[1]] = true
			}
		}
		fh.Close()
	}
	return known
}

func readSource(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, ErrSource
	}
	return ParseSource(data)
}

// Run renders the shared servers into every installed tool: it validates both sources, reads the
// three tool states, prints the plan ("<outil> : <action> <serveur>" or "<outil> : à jour"), then
// applies it unless DryRun. Nothing is written or called before the whole validation passed.
// Errors are plain messages; the caller prefixes them with "mcp : ".
func Run(o Options) error {
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	if o.Exec == nil {
		o.Exec = func(name string, args ...string) ([]byte, error) { return exec.Command(name, args...).Output() }
	}
	dir := filepath.Join(o.Home, ".config", "mcp")
	owners := make([]Owner, len(o.Profiles))
	for i, p := range o.Profiles {
		owners[i] = Owner{p.Key, KnownSecrets([]string{p.Secrets})}
	}
	shared, err := sharedSources(o, owners, filepath.Join(dir, "servers.json"))
	if err != nil {
		return err
	}
	local := map[string]any{}
	if _, err := os.Stat(filepath.Join(dir, "servers.local.json")); err == nil {
		if local, err = readSource(filepath.Join(dir, "servers.local.json")); err != nil {
			return errors.New("servers.local.json invalide")
		}
	}
	desired, err := Convert(shared, local, owners, filepath.Join(o.Home, ".local", "bin", "dot"))
	if err != nil {
		return err
	}
	want := map[string]any{}
	for name, s := range desired {
		want[name] = s.Value()
	}

	var steps []Step
	present := map[string]bool{}
	var claudeState, codexState Entries
	if _, err := o.LookPath("claude"); err == nil {
		present["claude"] = true
		claudeState = Entries{}
		if data, err := os.ReadFile(filepath.Join(o.Home, ".claude.json")); err == nil {
			if claudeState, err = ClaudeEntries(data); err != nil {
				return errors.New("configuration Claude illisible")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return errors.New("configuration Claude illisible")
		}
		steps = append(steps, Plan("claude", want, claudeState)...)
	}
	if _, err := o.LookPath("codex"); err == nil {
		present["codex"] = true
		data, err := o.Exec("codex", "mcp", "list", "--json")
		if err == nil {
			codexState, err = CodexEntries(data)
		}
		if err != nil {
			return errors.New("configuration Codex illisible")
		}
		steps = append(steps, Plan("codex", want, codexState)...)
	}
	var openOld, openNew map[string]any
	openTarget := filepath.Join(o.Home, ".config", "opencode", "config.json")
	if _, err := o.LookPath("opencode"); err == nil {
		present["opencode"] = true
		if st, err := os.Lstat(openTarget); err == nil && st.Mode()&os.ModeSymlink != 0 {
			return errors.New("config.json OpenCode doit être un fichier local, pas un lien")
		}
		openOld = map[string]any{}
		if data, err := os.ReadFile(openTarget); err == nil {
			if openOld, err = ParseOpenCode(data); err != nil {
				return errors.New("configuration OpenCode illisible")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return errors.New("configuration OpenCode illisible")
		}
		openNew = RenderOpenCode(openOld, desired)
		// Compare the tool's full generated entries; every other key and server stays as is.
		generated := map[string]any{}
		for name := range desired {
			generated[name] = openServers(openNew)[name]
		}
		steps = append(steps, Plan("opencode", generated, openServers(openOld))...)
	}

	for _, tool := range []string{"claude", "codex", "opencode"} {
		if !present[tool] {
			continue
		}
		changed := false
		for _, s := range steps {
			if s.Tool == tool {
				fmt.Fprintf(o.Out, "%s : %s %s\n", tool, s.Action, s.Name)
				changed = true
			}
		}
		if !changed {
			fmt.Fprintf(o.Out, "%s : à jour\n", tool)
		}
	}
	if o.DryRun {
		return nil
	}
	for _, s := range steps {
		if s.Tool == "opencode" {
			continue
		}
		if err := o.apply(s, desired[s.Name]); err != nil {
			return err
		}
	}
	if present["opencode"] && canon(openOld) != canon(openNew) {
		data, err := Pretty(openNew)
		if err != nil {
			return err
		}
		return writePrivate(openTarget, data)
	}
	return nil
}

// sharedSources loads the shared servers.json, or the profile files when it is not linked yet.
func sharedSources(o Options, owners []Owner, linked string) ([]Source, error) {
	if _, err := os.Stat(linked); err != nil && len(o.Profiles) > 0 && (o.DryRun || o.FallbackOnApply) {
		var out []Source
		for i, p := range o.Profiles {
			if _, err := os.Stat(p.Servers); err != nil {
				continue
			}
			m, err := readSource(p.Servers)
			if err != nil {
				return nil, errors.New("servers.json invalide ou absent")
			}
			out = append(out, Source{m, &owners[i]})
		}
		if len(out) == 0 {
			return nil, errors.New("servers.json invalide ou absent")
		}
		return out, nil
	}
	m, err := readSource(linked)
	if err != nil {
		return nil, errors.New("servers.json invalide ou absent")
	}
	return []Source{{Servers: m}}, nil
}

// apply runs one Claude or Codex step: a change is a remove then an add, like the CLIs expect.
func (o Options) apply(s Step, srv *Server) error {
	label := map[string]string{"claude": "Claude", "codex": "Codex"}[s.Tool]
	run := func(args ...string) error {
		_, err := o.Exec(s.Tool, args...)
		return err
	}
	if s.Action != "ajout" {
		args := []string{"mcp", "remove", s.Name}
		if s.Tool == "claude" {
			args = []string{"mcp", "remove", "-s", "user", s.Name}
		}
		if run(args...) != nil {
			return fmt.Errorf("%s : retrait impossible (%s)", label, s.Name)
		}
	}
	if s.Action == "retrait" {
		return nil
	}
	var args []string
	switch {
	case s.Tool == "claude":
		args = []string{"mcp", "add-json", "-s", "user", s.Name, srv.JSON()}
	case srv.http():
		args = []string{"mcp", "add", s.Name, "--url", srv.URL}
	default:
		args = []string{"mcp", "add", s.Name}
		for _, k := range slices.Sorted(maps.Keys(srv.Env)) {
			args = append(args, "--env", k+"="+srv.Env[k])
		}
		args = append(append(args, "--", srv.Command), srv.Args...)
	}
	if run(args...) != nil {
		return fmt.Errorf("%s : ajout impossible (%s)", label, s.Name)
	}
	return nil
}

// writePrivate replaces path atomically with a 0600 file, creating its directory (0700) when needed.
func writePrivate(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".mcp.*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
