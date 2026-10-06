package install

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fmatsos/dot/internal/link"
	"github.com/fmatsos/dot/internal/registry"
)

// sandbox gives a temporary HOME and git config, so no test touches the real ones.
func sandbox(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(filepath.Dir(home), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("DOTFILES_TOOLS", "")
	return home
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.com"}, args...)...)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v : %v\n%s", args, err, out)
	}
}

func manifestJSON(key, modules string) string {
	return fmt.Sprintf(`{"repo":"https://git.example.com/acmecorp/%[1]s.git","deploy":{"sparse":["home","bin",".githooks"]},`+
		`"profiles":{"%[1]s":"home/.%[1]s.gitconfig"},"deployProfile":"%[1]s","modules":%[2]s}`, key, modules)
}

// remote writes files (plus a valid dot.json and git identity unless given) into a repository.
func remote(t *testing.T, key string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), key)
	all := map[string]string{
		"dot.json":                    manifestJSON(key, "[]"),
		"home/." + key + ".gitconfig": "[user]\n\tname = " + key + "\n\temail = " + key + "@example.com\n",
		"home/." + key + "rc":         "rc of " + key + "\n",
		"bin/" + key + "-tool":        "#!/bin/sh\n",
	}
	for k, v := range files {
		all[k] = v
	}
	for name, content := range all {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if strings.HasPrefix(name, "bin/") {
			mode = 0o755
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "uploadpack.allowFilter", "true")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "fixture")
	return dir
}

func newInstaller(home string, dry bool) (*Installer, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	in := New(home, dry, &out, &errOut)
	in.Now = func() time.Time { return time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC) }
	return in, &out, &errOut
}

func keysOf(t *testing.T, home string) []string {
	t.Helper()
	r, err := registry.Load(filepath.Join(home, ".dot", "profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	return r.Keys()
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func TestDeriveKey(t *testing.T) {
	for url, want := range map[string]string{
		"https://git.example.com/acmecorp/dots.git": "dots",
		"git@git.example.com:acmecorp/my-dots.git":  "my-dots",
		"/tmp/some/place/profile/":                  "profile",
		"ssh://git@git.example.com/team/dots":       "dots",
	} {
		if got, err := DeriveKey(url); err != nil || got != want {
			t.Errorf("DeriveKey(%q) = %q, %v ; want %q", url, got, err, want)
		}
	}
	for _, url := range []string{"", "https://git.example.com/a/.git", "x/.."} {
		if got, err := DeriveKey(url); err == nil {
			t.Errorf("DeriveKey(%q) = %q ; want an error", url, got)
		}
	}
}

func TestAddInstallsInOrder(t *testing.T) {
	home := sandbox(t)
	url := remote(t, "a", nil)
	in, out, _ := newInstaller(home, false)
	var seen []string
	in.Tools = func(req ToolsRequest) error {
		// Tools run after the links and the global include, before the module and the final ok.
		seen = append(seen, fmt.Sprintf("links=%v include=%v skip=%v dry=%v profiles=%d",
			exists(filepath.Join(home, ".arc")), strings.Contains(globalInclude(t), includePath), req.Skip, req.Dry, len(req.Profiles)))
		if got := req.Profiles[0]; got.Key != "a" || got.Dir != filepath.Join(home, ".dot", "a") || got.Manifest == nil {
			t.Errorf("tool profile = %+v", got)
		}
		return nil
	}
	t.Setenv("DOTFILES_TOOLS", "0")
	if err := in.Add(url, "a"); err != nil {
		t.Fatal(err)
	}
	if want := "links=true include=true skip=true dry=false profiles=1"; len(seen) != 1 || seen[0] != want {
		t.Errorf("tools saw %v ; want %q", seen, want)
	}
	if got := keysOf(t, home); len(got) != 1 || got[0] != "a" {
		t.Errorf("registry keys = %v", got)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if lines[0] == "" || lines[len(lines)-1] != "ok" || !strings.HasPrefix(lines[0], "clone partiel → ") {
		t.Errorf("output = %q", out.String())
	}
	dir := filepath.Join(home, ".dot", "a")
	if got, _ := os.Readlink(filepath.Join(home, ".arc")); got != filepath.Join(dir, "home", ".arc") {
		t.Errorf("link = %q", got)
	}
	if got := gitCfg(t, dir, "core.hooksPath"); got != ".githooks" {
		t.Errorf("core.hooksPath = %q", got)
	}
	if got := gitCfg(t, dir, "user.email"); got != "a@example.com" {
		t.Errorf("user.email = %q", got)
	}
	// A second run is idempotent: no link, no include.path twice.
	out.Reset()
	if err := in.Reinstall([]Profile{{"a", dir}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "lien :") {
		t.Errorf("second run linked again:\n%s", out.String())
	}
	if n := strings.Count(globalInclude(t), includePath); n != 1 {
		t.Errorf("include.path appears %d times", n)
	}
}

func globalInclude(t *testing.T) string {
	t.Helper()
	out, _ := exec.Command("git", "config", "--global", "--get-all", "include.path").Output()
	return string(out)
}

func gitCfg(t *testing.T, dir, key string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "config", "--local", key).Output()
	if err != nil {
		t.Fatalf("git config %s : %v", key, err)
	}
	return strings.TrimSpace(string(out))
}

func TestAddFailureCleansUp(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"bad manifest": {"dot.json": "{}"},
		"no identity":  {"home/.bad.gitconfig": "[user]\n\tname = x\n"},
	} {
		t.Run(name, func(t *testing.T) {
			home := sandbox(t)
			url := remote(t, "bad", files)
			in, _, _ := newInstaller(home, false)
			err := in.Add(url, "bad")
			if err == nil {
				t.Fatal("Add succeeded")
			}
			if exists(filepath.Join(home, ".dot", "bad")) || exists(filepath.Join(home, ".dot", "profiles.json")) || exists(filepath.Join(home, ".arc")) {
				t.Errorf("leftovers after %v", err)
			}
		})
	}
	t.Run("unreachable remote", func(t *testing.T) {
		home := sandbox(t)
		in, _, _ := newInstaller(home, false)
		if err := in.Add(filepath.Join(t.TempDir(), "nope"), "nope"); err == nil {
			t.Fatal("Add succeeded")
		}
		if exists(filepath.Join(home, ".dot")) && exists(filepath.Join(home, ".dot", "nope")) {
			t.Error("half clone left behind")
		}
	})
	t.Run("kept clone survives", func(t *testing.T) {
		home := sandbox(t)
		url := remote(t, "bad", map[string]string{"dot.json": "{}"})
		dir := filepath.Join(home, ".dot", "bad")
		in, _, _ := newInstaller(home, false)
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(filepath.Join(dir, "mine"), nil, 0o644)
		if err := in.Add(url, "bad"); err == nil || !exists(filepath.Join(dir, "mine")) {
			t.Errorf("a directory this call did not create must stay: %v", err)
		}
	})
}

func TestAddRefusals(t *testing.T) {
	home := sandbox(t)
	a := remote(t, "a", nil)
	in, _, _ := newInstaller(home, false)
	if err := in.Add(a, "a"); err != nil {
		t.Fatal(err)
	}
	other := remote(t, "b", nil)
	if err := in.Add(other, "a"); err == nil || !strings.Contains(err.Error(), "déjà inscrit avec un autre dépôt") {
		t.Errorf("same key, other repo: %v", err)
	}
	if err := in.Add(a, "bad key"); err == nil {
		t.Error("invalid key accepted")
	}
	if err := in.Add(a, "a"); err != nil { // same URL: reinstall
		t.Errorf("same key and repo: %v", err)
	}
}

func TestConflictWritesNothing(t *testing.T) {
	home := sandbox(t)
	in, _, _ := newInstaller(home, false)
	if err := in.Add(remote(t, "a", map[string]string{"home/.shared": "a\n"}), "a"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(home, ".dot", "profiles.json"))
	err := in.Add(remote(t, "b", map[string]string{"home/.shared": "b\n"}), "b")
	var ce *link.ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v ; want a ConflictError", err)
	}
	for _, want := range []string{filepath.Join(home, ".dot", "a", "home", ".shared"), filepath.Join(home, ".dot", "b", "home", ".shared")} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message lacks %s:\n%v", want, err)
		}
	}
	after, _ := os.ReadFile(filepath.Join(home, ".dot", "profiles.json"))
	if !bytes.Equal(before, after) || exists(filepath.Join(home, ".dot", "b")) || exists(filepath.Join(home, ".brc")) {
		t.Error("a refused install wrote something")
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	home := sandbox(t)
	url := remote(t, "a", nil)
	in, out, _ := newInstaller(home, true)
	tools := 0
	in.Tools = func(ToolsRequest) error { tools++; return nil }
	if err := in.Add(url, "a"); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Errorf("dry run created %v", entries)
	}
	if !strings.Contains(out.String(), "rien à lier") || !strings.Contains(out.String(), "[dry] git -C ") || tools != 0 {
		t.Errorf("output = %q, tools = %d", out.String(), tools)
	}
	// Dry run over a real clone: previews, still writes nothing and does not register a new profile.
	real, _, _ := newInstaller(home, false)
	if err := real.Add(url, "a"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(home, ".arc")); err != nil {
		t.Fatal(err)
	}
	b := remote(t, "b", nil)
	if err := in.Add(b, "b"); err != nil { // clone absent: preview only
		t.Fatal(err)
	}
	out.Reset()
	if err := in.Reinstall([]Profile{{"a", filepath.Join(home, ".dot", "a")}}); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(home, ".arc")) || len(keysOf(t, home)) != 1 || exists(filepath.Join(home, ".dot", "b")) ||
		!strings.HasSuffix(strings.TrimSpace(out.String()), "ok (dry run)") || !strings.Contains(out.String(), "[dry]") {
		t.Errorf("dry run over a clone wrote something:\n%s", out.String())
	}
	if tools != 1 {
		t.Errorf("tools called %d times in a dry run over a clone ; want 1 (they decide what Dry means)", tools)
	}
}

func TestPullContinuesAfterFailure(t *testing.T) {
	home := sandbox(t)
	a := remote(t, "a", nil)
	b := remote(t, "b", nil)
	in, out, errOut := newInstaller(home, false)
	for k, u := range map[string]string{"a": a, "b": b} {
		if err := in.Add(u, k); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(a, "home", ".anew"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, a, "add", "-A")
	gitIn(t, a, "commit", "-q", "-m", "more")
	if err := os.Rename(b, b+".away"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	batch := []Profile{{"a", filepath.Join(home, ".dot", "a")}, {"b", filepath.Join(home, ".dot", "b")}}
	err := in.Pull(batch)
	if !errors.Is(err, ErrReported) {
		t.Fatalf("err = %v ; want ErrReported", err)
	}
	if !exists(filepath.Join(home, ".anew")) {
		t.Error("profile a was not updated after b failed")
	}
	if !strings.Contains(errOut.String(), "pull b") || !strings.Contains(errOut.String(), "échec : b") || !strings.Contains(out.String(), "==> a") {
		t.Errorf("stderr = %q stdout = %q", errOut.String(), out.String())
	}
}

func TestUninstall(t *testing.T) {
	home := sandbox(t)
	in, out, _ := newInstaller(home, false)
	for _, k := range []string{"a", "b"} {
		if err := in.Add(remote(t, k, nil), k); err != nil {
			t.Fatal(err)
		}
	}
	if err := in.Uninstall("a", false, false); err == nil || !strings.Contains(err.Error(), "profil par défaut") {
		t.Errorf("uninstalling the default with another profile: %v", err)
	}
	if err := in.Uninstall("b", false, false); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(home, ".brc")) || !exists(filepath.Join(home, ".arc")) || !exists(filepath.Join(home, ".dot", "b", "home")) {
		t.Error("uninstall removed the wrong links or the clone")
	}
	if !strings.Contains(out.String(), "clone gardé") {
		t.Errorf("output = %q", out.String())
	}
	if err := in.Uninstall("b", false, false); err == nil || !strings.Contains(err.Error(), "profil inconnu : b") {
		t.Errorf("second uninstall: %v", err)
	}
	// Purge refusals leave the profile installed.
	dir := filepath.Join(home, ".dot", "a")
	if err := os.WriteFile(filepath.Join(dir, "wip"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := in.Uninstall("a", true, false); err == nil || !strings.Contains(err.Error(), "non commités") || !exists(filepath.Join(home, ".arc")) {
		t.Errorf("dirty purge: %v", err)
	}
	if err := in.Uninstall("a", true, true); err != nil || exists(dir) || exists(filepath.Join(home, ".arc")) {
		t.Errorf("forced purge: %v", err)
	}
	if len(keysOf(t, home)) != 0 {
		t.Error("registry not empty")
	}
}

var moduleFiles = map[string]string{
	"home/.claude/settings.base.json": "{}\n",
	"home/.config/mcp/servers.json":   "{}\n",
}

// Two profiles providing the module sources is the normal multi-profile case: the install works
// and the files stay per-profile inputs, unlinked.
func TestModuleSourcesAcrossProfiles(t *testing.T) {
	home := sandbox(t)
	in, out, _ := newInstaller(home, false)
	settingsLink := filepath.Join(home, ".claude", "settings.base.json")
	serversLink := filepath.Join(home, ".config", "mcp", "servers.json")
	if err := in.Add(remote(t, "a", moduleFiles), "a"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{settingsLink, serversLink} { // one profile: linked as before
		if got, _ := os.Readlink(p); got == "" {
			t.Fatalf("%s not linked with a single profile", p)
		}
	}
	out.Reset()
	if err := in.Add(remote(t, "b", moduleFiles), "b"); err != nil {
		t.Fatalf("two profiles with the module sources: %v", err)
	}
	for _, p := range []string{settingsLink, serversLink} {
		if exists(p) {
			t.Errorf("%s still linked with two profiles", p)
		}
	}
	if n := strings.Count(out.String(), "lien retiré"); n != 2 {
		t.Errorf("%d \"lien retiré\" lines ; want 2:\n%s", n, out.String())
	}
	if !exists(filepath.Join(home, ".arc")) || !exists(filepath.Join(home, ".brc")) {
		t.Error("other files not linked")
	}
	// A reinstall stays unlinked and quiet.
	out.Reset()
	if err := in.Reinstall([]Profile{{"a", filepath.Join(home, ".dot", "a")}}); err != nil || exists(settingsLink) || strings.Contains(out.String(), "lien retiré") {
		t.Errorf("reinstall: %v\n%s", err, out.String())
	}
	// Back to one profile: nothing is linked on its own, the message says so; install links again.
	out.Reset()
	if err := in.Uninstall("b", false, false); err != nil {
		t.Fatal(err)
	}
	if exists(settingsLink) || exists(serversLink) || strings.Count(out.String(), "dot install") != 2 {
		t.Errorf("uninstall of one of two:\n%s", out.String())
	}
	if err := in.Reinstall([]Profile{{"a", filepath.Join(home, ".dot", "a")}}); err != nil || !exists(settingsLink) || !exists(serversLink) {
		t.Errorf("reinstall of the last profile: %v", err)
	}
}

// defaultBranchRemote is a profile remote whose default branch is branch.
func defaultBranchRemote(t *testing.T, key, branch string) string {
	t.Helper()
	dir := remote(t, key, nil)
	gitIn(t, dir, "branch", "-m", branch)
	return dir
}

func TestAddResolvesDefaultBranch(t *testing.T) {
	for _, branch := range []string{"master", "trunk", "feature/x"} {
		t.Run(branch, func(t *testing.T) {
			home := sandbox(t)
			in, _, _ := newInstaller(home, false)
			if err := in.Add(defaultBranchRemote(t, "a", branch), "a"); err != nil {
				t.Fatal(err)
			}
			clone := filepath.Join(home, ".dot", "a")
			out, err := gitOut(clone, "symbolic-ref", "--short", "HEAD")
			if err != nil || strings.TrimSpace(string(out)) != branch {
				t.Errorf("HEAD = %q, %v ; want %s", out, err, branch)
			}
			if !exists(filepath.Join(home, ".arc")) {
				t.Error("profile not linked")
			}
		})
	}
	t.Run("dry run previews the real branch", func(t *testing.T) {
		home := sandbox(t)
		in, out, _ := newInstaller(home, true)
		if err := in.Add(defaultBranchRemote(t, "a", "trunk"), "a"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "origin trunk") || !strings.Contains(out.String(), "init -q -b trunk") {
			t.Errorf("out = %s", out.String())
		}
	})
	t.Run("detached HEAD keeps main", func(t *testing.T) {
		home := sandbox(t)
		in, _, _ := newInstaller(home, false)
		url := remote(t, "a", nil)
		gitIn(t, url, "checkout", "-q", "--detach")
		if err := in.Add(url, "a"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestAddWithoutDefaultBranchCleansUp(t *testing.T) {
	t.Run("empty repository", func(t *testing.T) {
		home := sandbox(t)
		in, _, _ := newInstaller(home, false)
		url := filepath.Join(t.TempDir(), "empty")
		gitIn(t, t.TempDir(), "init", "-q", "--bare", "-b", "main", url)
		err := in.Add(url, "empty")
		if err == nil || !strings.Contains(err.Error(), "branche par défaut introuvable") {
			t.Fatalf("err = %v", err)
		}
		if exists(filepath.Join(home, ".dot", "empty")) || exists(filepath.Join(home, ".dot", "profiles.json")) {
			t.Error("leftovers after an empty remote")
		}
	})
	t.Run("detached HEAD without main", func(t *testing.T) {
		home := sandbox(t)
		in, _, _ := newInstaller(home, false)
		url := defaultBranchRemote(t, "a", "trunk")
		gitIn(t, url, "checkout", "-q", "--detach")
		if err := in.Add(url, "a"); err == nil || exists(filepath.Join(home, ".dot", "a")) {
			t.Errorf("err = %v", err)
		}
	})
}
