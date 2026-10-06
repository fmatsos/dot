package guard

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repo makes an isolated repository with one clean commit and returns its directory.
func repo(t *testing.T) string {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, []byte("[user]\n\tname = T\n\temail = t@example.org\n[init]\n\tdefaultBranch = main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	commit(t, dir, "a.txt", "items and systems, zedd\n", "init")
	return dir
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v : %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, dir, name, content, msg string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", name)
	git(t, dir, "commit", "-q", "-m", msg)
	return git(t, dir, "rev-parse", "HEAD")
}

type scans struct {
	calls [][]string
	err   error
}

func (s *scans) run(args ...string) error {
	s.calls = append(s.calls, args)
	return s.err
}

func newGuard(t *testing.T, dir string, s *scans) *Guard {
	t.Helper()
	terms, err := ParseTerms([]byte(fictional))
	if err != nil {
		t.Fatal(err)
	}
	return &Guard{Terms: terms, Dir: dir, Scan: s.run, Stderr: &bytes.Buffer{}}
}

func wantFailure(t *testing.T, err error, sub string) {
	t.Helper()
	var f *Failure
	if !errors.As(err, &f) || !strings.Contains(f.Msg, sub) {
		t.Fatalf("erreur = %v, attendu un refus contenant %q", err, sub)
	}
}

func TestPushRanges(t *testing.T) {
	dir := repo(t)
	first := git(t, dir, "rev-parse", "HEAD")
	second := commit(t, dir, "b.txt", "propre\n", "second")
	zeros := strings.Repeat("0", 40)

	for _, tc := range []struct {
		name, line, wantOpts string
	}{
		{"nouvelle ref", "refs/heads/main " + second + " refs/heads/main " + zeros, "--log-opts=" + second + " --not --remotes"},
		{"plage connue", "refs/heads/main " + second + " refs/heads/main " + first, "--log-opts=" + first + ".." + second},
		{"tip distant inconnu", "refs/heads/main " + second + " refs/heads/main " + strings.Repeat("a", 40), "--log-opts=" + second + " --not --remotes"},
	} {
		s := &scans{}
		if err := newGuard(t, dir, s).Push(strings.NewReader(tc.line + "\n")); err != nil {
			t.Fatalf("%s : %v", tc.name, err)
		}
		if len(s.calls) != 1 || strings.Join(s.calls[0], " ") != "git "+tc.wantOpts+" ." {
			t.Errorf("%s : scanner appelé avec %v", tc.name, s.calls)
		}
	}
}

func TestPushDeletionIgnoredAndEmptyInput(t *testing.T) {
	dir := repo(t)
	zeros := strings.Repeat("0", 40)
	s := &scans{}
	g := newGuard(t, dir, s)
	// Deleting a ref whose name holds a term is not checked either: nothing is sent.
	if err := g.Push(strings.NewReader("refs/heads/acmecorp " + zeros + " refs/heads/acmecorp " + strings.Repeat("b", 40) + "\n\n")); err != nil {
		t.Fatal(err)
	}
	if err := g.Push(strings.NewReader("")); err != nil || len(s.calls) != 0 {
		t.Fatalf("%v, %v", err, s.calls)
	}
}

func TestPushRefNameBlocked(t *testing.T) {
	dir := repo(t)
	head := git(t, dir, "rev-parse", "HEAD")
	line := "refs/heads/main " + head + " refs/heads/acmecorp-sync " + strings.Repeat("0", 40) + "\n"
	err := newGuard(t, dir, &scans{}).Push(strings.NewReader(line))
	wantFailure(t, err, "nom de branche ou de tag interdit : refs/heads/main")
}

func TestPushMalformedInputFailsClosed(t *testing.T) {
	dir := repo(t)
	for _, in := range []string{"a b c\n", "refs/heads/main --upload-pack=x refs/heads/main " + strings.Repeat("0", 40) + "\n"} {
		wantFailure(t, newGuard(t, dir, &scans{}).Push(strings.NewReader(in)), "invalide")
	}
}

func TestPushAddedContentOnlyAndHistory(t *testing.T) {
	dir := repo(t)
	first := git(t, dir, "rev-parse", "HEAD")
	bad := commit(t, dir, "c.txt", "client zed\n", "add")
	zeros := strings.Repeat("0", 40)
	s := &scans{}
	g := newGuard(t, dir, s)
	wantFailure(t, g.Push(strings.NewReader("refs/heads/main "+bad+" refs/heads/main "+first+"\n")), "contenu ajouté par : "+first+".."+bad)
	// Removing the term is allowed: only added lines count.
	git(t, dir, "rm", "-q", "c.txt")
	git(t, dir, "commit", "-q", "-m", "remove")
	fixed := git(t, dir, "rev-parse", "HEAD")
	if err := g.Push(strings.NewReader("refs/heads/main " + fixed + " refs/heads/main " + bad + "\n")); err != nil {
		t.Fatal(err)
	}
	// A new ref checks everything not on a remote, the bad commit included.
	wantFailure(t, g.Push(strings.NewReader("refs/heads/x "+fixed+" refs/heads/x "+zeros+"\n")), "contenu ajouté par")
}

func TestHistoryMetadata(t *testing.T) {
	dir := repo(t)
	base := git(t, dir, "rev-parse", "HEAD")
	head := commit(t, dir, "d.txt", "propre\n", "fix for Globex-Inc")
	wantFailure(t, newGuard(t, dir, &scans{}).history(base+".."+head), "métadonnées ou noms de fichiers")
}

func TestStagedBlocksNameContentAndReportsOnlyNames(t *testing.T) {
	dir := repo(t)
	for name, content := range map[string]string{"notes-acmecorp.txt": "propre\n", "b.txt": "tenant = AcmeCorp\n", "ok.txt": "rien\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git(t, dir, "add", "-A")
	s := &scans{}
	err := newGuard(t, dir, s).Staged()
	wantFailure(t, err, "  b.txt")
	wantFailure(t, err, "  notes-acmecorp.txt")
	if strings.Contains(err.Error(), "ok.txt") || strings.Contains(strings.ToLower(err.Error()), "tenant") || len(s.calls) != 0 {
		t.Fatalf("rapport trop bavard ou scanner lancé : %v", err)
	}
}

func TestStagedCleanRunsScannerAndScannerFailureBlocks(t *testing.T) {
	dir := repo(t)
	if err := os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("rien\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "ok.txt")
	s := &scans{}
	if err := newGuard(t, dir, s).Staged(); err != nil || len(s.calls) != 1 || strings.Join(s.calls[0], " ") != "git --staged ." {
		t.Fatalf("%v, %v", err, s.calls)
	}
	s.err = &exec.ExitError{}
	wantFailure(t, newGuard(t, dir, s).Staged(), "betterleaks a signalé un secret")
	s.err = errors.New("betterleaks : empreinte non figée pour x")
	wantFailure(t, newGuard(t, dir, s).Staged(), "empreinte non figée")
	g := newGuard(t, dir, s)
	g.Scan = nil
	wantFailure(t, g.Staged(), "betterleaks indisponible")
}

func TestStagedInvalidIndexedManifest(t *testing.T) {
	dir := repo(t)
	if err := os.WriteFile(filepath.Join(dir, "dot.json"), []byte(`{"unexpected":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "dot.json")
	err := newGuard(t, dir, &scans{}).Staged()
	wantFailure(t, err, "clé unexpected")
}

func TestGitFailureFailsClosed(t *testing.T) {
	notARepo := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(notARepo))
	for name, f := range map[string]func(*Guard) error{
		"staged": (*Guard).Staged,
		"all":    (*Guard).All,
	} {
		s := &scans{}
		wantFailure(t, f(newGuard(t, notARepo, s)), "")
		if len(s.calls) != 0 {
			t.Errorf("%s : le scanner ne doit pas tourner après un échec git", name)
		}
	}
}

func TestMsg(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) string {
		p := filepath.Join(dir, "msg")
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	g := &Guard{Terms: mustTerms(t)}
	wantFailure(t, g.Msg(write("fix for Globex-Inc\n")), "message de commit")
	if err := g.Msg(write("propre\n# Globex-Inc dans un commentaire\n")); err != nil {
		t.Fatal(err)
	}
	wantFailure(t, g.Msg(filepath.Join(dir, "absent")), "illisible")
	if err := (&Guard{}).Msg("/nonexistent"); err != nil { // secrets-only mode
		t.Fatal(err)
	}
}

func TestSecretsOnlyModeSkipsTerms(t *testing.T) {
	dir := repo(t)
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("AcmeCorp\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "b.txt")
	s := &scans{}
	g := &Guard{Dir: dir, Scan: s.run, Stderr: &bytes.Buffer{}}
	if err := g.Staged(); err != nil || len(s.calls) != 1 {
		t.Fatalf("%v, %v", err, s.calls)
	}
}

func TestAllChecksRefsHistoryAndSecrets(t *testing.T) {
	dir := repo(t)
	s := &scans{}
	if err := newGuard(t, dir, s).All(); err != nil || len(s.calls) != 1 || strings.Join(s.calls[0], " ") != "git ." {
		t.Fatalf("%v, %v", err, s.calls)
	}
	git(t, dir, "branch", "globex-inc")
	wantFailure(t, newGuard(t, dir, &scans{}).All(), "nom de branche ou de tag interdit")
	git(t, dir, "branch", "-q", "-D", "globex-inc")
	commit(t, dir, "z.txt", "client zed\n", "add")
	wantFailure(t, newGuard(t, dir, &scans{}).All(), "contenu de l'historique")
}

func mustTerms(t *testing.T) *Terms {
	t.Helper()
	terms, err := ParseTerms([]byte(fictional))
	if err != nil {
		t.Fatal(err)
	}
	return terms
}
