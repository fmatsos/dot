package link

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fmatsos/dot/internal/registry"
)

var clock = func() time.Time { return time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC) }

func write(t *testing.T, p, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// fixture builds a temp HOME and a profile dir with home/ files and bin/ scripts.
func fixture(t *testing.T) (home, dir string) {
	t.Helper()
	root := t.TempDir()
	home = filepath.Join(root, "home")
	dir = filepath.Join(root, "profile")
	must(t, os.MkdirAll(home, 0o755))
	t.Setenv("HOME", home)
	write(t, dir+"/home/.zshrc", "zsh\n", 0o644)
	write(t, dir+"/home/.config/git/config", "git\n", 0o644)
	write(t, dir+"/bin/dot-x", "#!/bin/sh\n", 0o755)
	write(t, dir+"/bin/notexec", "data\n", 0o644)
	write(t, dir+"/bin/sub/deep", "#!/bin/sh\n", 0o755)
	return
}

func TestPlan(t *testing.T) {
	home, dir := fixture(t)
	got, err := Plan(dir, home)
	must(t, err)
	var dsts []string
	for _, k := range got {
		dsts = append(dsts, Tilde(home, k.Dst))
	}
	want := "~/.config/git/config ~/.local/bin/dot-x ~/.zshrc"
	if strings.Join(dsts, " ") != want {
		t.Fatalf("plan = %v, want %s", dsts, want)
	}
	if got[0].Src != dir+"/home/.config/git/config" {
		t.Fatalf("src = %s", got[0].Src)
	}
}

func TestApply(t *testing.T) {
	type check func(t *testing.T, home, dir, out, errOut string)
	tests := []struct {
		name  string
		setup func(t *testing.T, home, dir string)
		dry   bool
		check check
	}{
		{"fresh links, file by file", nil, false, func(t *testing.T, home, dir, out, _ string) {
			for _, rel := range []string{".zshrc", ".config/git/config", ".local/bin/dot-x"} {
				if _, err := os.Readlink(filepath.Join(home, rel)); err != nil {
					t.Errorf("%s not a link: %v", rel, err)
				}
			}
			if fi, _ := os.Lstat(filepath.Join(home, ".config")); fi.Mode()&os.ModeSymlink != 0 {
				t.Error("directory linked as a whole")
			}
			if _, err := os.Lstat(filepath.Join(home, ".local/bin/notexec")); err == nil {
				t.Error("non executable linked")
			}
			if _, err := os.Lstat(filepath.Join(home, ".local/bin/deep")); err == nil {
				t.Error("bin/ depth 2 linked")
			}
			if !strings.Contains(out, "  lien : ~/.zshrc\n") || strings.Contains(out, "sauvegarde") {
				t.Errorf("out = %q", out)
			}
		}},
		{"already linked is silent", func(t *testing.T, home, dir string) {
			must(t, os.Symlink(dir+"/home/.zshrc", home+"/.zshrc"))
		}, false, func(t *testing.T, home, dir, out, _ string) {
			if strings.Contains(out, ".zshrc") {
				t.Errorf("out = %q", out)
			}
		}},
		{"different file is backed up with message", func(t *testing.T, home, dir string) {
			write(t, home+"/.zshrc", "mine\n", 0o644)
		}, false, func(t *testing.T, home, dir, out, _ string) {
			b := filepath.Join(home, ".local/state/dotfiles/backup/20261006-120000")
			if got, _ := os.ReadFile(b + "/.zshrc"); string(got) != "mine\n" {
				t.Errorf("backup = %q", got)
			}
			if !strings.Contains(out, "  sauvegarde : ~/.zshrc → ~/.local/state/dotfiles/backup/20261006-120000/\n  lien : ~/.zshrc\n") {
				t.Errorf("out = %q", out)
			}
		}},
		{"identical content is moved silently", func(t *testing.T, home, dir string) {
			write(t, home+"/.zshrc", "zsh\n", 0o644)
		}, false, func(t *testing.T, home, dir, out, _ string) {
			if strings.Contains(out, "sauvegarde") {
				t.Errorf("out = %q", out)
			}
			if _, err := os.Stat(home + "/.local/state/dotfiles/backup/20261006-120000/.zshrc"); err != nil {
				t.Error("not moved to backup")
			}
		}},
		{"wrong link is backed up, nested dirs created, one backup dir", func(t *testing.T, home, dir string) {
			must(t, os.Symlink("/elsewhere", home+"/.zshrc"))
			write(t, home+"/.config/git/config", "old\n", 0o644)
		}, false, func(t *testing.T, home, dir, out, _ string) {
			b := home + "/.local/state/dotfiles/backup"
			if tgt, _ := os.Readlink(b + "/20261006-120000/.zshrc"); tgt != "/elsewhere" {
				t.Errorf("link not preserved: %q", tgt)
			}
			if _, err := os.Stat(b + "/20261006-120000/.config/git/config"); err != nil {
				t.Error(err)
			}
			if es, _ := os.ReadDir(b); len(es) != 1 {
				t.Errorf("%d backup dirs", len(es))
			}
		}},
		{"real directory is ignored", func(t *testing.T, home, dir string) {
			must(t, os.MkdirAll(home+"/.zshrc", 0o755))
		}, false, func(t *testing.T, home, dir, out, errOut string) {
			if errOut != "  ignoré (dossier existant) : "+home+"/.zshrc\n" {
				t.Errorf("err = %q", errOut)
			}
			if fi, _ := os.Lstat(home + "/.zshrc"); !fi.IsDir() {
				t.Error("directory replaced")
			}
		}},
		{"dead bin link of the profile is removed, foreign ones kept", func(t *testing.T, home, dir string) {
			must(t, os.MkdirAll(home+"/.local/bin", 0o755))
			must(t, os.Symlink(dir+"/bin/gone", home+"/.local/bin/gone"))
			must(t, os.Symlink("/nowhere/x", home+"/.local/bin/foreign"))
		}, false, func(t *testing.T, home, dir, out, _ string) {
			if !strings.Contains(out, "  lien mort retiré : ~/.local/bin/gone\n") {
				t.Errorf("out = %q", out)
			}
			if _, err := os.Lstat(home + "/.local/bin/gone"); err == nil {
				t.Error("dead link kept")
			}
			if _, err := os.Lstat(home + "/.local/bin/foreign"); err != nil {
				t.Error("foreign link removed")
			}
		}},
		{"dry run writes nothing", func(t *testing.T, home, dir string) {
			write(t, home+"/.zshrc", "mine\n", 0o644)
			must(t, os.MkdirAll(home+"/.local/bin", 0o755))
			must(t, os.Symlink(dir+"/bin/gone", home+"/.local/bin/gone"))
		}, true, func(t *testing.T, home, dir, out, _ string) {
			b := "20261006-120000"
			for _, want := range []string{
				"  sauvegarde : ~/.zshrc → ~/.local/state/dotfiles/backup/" + b + "/\n",
				"  [dry] mkdir -p " + home + "/.local/state/dotfiles/backup/" + b + "\n",
				"  [dry] mv " + home + "/.zshrc " + home + "/.local/state/dotfiles/backup/" + b + "/.zshrc\n",
				"  [dry] ln -s " + dir + "/home/.zshrc " + home + "/.zshrc\n",
				"  lien mort retiré : ~/.local/bin/gone\n  [dry] rm -f " + home + "/.local/bin/gone\n",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q in %q", want, out)
				}
			}
			if got, _ := os.ReadFile(home + "/.zshrc"); string(got) != "mine\n" {
				t.Error("file touched")
			}
			if _, err := os.Lstat(home + "/.local/bin/gone"); err != nil {
				t.Error("dead link removed in dry run")
			}
			if _, err := os.Stat(home + "/.local/state"); err == nil {
				t.Error("backup dir created in dry run")
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home, dir := fixture(t)
			if tc.setup != nil {
				tc.setup(t, home, dir)
			}
			var out, errOut bytes.Buffer
			must(t, New(home, tc.dry, &out, &errOut, clock).Apply(dir))
			tc.check(t, home, dir, out.String(), errOut.String())
			if !tc.dry { // idempotent: a second run says nothing
				out.Reset()
				must(t, New(home, false, &out, &errOut, clock).Apply(dir))
				if strings.Contains(out.String(), "lien :") {
					t.Errorf("second run relinked: %q", out.String())
				}
			}
		})
	}
}

func TestApplyNoHome(t *testing.T) {
	home := t.TempDir()
	err := New(home, true, nil, nil, clock).Apply(filepath.Join(home, "nope"))
	var nh *NoHomeError
	if err == nil || !strings.HasPrefix(err.Error(), "rien à lier : ") || !strings.HasSuffix(err.Error(), "/home absent (dry run sans clone ?)") {
		t.Fatalf("err = %v (%T) %v", err, err, nh)
	}
}

func TestDriftAndDeadLinks(t *testing.T) {
	home, dir := fixture(t)
	must(t, New(home, false, nil, nil, clock).Apply(dir))
	if d := Drift(dir, home); len(d) != 0 {
		t.Fatalf("drift on clean install: %v", d)
	}
	must(t, os.Remove(home+"/.zshrc"))
	write(t, home+"/.zshrc", "rewritten\n", 0o644) // a tool replaced the link by a file
	must(t, os.Remove(home+"/.config/git/config"))
	got := Drift(dir, home)
	want := []string{
		"détaché (plus un lien, à réconcilier) : ~/.config/git/config",
		"détaché (plus un lien, à réconcilier) : ~/.zshrc",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("drift = %v", got)
	}
	must(t, os.Symlink("/nowhere", home+"/.local/bin/dead1"))
	must(t, os.Symlink("/nowhere", home+"/.local/bin/.hidden"))
	must(t, os.Symlink(dir+"/bin/dot-x", home+"/.local/bin/alive2"))
	if n := DeadLinks(home); n != 2 {
		t.Fatalf("dead links = %d, want 2", n)
	}
	if n := DeadLinks(t.TempDir()); n != 0 {
		t.Fatalf("no bin dir: %d", n)
	}
}

func TestConflicts(t *testing.T) {
	home := t.TempDir()
	mk := func(name string, files map[string]string) Profile {
		d := filepath.Join(t.TempDir(), name)
		for rel, mode := range files {
			m := os.FileMode(0o644)
			if mode == "x" {
				m = 0o755
			}
			write(t, filepath.Join(d, rel), "x", m)
		}
		return Profile{name, d}
	}
	tests := []struct {
		name  string
		a, b  map[string]string
		wants []string // Dst relative to home
	}{
		{"disjoint", map[string]string{"home/.a": ""}, map[string]string{"home/.b": ""}, nil},
		{"same home file", map[string]string{"home/.a": "", "home/.c": ""}, map[string]string{"home/.a": ""}, []string{".a"}},
		{"same bin name", map[string]string{"bin/dot-x": "x"}, map[string]string{"bin/dot-x": "x"}, []string{".local/bin/dot-x"}},
		{"two conflicts sorted", map[string]string{"home/.z": "", "home/.b": ""}, map[string]string{"home/.z": "", "home/.b": ""}, []string{".b", ".z"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pa, pb := mk("perso", tc.a), mk("acmecorp", tc.b)
			cs, err := Conflicts(home, []Profile{pa, pb})
			if len(tc.wants) == 0 {
				if err != nil || cs != nil {
					t.Fatalf("got %v, %v", cs, err)
				}
				return
			}
			if err == nil || len(cs) != len(tc.wants) {
				t.Fatalf("got %v, %v", cs, err)
			}
			for i, w := range tc.wants {
				c := cs[i]
				if c.Dst != filepath.Join(home, w) || strings.Join(c.Keys, ",") != "perso,acmecorp" {
					t.Errorf("conflict %d = %+v", i, c)
				}
				msg := err.Error()
				if !strings.Contains(msg, c.Srcs[0]) || !strings.Contains(msg, c.Srcs[1]) {
					t.Errorf("message lacks both sources: %s", msg)
				}
			}
			if !strings.HasPrefix(err.Error(), "installation refusée") {
				t.Errorf("msg = %s", err)
			}
		})
	}
}

func TestUnlink(t *testing.T) {
	home, dir := fixture(t)
	write(t, home+"/.zshrc", "mine\n", 0o644)
	must(t, New(home, false, nil, nil, clock).Apply(dir)) // backs up .zshrc
	must(t, os.Symlink(dir+"/bin/gone", home+"/.local/bin/gone"))
	must(t, os.Symlink("/nowhere/x", home+"/.local/bin/foreign"))
	write(t, home+"/.config/other", "keep\n", 0o644)
	var out bytes.Buffer
	must(t, Unlink(dir, home, &out))
	for _, rel := range []string{".zshrc", ".config/git/config", ".local/bin/dot-x", ".local/bin/gone"} {
		if _, err := os.Lstat(filepath.Join(home, rel)); err == nil {
			t.Errorf("%s still there", rel)
		}
	}
	if _, err := os.Lstat(home + "/.local/bin/foreign"); err != nil {
		t.Error("foreign link removed")
	}
	if _, err := os.Stat(home + "/.config/git"); err == nil {
		t.Error("empty parent .config/git kept")
	}
	if _, err := os.Stat(home + "/.config/other"); err != nil {
		t.Error("non-empty parent or sibling removed")
	}
	if _, err := os.Stat(home + "/.local/state/dotfiles/backup/20261006-120000/.zshrc"); err != nil {
		t.Error("backup removed")
	}
	if _, err := os.Stat(home); err != nil {
		t.Error("home removed")
	}
	if !strings.Contains(out.String(), "  lien retiré : ~/.zshrc\n") {
		t.Errorf("out = %q", out.String())
	}
}

func TestUnlinkEmptiesHomeSubtree(t *testing.T) {
	home, dir := fixture(t)
	must(t, New(home, false, nil, nil, clock).Apply(dir))
	must(t, Unlink(dir, home, nil))
	es, _ := os.ReadDir(home)
	if len(es) != 0 {
		t.Fatalf("home not empty: %v", es)
	}
}

// ~/.config is a symlink to a directory elsewhere: Unlink must leave that link alone.
func TestUnlinkKeepsDirectorySymlinkParents(t *testing.T) {
	root := t.TempDir()
	home, dir, ext := filepath.Join(root, "home"), filepath.Join(root, "prof"), filepath.Join(root, "ext")
	write(t, dir+"/home/.config/git/ignore", "x\n", 0o644)
	write(t, ext+"/keep.txt", "user data\n", 0o644)
	must(t, os.MkdirAll(home, 0o755))
	must(t, os.Symlink(ext, home+"/.config"))
	must(t, New(home, false, nil, nil, clock).Apply(dir))
	must(t, Unlink(dir, home, nil))
	if fi, err := os.Lstat(home + "/.config"); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("~/.config symlink removed: %v", err)
	}
	if _, err := os.Stat(ext + "/keep.txt"); err != nil {
		t.Fatal("data behind the symlink lost")
	}
}

// ~/.config/foo is a symlink into the profile: the file is already "linked" through it and must
// neither be moved to the backup nor reported.
func TestApplyThroughDirectoryLinkKeepsProfileSource(t *testing.T) {
	root := t.TempDir()
	home, dir := filepath.Join(root, "home"), filepath.Join(root, "prof")
	src := dir + "/home/.config/foo/bar"
	write(t, src, "source of truth\n", 0o644)
	must(t, os.MkdirAll(home+"/.config", 0o755))
	must(t, os.Symlink(dir+"/home/.config/foo", home+"/.config/foo"))
	var out, errOut bytes.Buffer
	must(t, New(home, false, &out, &errOut, clock).Apply(dir))
	if b, err := os.ReadFile(src); err != nil || string(b) != "source of truth\n" {
		t.Fatalf("profile source lost: %q, %v", b, err)
	}
	if _, err := os.Stat(home + "/.local/state/dotfiles/backup"); err == nil {
		t.Error("backup made for a file already linked")
	}
	if strings.Contains(out.String(), ".config/foo/bar") || strings.Contains(errOut.String(), ".config/foo/bar") {
		t.Errorf("message for a file already linked: %q %q", out.String(), errOut.String())
	}
}

func linkTarget(p string) string { t, _ := os.Readlink(p); return t }

// A file deleted from the profile after the install leaves its link behind: Unlink still finds it
// in the directories of the planned links, and only removes links into the profile.
func TestUnlinkRemovesOrphanLinks(t *testing.T) {
	home, dir := fixture(t)
	must(t, New(home, false, nil, nil, clock).Apply(dir))
	must(t, os.Symlink(dir+"/home/.old", home+"/.old"))                       // source never existed
	must(t, os.Symlink(dir+"/home/.config/git/old", home+"/.config/git/old")) // orphan in a planned directory
	must(t, os.Symlink(dir+"-other/home/x", home+"/.config/git/lookalike"))   // prefix without the trailing slash
	must(t, os.Symlink("/nowhere/x", home+"/.config/git/foreign"))
	write(t, home+"/.config/git/regular", "mine\n", 0o644)
	must(t, Unlink(dir, home, nil))
	for _, rel := range []string{".old", ".config/git/old", ".zshrc", ".config/git/config"} {
		if _, err := os.Lstat(filepath.Join(home, rel)); err == nil {
			t.Errorf("%s still there", rel)
		}
	}
	for _, rel := range []string{".config/git/lookalike", ".config/git/foreign", ".config/git/regular"} {
		if _, err := os.Lstat(filepath.Join(home, rel)); err != nil {
			t.Errorf("%s removed: %v", rel, err)
		}
	}
}

// The source is removed from the clone before the uninstall, like a file dropped by a pull.
func TestUnlinkAfterSourceDeleted(t *testing.T) {
	home, dir := fixture(t)
	write(t, dir+"/home/.bashrc", "bash\n", 0o644) // keeps ~ in the plan once .zshrc is gone
	must(t, New(home, false, nil, nil, clock).Apply(dir))
	must(t, os.Remove(dir+"/home/.zshrc"))
	if _, err := os.Stat(home + "/.zshrc"); err == nil {
		t.Fatal("link not dead")
	}
	var out bytes.Buffer
	must(t, Unlink(dir, home, &out))
	if _, err := os.Lstat(home + "/.zshrc"); err == nil {
		t.Error("dead link left in ~")
	}
	if !strings.Contains(out.String(), "  lien retiré : ~/.zshrc\n") {
		t.Errorf("out = %q", out.String())
	}
}

func moduleProfile(t *testing.T, name string) Profile {
	t.Helper()
	d := filepath.Join(t.TempDir(), name)
	write(t, d+"/home/.claude/settings.base.json", "{}\n", 0o644)
	write(t, d+"/home/.config/mcp/servers.json", "{}\n", 0o644)
	write(t, d+"/home/."+name+"rc", "x\n", 0o644)
	return Profile{name, d}
}

func TestModuleSourcesNeverConflictBetweenProfiles(t *testing.T) {
	home := t.TempDir()
	a, b := moduleProfile(t, "a"), moduleProfile(t, "b")
	if cs, err := Conflicts(home, []Profile{a, b}); err != nil || cs != nil {
		t.Fatalf("module sources conflict: %v, %v", cs, err)
	}
	write(t, b.Dir+"/home/.arc", "x\n", 0o644)
	cs, err := Conflicts(home, []Profile{a, b})
	if err == nil || len(cs) != 1 || cs[0].Dst != filepath.Join(home, ".arc") {
		t.Fatalf("other files still conflict: %v, %v", cs, err)
	}
	plan, _ := PlanFor(a.Dir, home, true)
	for _, k := range plan {
		for _, rel := range ModuleSources {
			if k.Dst == filepath.Join(home, rel) {
				t.Errorf("%s planned in multi mode", rel)
			}
		}
	}
	if plan, _ = Plan(a.Dir, home); len(plan) != 3 {
		t.Errorf("single plan = %d links ; want 3", len(plan))
	}
}

func TestApplyModuleSources(t *testing.T) {
	setup := func(t *testing.T) (home string, a, b Profile) {
		home = filepath.Join(t.TempDir(), "home")
		must(t, os.MkdirAll(home, 0o755))
		return home, moduleProfile(t, "a"), moduleProfile(t, "b")
	}
	t.Run("one profile links them", func(t *testing.T) {
		home, a, _ := setup(t)
		lk := New(home, false, nil, nil, clock)
		lk.Registered = []string{a.Dir}
		must(t, lk.Apply(a.Dir))
		for _, rel := range ModuleSources {
			if linkTarget(filepath.Join(home, rel)) != a.Dir+"/home/"+rel {
				t.Errorf("%s not linked", rel)
			}
		}
	})
	t.Run("two profiles link nothing and drop a link left by one", func(t *testing.T) {
		home, a, b := setup(t)
		first := New(home, false, nil, nil, clock)
		first.Registered = []string{a.Dir}
		must(t, first.Apply(a.Dir))
		var out bytes.Buffer
		lk := New(home, false, &out, nil, clock)
		lk.Registered = []string{a.Dir, b.Dir}
		must(t, lk.Apply(b.Dir))
		for _, rel := range ModuleSources {
			if _, err := os.Lstat(filepath.Join(home, rel)); err == nil {
				t.Errorf("%s still linked", rel)
			}
			if !strings.Contains(out.String(), "  lien retiré : ~/"+rel+"\n") {
				t.Errorf("no message for %s: %q", rel, out.String())
			}
		}
		if linkTarget(home+"/.brc") != b.Dir+"/home/.brc" {
			t.Error("other files not linked")
		}
		if _, err := os.Stat(home + "/.claude"); err != nil {
			t.Error("parent directory removed")
		}
	})
	t.Run("a foreign link or a regular file stays", func(t *testing.T) {
		home, a, b := setup(t)
		must(t, os.MkdirAll(home+"/.claude", 0o755))
		must(t, os.Symlink("/nowhere/settings", home+"/.claude/settings.base.json"))
		write(t, home+"/.config/mcp/servers.json", "mine\n", 0o644)
		lk := New(home, false, nil, nil, clock)
		lk.Registered = []string{a.Dir, b.Dir}
		must(t, lk.Apply(a.Dir))
		if linkTarget(home+"/.claude/settings.base.json") != "/nowhere/settings" {
			t.Error("foreign link removed")
		}
		if body, _ := os.ReadFile(home + "/.config/mcp/servers.json"); string(body) != "mine\n" {
			t.Error("regular file touched")
		}
	})
	t.Run("dry run removes nothing", func(t *testing.T) {
		home, a, b := setup(t)
		first := New(home, false, nil, nil, clock)
		first.Registered = []string{a.Dir}
		must(t, first.Apply(a.Dir))
		var out bytes.Buffer
		lk := New(home, true, &out, nil, clock)
		lk.Registered = []string{a.Dir, b.Dir}
		must(t, lk.Apply(b.Dir))
		if linkTarget(home+"/.claude/settings.base.json") == "" || !strings.Contains(out.String(), "[dry] rm -f") {
			t.Errorf("dry run: %q", out.String())
		}
	})
}

func TestDetachedIgnoresModuleSourcesWithSeveralProfiles(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	a := moduleProfile(t, "a")
	if d := Detached(a.Dir, home); len(d) != 3 {
		t.Fatalf("single profile: %v", d)
	}
	r := &registry.Registry{}
	must(t, r.Add("a", "/x/a"))
	must(t, r.Add("b", "/x/b"))
	must(t, os.MkdirAll(home+"/.dot", 0o755))
	must(t, r.Save(home+"/.dot/profiles.json"))
	d := Detached(a.Dir, home)
	if len(d) != 1 || d[0] != "~/.arc" {
		t.Fatalf("several profiles: %v", d)
	}
}
