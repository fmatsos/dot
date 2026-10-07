package repos

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fmatsos/dot/internal/clone"
)

type world struct {
	t                  *testing.T
	home, src, sandbox string
	base               string // src/git.example.com/team
}

func (w *world) git(args ...string) string {
	w.t.Helper()
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		w.t.Fatalf("git %v : %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// newWorld mirrors the black-box fixture: three clones from local bare remotes behind a URL
// rewrite, a linked worktree, a symlink, a plain directory and one sandbox repository.
func newWorld(t *testing.T) *world {
	t.Helper()
	s := t.TempDir()
	w := &world{t: t, home: filepath.Join(s, "home")}
	w.src, w.sandbox = filepath.Join(w.home, "source repos"), filepath.Join(w.home, "sandbox")
	t.Setenv("HOME", w.home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(s, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	w.git("config", "--global", "user.name", "Tester")
	w.git("config", "--global", "user.email", "tester@example.com")
	w.git("config", "--global", "url.file://"+s+"/remotes/.insteadOf", "https://git.example.com/")
	w.git("init", "-q", "-b", "main", s+"/seed")
	write(t, s+"/seed/marker", "fixture\n", 0o644)
	w.git("-C", s+"/seed", "add", "marker")
	w.git("-C", s+"/seed", "commit", "-qm", "fixture")
	w.base = filepath.Join(w.src, "git.example.com", "team")
	for _, name := range []string{"clean", "dirty", "ahead"} {
		w.git("clone", "-q", "--bare", s+"/seed", s+"/remotes/team/"+name+".git")
		if _, _, err := clone.Clone("https://git.example.com/team/"+name+".git", clone.Options{Root: w.src, Env: Env()}); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(w.home, ".config/git/profiles/perso.gitconfig"),
		"[user]\n  email = tester@example.com\n[dotfiles]\n  profile = perso\n[core]\n  hooksPath = ~/.config/git/hooks\n", 0o644)
	for _, name := range []string{"clean", "dirty"} {
		w.git("config", "--global", "--add", "includeIf.gitdir:"+w.base+"/"+name+"/.path",
			filepath.Join(w.home, ".config/git/profiles/perso.gitconfig"))
	}
	w.git("-C", w.base+"/dirty", "config", "user.email", "other@example.com")
	write(t, w.base+"/dirty/new file", "untracked\n", 0o644)
	write(t, w.base+"/ahead/marker", "fixture\nahead\n", 0o644)
	w.git("-C", w.base+"/ahead", "commit", "-qam", "ahead")
	w.git("-C", w.base+"/clean", "worktree", "add", "-qb", "side", w.base+"/clean.wt/side")
	w.git("-C", w.base+"/clean", "worktree", "add", "-qb", "linked", w.base+"/linked")
	if err := os.Symlink(w.base+"/clean", w.base+"/mirror"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(w.base+"/plain", 0o755); err != nil {
		t.Fatal(err)
	}
	w.git("init", "-q", w.sandbox+"/scratch")
	return w
}

func (w *world) lines(rows []Row) string { return strings.Join(Format(rows, false), "\n") }

func TestOverview(t *testing.T) {
	w := newWorld(t)
	out := w.lines(Collect(w.home, w.src, w.sandbox, true))
	if n := strings.Count(out, "\n") + 1; n != 4 {
		t.Fatalf("%d lignes :\n%s", n, out)
	}
	for _, want := range []string{
		"~/source repos/git.example.com/team/clean ", "✎0", "⇡0 ⇣0", "perso", "tester@example.com",
		"/dirty ", "✎1", "✗ email différent du profil perso", "/ahead ", "⇡1 ⇣0", "~/sandbox/scratch ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("sans %q :\n%s", want, out)
		}
	}
	for _, bad := range []string{"clean.wt", "linked", "plain", "mirror"} {
		if strings.Contains(out, "/"+bad) {
			t.Errorf("%q ne doit pas apparaître :\n%s", bad, out)
		}
	}
}

func TestProblemsAndHooksGuard(t *testing.T) {
	w := newWorld(t)
	rows, ok, err := Problems(w.home, w.src, w.sandbox)
	if err != nil || ok || len(rows) != 1 || !strings.Contains(rows[0], "/dirty ") {
		t.Fatalf("problèmes = %q, %v, %v", rows, ok, err)
	}
	w.git("-C", w.base+"/dirty", "config", "user.email", "tester@example.com")
	if rows, ok, _ := Problems(w.home, w.src, w.sandbox); !ok || len(rows) != 0 {
		t.Fatalf("après correction : %q", rows)
	}
	w.git("-C", w.base+"/clean", "checkout", "-q", "--detach")
	if out := w.lines(Collect(w.home, w.src, w.sandbox, true)); !strings.Contains(out, "détaché") {
		t.Fatalf("détaché absent :\n%s", out)
	}
	w.git("-C", w.base+"/clean", "config", "core.hooksPath", ".husky")
	rows, ok, _ = Problems(w.home, w.src, w.sandbox)
	if ok || len(rows) != 1 || !strings.Contains(rows[0], "✗ hooksPath local : garde-fou contourné") {
		t.Fatalf("hooksPath : %q", rows)
	}
	w.git("-C", w.base+"/clean", "config", "user.email", "other@example.com")
	rows, _, _ = Problems(w.home, w.src, w.sandbox)
	if len(rows) != 1 || !strings.Contains(rows[0], "✗ email différent du profil perso ; ✗ hooksPath local") {
		t.Fatalf("raisons combinées : %q", rows)
	}
	w.git("-C", w.base+"/clean", "config", "user.email", "tester@example.com")
	w.git("-C", w.base+"/clean", "config", "core.hooksPath", ".githooks")
	if _, ok, _ := Problems(w.home, w.src, w.sandbox); ok {
		t.Fatal(".githooks sans pre-push exécutable toléré")
	}
	for _, hook := range []string{"# dot guard push\nexit 0", "exit 0 # dot guard push", "echo dot guard push", "dotguard push"} {
		write(t, w.base+"/clean/.githooks/pre-push", "#!/bin/sh\n"+hook+"\n", 0o755)
		if _, ok, _ := Problems(w.home, w.src, w.sandbox); ok {
			t.Fatalf("pre-push %q toléré", hook)
		}
	}
	write(t, w.base+"/clean/.githooks/pre-push", "#!/bin/sh\nMODE=x dot guard push \"$@\" || exit 1\n", 0o755)
	if rows, ok, _ := Problems(w.home, w.src, w.sandbox); !ok {
		t.Fatalf("appel avec variable toléré : %q", rows)
	}
	write(t, w.base+"/clean/.githooks/pre-push", "#!/bin/sh\nexec dot guard push \"$@\"\n", 0o755)
	if rows, ok, _ := Problems(w.home, w.src, w.sandbox); !ok {
		t.Fatalf("pre-push exécutable toléré : %q", rows)
	}
	w.git("-C", w.base+"/clean", "config", "--unset", "core.hooksPath")
}

func TestProfileWithoutHooksPath(t *testing.T) {
	w := newWorld(t)
	w.git("-C", w.base+"/dirty", "config", "user.email", "tester@example.com")
	write(t, filepath.Join(w.home, ".config/git/profiles/demo.gitconfig"),
		"[user]\n  email = tester@example.com\n[dotfiles]\n  profile = demo\n", 0o644)
	w.git("config", "--global", "--add", "includeIf.gitdir:"+w.sandbox+"/scratch/.path",
		filepath.Join(w.home, ".config/git/profiles/demo.gitconfig"))
	w.git("-C", w.sandbox+"/scratch", "config", "core.hooksPath", ".husky")
	if rows, ok, _ := Problems(w.home, w.src, w.sandbox); !ok {
		t.Fatalf("hooks locaux non signalés sans hooksPath de profil : %q", rows)
	}
}

func TestFormatAlignsByRunes(t *testing.T) {
	rows := []Row{
		{display: "~/é", Branch: "main", Profile: "-", Email: "aucun"},
		{display: "~/longer", Branch: "détaché", Changes: 12, Profile: "perso", Email: "a@example.com", Reason: "✗ x"},
	}
	got := Format(rows, true)
	if !strings.HasSuffix(got[0], "aucun") {
		t.Errorf("ligne 0 = %q", got[0])
	}
	if !strings.HasSuffix(got[1], "  \033[31m✗ x\033[0m") {
		t.Errorf("ligne 1 = %q", got[1])
	}
	col := func(line, word string) int { return len([]rune(line[:strings.Index(line, word)])) }
	if col(got[0], "main") != col(got[1], "détaché") || col(got[0], "aucun") != col(got[1], "a@example.com") {
		t.Errorf("colonnes désalignées :\n%s\n%s", got[0], got[1])
	}
	if strings.Contains(Format(rows, false)[1], "\033[") {
		t.Error("couleur sans demande")
	}
}

func TestExportSortedPrivate(t *testing.T) {
	w := newWorld(t)
	file := filepath.Join(w.home, "repos.local")
	write(t, file, "old\n", 0o644)
	n, err := Export(w.src, file)
	if err != nil || n != 3 {
		t.Fatalf("export = %d, %v", n, err)
	}
	want := "https://git.example.com/team/ahead.git\nhttps://git.example.com/team/clean.git\nhttps://git.example.com/team/dirty.git\n"
	if b, _ := os.ReadFile(file); string(b) != want {
		t.Fatalf("contenu = %q", b)
	}
	if fi, _ := os.Stat(file); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", fi.Mode())
	}
	if m, _ := filepath.Glob(file + ".*"); len(m) != 0 {
		t.Fatalf("temporaires restants : %v", m)
	}
}

func TestCloneListContinuesAfterFailures(t *testing.T) {
	w := newWorld(t)
	list := filepath.Join(w.home, "list")
	write(t, list, "\n# comment\ninvalid-url\nhttps://git.example.com/team/clean.git\nhttps://git.example.com/team/unreachable.git\n", 0o644)
	fresh := filepath.Join(w.home, "fresh repos")
	var errb bytes.Buffer
	good, bad, err := CloneList(list, fresh, &errb)
	if err != nil || good != 1 || bad != 2 {
		t.Fatalf("good %d bad %d err %v", good, bad, err)
	}
	for _, want := range []string{"échec : (URL invalide)\n", "échec : " + fresh + "/git.example.com/team/unreachable\n"} {
		if !strings.Contains(errb.String(), want) {
			t.Errorf("stderr sans %q : %q", want, errb.String())
		}
	}
	if strings.Contains(errb.String(), "invalid-url") || strings.Contains(errb.String(), "https://") || strings.Contains(errb.String(), "file://") {
		t.Errorf("URL affichée : %q", errb.String())
	}
	if _, err := os.Stat(fresh + "/git.example.com/team/clean/marker"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CloneList(filepath.Join(w.home, "absent"), fresh, &errb); err != ErrUnreadable {
		t.Fatalf("err = %v", err)
	}
}
