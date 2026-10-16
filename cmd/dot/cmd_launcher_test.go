package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// launcherProvider gives a provider reading this ssh.conf, and optionally this launcher.conf, from a
// throw-away profile.
func launcherProvider(t *testing.T, conf string, run ...string) *provider {
	t.Helper()
	env, _, _ := testEnv(t)
	dir := t.TempDir()
	t.Setenv("DOTFILES_DEPLOY", dir)
	if err := os.WriteFile(filepath.Join(dir, sshFile), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	if len(run) > 0 {
		if err := os.WriteFile(filepath.Join(dir, launcherFile), []byte(run[0]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &provider{env: env, self: "/opt/dot", idle: time.NewTimer(time.Hour)}
}

func TestLauncherSearch(t *testing.T) {
	p := launcherProvider(t, "prod-db -p 2222 deploy@10.0.0.4\nbox me@box\n-evil x\n")
	for terms, want := range map[string][]string{
		"ssh":      {"ssh:prod-db", "ssh:box"},
		"ssh pr":   {"ssh:prod-db"},
		"db":       {"ssh:prod-db"},
		"BO":       {"ssh:box"},
		"sh":       {},
		"zzz":      {},
		"ssh -evi": {},
	} {
		got, _ := p.GetInitialResultSet(strings.Fields(terms))
		if !slices.Equal(got, want) {
			t.Errorf("%q: %v, want %v", terms, got, want)
		}
	}
}

func TestLauncherMetasHideVaultReferences(t *testing.T) {
	p := launcherProvider(t, "pve root@{pass:Infra/pve#host} password={PVE_PW}\n")
	metas, _ := p.GetResultMetas([]string{"ssh:pve", "ssh:nope", "run:pve", "pve"})
	if len(metas) != 1 {
		t.Fatalf("%d results, want 1", len(metas))
	}
	if desc := metas[0]["description"].Value().(string); strings.Contains(desc, "Infra") || strings.Contains(desc, "PVE_PW") {
		t.Fatalf("a vault reference is visible: %q", desc)
	}
}

func TestLauncherActivate(t *testing.T) {
	p := launcherProvider(t, "prod me@prod\n-h x\n")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "xdg-terminal-exec"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	var got []string
	old := startTerminal
	startTerminal = func(argv []string) error { got = argv; return nil }
	t.Cleanup(func() { startTerminal = old })

	for _, id := range []string{"nope", "prod", "ssh:nope", "ssh:-h", "ssh:prod; rm -rf ~", "run:prod", "bad:prod"} {
		if err := p.ActivateResult(id, nil, 0); err == nil || got != nil {
			t.Fatalf("%q accepted: %v %v", id, err, got)
		}
	}
	if err := p.ActivateResult("ssh:prod", nil, 0); err != nil {
		t.Fatal(err)
	}
	if want := []string{filepath.Join(bin, "xdg-terminal-exec"), "--", "/opt/dot", "ssh", "prod"}; !slices.Equal(got, want) {
		t.Fatalf("terminal: %v, want %v", got, want)
	}
	t.Setenv("PATH", t.TempDir())
	if err := p.ActivateResult("ssh:prod", nil, 0); err == nil {
		t.Fatal("no terminal: an error is expected")
	}
}

func TestLauncherRunAliases(t *testing.T) {
	p := launcherProvider(t, "prod me@prod\n", "# shortcuts\ngh xdg-open https://github.com\nprod echo \"a b\" && true\n")
	// An alias of launcher.conf comes before the ssh one of the same name, and both stay reachable.
	if got, _ := p.GetInitialResultSet([]string{"gh"}); !slices.Equal(got, []string{"run:gh"}) {
		t.Fatalf("gh: %v", got)
	}
	if got, _ := p.GetInitialResultSet([]string{"pr"}); !slices.Equal(got, []string{"run:prod", "ssh:prod"}) {
		t.Fatalf("pr: %v", got)
	}
	metas, _ := p.GetResultMetas([]string{"run:gh"})
	if len(metas) != 1 || metas[0]["name"].Value() != "gh" || metas[0]["description"].Value() != "xdg-open https://github.com" {
		t.Fatalf("meta: %v", metas)
	}
	var self, got string
	old := startShell
	startShell = func(s, c, _ string) error { self, got = s, c; return nil }
	t.Cleanup(func() { startShell = old })
	if err := p.ActivateResult("run:prod", nil, 0); err != nil || self != "/opt/dot" || got != `echo "a b" && true` {
		t.Fatalf("run:prod: %v %q %q", err, self, got)
	}
}

func TestLauncherQueryAlias(t *testing.T) {
	p := launcherProvider(t, "", "c claude --model sonnet {q}\ngh xdg-open https://github.com\n")
	for terms, want := range map[string][]string{
		"c":           {"run:c"},
		"c hello":     {"run:c"},
		"C Hello Big": {"run:c"},
		"cl hello":    {},
		"gh hello":    {},
	} {
		if got, _ := p.GetInitialResultSet(strings.Fields(terms)); !slices.Equal(got, want) {
			t.Errorf("%q: %v, want %v", terms, got, want)
		}
	}
	var cmd, query string
	old := startShell
	startShell = func(_, c, q string) error { cmd, query = c, q; return nil }
	t.Cleanup(func() { startShell = old })
	for terms, want := range map[string]string{"c hello  world $(id)": "hello world $(id)", "c": "", "cl hello": "", "gh x": ""} {
		query = "unset"
		id := "run:c"
		if strings.HasPrefix(terms, "gh") {
			id = "run:gh"
		}
		if err := p.ActivateResult(id, strings.Fields(terms), 0); err != nil || query != want {
			t.Errorf("%q: %v, query %q, want %q", terms, err, query, want)
		}
	}
	if err := p.ActivateResult("run:c", []string{"c", "hi"}, 0); err != nil || cmd != "claude --model sonnet {q}" {
		t.Fatalf("command: %v %q", err, cmd)
	}
}

// The text typed after the alias reaches the command as one quoted word, never as shell code.
func TestStartShellQuery(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	if err := startShell("/opt/dot", `printf '%s|' {q} > `+out, `a b; echo pwned $(id)`); err != nil {
		t.Fatal(err)
	}
	var b []byte
	for i := 0; i < 100 && len(b) == 0; i++ {
		time.Sleep(20 * time.Millisecond)
		b, _ = os.ReadFile(out)
	}
	if string(b) != "a b; echo pwned $(id)|" {
		t.Fatalf("got %q", b)
	}
}

func TestLauncherAliasCommands(t *testing.T) {
	env, out, errb := testEnv(t)
	dir := t.TempDir()
	t.Setenv("DOTFILES_DEPLOY", dir)
	run := func(code int, args ...string) string {
		t.Helper()
		out.Reset()
		errb.Reset()
		if got := execute(env, append([]string{"launcher"}, args...)); got != code {
			t.Fatalf("%v: code %d, want %d, stderr %q", args, got, code, errb)
		}
		return out.String()
	}
	file := filepath.Join(dir, launcherFile)
	if err := os.WriteFile(file, []byte("# my aliases\ngh xdg-open https://github.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Flags after the alias belong to the command.
	run(0, "add", "up", "dot", "pull", "-p", "x")
	run(0, "add", "n", "notify-send 'a  b' && true")
	if got := run(0, "list"); got != "gh\txdg-open https://github.com\nup\tdot pull -p x\nn\tnotify-send 'a  b' && true\n" {
		t.Fatalf("list: %q", got)
	}
	if got := run(0, "get", "up"); got != "dot pull -p x\n" {
		t.Fatalf("get: %q", got)
	}
	run(2, "add", "up", "other")
	run(2, "add", "bad name", "x")
	run(2, "add", "-x", "x")
	run(2, "add", "alone")
	run(0, "update", "up", "dot", "push")
	run(2, "update", "nope", "x")
	run(2, "get", "nope")
	run(0, "rm", "gh")
	run(2, "rm", "gh")
	b, _ := os.ReadFile(file)
	if want := "# my aliases\nup dot push\nn notify-send 'a  b' && true\n"; string(b) != want {
		t.Fatalf("file: %q, want %q", b, want)
	}
	run(2, "frobnicate")
}

func TestLauncherInstallUninstall(t *testing.T) {
	env, _, errb := testEnv(t)
	t.Setenv("XDG_DATA_HOME", "")
	sys := t.TempDir()
	oldDir, oldSelf, oldInst, oldRm, oldReload := launcherSystemDir, launcherSelf, installSystemFile, removeSystemFile, reloadBus
	reloadBus = func(*Env) {}
	launcherSystemDir = sys
	launcherSelf = func() (string, error) { return "/opt/dot", nil }
	installSystemFile = func(_ *Env, dest string, data []byte) error { return os.WriteFile(dest, data, 0o644) }
	removeSystemFile = func(_ *Env, dest string) error {
		if err := os.Remove(dest); !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	t.Cleanup(func() {
		launcherSystemDir, launcherSelf, installSystemFile, removeSystemFile, reloadBus = oldDir, oldSelf, oldInst, oldRm, oldReload
	})

	if code := execute(env, []string{"launcher", "--install"}); code != 0 {
		t.Fatalf("install: code %d, stderr %q", code, errb)
	}
	desktop, service, ini := launcherFiles(env)
	for path, want := range map[string]string{
		ini:     "BusName=io.github.fmatsos.dot.SearchProvider\n",
		service: "Exec=/opt/dot launcher\n",
		desktop: "Terminal=true\n",
	} {
		b, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(b), want) {
			t.Fatalf("%s: %v, %q", path, err, b)
		}
	}
	if code := execute(env, []string{"launcher", "--install", "--uninstall"}); code != 2 {
		t.Fatalf("--install --uninstall: code %d", code)
	}
	if code := execute(env, []string{"launcher", "--uninstall"}); code != 0 {
		t.Fatalf("uninstall: code %d, stderr %q", code, errb)
	}
	for _, f := range []string{desktop, service, ini} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Fatalf("%s remained (%v)", f, err)
		}
	}
	// A second uninstall finds nothing and still succeeds; a `go run` binary is refused.
	if code := execute(env, []string{"launcher", "--uninstall"}); code != 0 {
		t.Fatalf("second uninstall: code %d", code)
	}
	launcherSelf = func() (string, error) { return "/tmp/go-build123/b001/exe/dot", nil }
	if code := execute(env, []string{"launcher", "--install"}); code != 1 {
		t.Fatalf("go run: code %d, stderr %q", code, errb)
	}
	for _, f := range []string{desktop, service, ini} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Fatalf("%s written despite the refusal (%v)", f, err)
		}
	}
}
