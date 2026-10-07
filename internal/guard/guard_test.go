package guard

import (
	"bytes"
	"errors"
	"fmt"
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
		if err := newGuard(t, dir, s).Push(strings.NewReader(tc.line+"\n"), ""); err != nil {
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
	if err := g.Push(strings.NewReader("refs/heads/acmecorp "+zeros+" refs/heads/acmecorp "+strings.Repeat("b", 40)+"\n\n"), ""); err != nil {
		t.Fatal(err)
	}
	if err := g.Push(strings.NewReader(""), ""); err != nil || len(s.calls) != 0 {
		t.Fatalf("%v, %v", err, s.calls)
	}
}

func TestPushRefNameBlocked(t *testing.T) {
	dir := repo(t)
	head := git(t, dir, "rev-parse", "HEAD")
	line := "refs/heads/main " + head + " refs/heads/acmecorp-sync " + strings.Repeat("0", 40) + "\n"
	err := newGuard(t, dir, &scans{}).Push(strings.NewReader(line), "")
	wantFailure(t, err, "nom de branche ou de tag interdit : refs/heads/main")
}

func TestPushMalformedInputFailsClosed(t *testing.T) {
	dir := repo(t)
	for _, in := range []string{"a b c\n", "refs/heads/main --upload-pack=x refs/heads/main " + strings.Repeat("0", 40) + "\n"} {
		wantFailure(t, newGuard(t, dir, &scans{}).Push(strings.NewReader(in), ""), "invalide")
	}
}

func TestPushAddedContentOnlyAndHistory(t *testing.T) {
	dir := repo(t)
	first := git(t, dir, "rev-parse", "HEAD")
	bad := commit(t, dir, "c.txt", "client zed\n", "add")
	zeros := strings.Repeat("0", 40)
	s := &scans{}
	g := newGuard(t, dir, s)
	wantFailure(t, g.Push(strings.NewReader("refs/heads/main "+bad+" refs/heads/main "+first+"\n"), ""), "contenu ajouté par : "+first+".."+bad)
	// Removing the term is allowed: only added lines count.
	git(t, dir, "rm", "-q", "c.txt")
	git(t, dir, "commit", "-q", "-m", "remove")
	fixed := git(t, dir, "rev-parse", "HEAD")
	if err := g.Push(strings.NewReader("refs/heads/main "+fixed+" refs/heads/main "+bad+"\n"), ""); err != nil {
		t.Fatal(err)
	}
	// A new ref checks everything not on a remote, the bad commit included.
	wantFailure(t, g.Push(strings.NewReader("refs/heads/x "+fixed+" refs/heads/x "+zeros+"\n"), ""), "contenu ajouté par")
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
	wantFailure(t, err, "  <nom masqué> (fichier 2)") // the name holds a term: masked, with its rank
	if strings.Contains(err.Error(), "ok.txt") || strings.Contains(strings.ToLower(err.Error()), "acmecorp") || strings.Contains(strings.ToLower(err.Error()), "tenant") || len(s.calls) != 0 {
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

// evilMerge builds a merge whose resolution adds the file name with the content, on top of two
// clean branches, and returns the commit before it and the merge.
func evilMerge(t *testing.T, dir, name, content string) (base, merge string) {
	t.Helper()
	base = git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "checkout", "-q", "-b", "side")
	commit(t, dir, "side.txt", "propre\n", "side")
	git(t, dir, "checkout", "-q", "-")
	commit(t, dir, "main.txt", "propre\n", "main")
	git(t, dir, "merge", "-q", "--no-ff", "--no-commit", "side")
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", name)
	git(t, dir, "commit", "-q", "-m", "merge")
	return base, git(t, dir, "rev-parse", "HEAD")
}

// TestHiddenContentIsSeen: binary files, -diff files and merge resolutions are checked by push and all.
func TestHiddenContentIsSeen(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, dir string) (base, head string)
		want  string
	}{
		{"binaire avec NUL", func(t *testing.T, dir string) (string, string) {
			base := git(t, dir, "rev-parse", "HEAD")
			return base, commit(t, dir, "f.bin", "\x00\x01 client zed \x00\n", "bin")
		}, "contenu ajouté par"},
		{"attribut -diff", func(t *testing.T, dir string) (string, string) {
			commit(t, dir, ".gitattributes", "* -diff\n", "attrs")
			base := git(t, dir, "rev-parse", "HEAD")
			return base, commit(t, dir, "p.txt", "client zed\n", "nodiff")
		}, "contenu ajouté par"},
		{"fusion dont la résolution ajoute le terme", func(t *testing.T, dir string) (string, string) {
			return evilMerge(t, dir, "m.txt", "client zed\n")
		}, "contenu ajouté par"},
		{"fusion dont la résolution ajoute un nom interdit", func(t *testing.T, dir string) (string, string) {
			return evilMerge(t, dir, "notes-acmecorp.txt", "propre\n")
		}, "noms de fichiers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := repo(t)
			base, head := tc.setup(t, dir)
			g := newGuard(t, dir, &scans{})
			wantFailure(t, g.Push(strings.NewReader("refs/heads/main "+head+" refs/heads/main "+base+"\n"), ""), tc.want)
			wantFailure(t, g.All(), "historique")
		})
	}
}

// TestMediaContentIsNotScanned: bytes of an image or a font are not text, so a term matched in
// them is chance; their names are still checked and any other binary file is still read.
func TestMediaContentIsNotScanned(t *testing.T) {
	dir := repo(t)
	base := git(t, dir, "rev-parse", "HEAD")
	head := commit(t, dir, "logo.PNG", "\x89PNG\x00\x01 client zed \x00\n", "image")
	commit(t, dir, "a.woff2", "wOF2\x00 client zed \x00\n", "font")
	head = git(t, dir, "rev-parse", "HEAD")
	g := newGuard(t, dir, &scans{})
	if err := g.Push(strings.NewReader("refs/heads/main "+head+" refs/heads/main "+base+"\n"), ""); err != nil {
		t.Fatal(err)
	}
	if err := newGuard(t, dir, &scans{}).All(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.webp"), []byte("RIFF\x00 client zed \x00\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "x.webp")
	s := &scans{}
	if err := newGuard(t, dir, s).Staged(); err != nil || len(s.calls) != 1 { // the secret scan still runs
		t.Fatalf("%v, %v", err, s.calls)
	}
	git(t, dir, "reset", "-q")
	commit(t, dir, "globex-inc.png", "\x89PNG\n", "named")
	wantFailure(t, newGuard(t, dir, &scans{}).All(), "noms de fichiers")
}

func TestOrdinaryCommitsStillPass(t *testing.T) {
	dir := repo(t)
	base, head := evilMerge(t, dir, "m.txt", "propre\n")
	if err := newGuard(t, dir, &scans{}).Push(strings.NewReader("refs/heads/main "+head+" refs/heads/main "+base+"\n"), ""); err != nil {
		t.Fatal(err)
	}
	if err := newGuard(t, dir, &scans{}).All(); err != nil {
		t.Fatal(err)
	}
}

func TestAnnotatedTags(t *testing.T) {
	zeros := strings.Repeat("0", 40)
	for _, tc := range []struct {
		name string
		args []string // git arguments creating the tag v1
		bad  bool
	}{
		{"message interdit", []string{"tag", "-a", "-m", "release for Globex-Inc", "v1"}, true},
		{"tagger interdit", []string{"-c", "user.name=Acmecorp Bot", "tag", "-a", "-m", "propre", "v1"}, true},
		{"email du tagger interdit", []string{"-c", "user.email=me@acmecorp.example", "tag", "-a", "-m", "propre", "v1"}, true},
		{"tag annoté propre", []string{"tag", "-a", "-m", "propre", "v1"}, false},
		{"tag léger", []string{"tag", "v1"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := repo(t)
			git(t, dir, tc.args...)
			sha := git(t, dir, "rev-parse", "v1")
			g := newGuard(t, dir, &scans{})
			push, all := g.Push(strings.NewReader("refs/tags/v1 "+sha+" refs/tags/v1 "+zeros+"\n"), "origin"), g.All()
			if !tc.bad {
				if push != nil || all != nil {
					t.Fatalf("push = %v, all = %v", push, all)
				}
				return
			}
			wantFailure(t, push, "tag annoté : "+sha)
			wantFailure(t, all, "tag annoté : "+sha)
			for _, err := range []error{push, all} {
				if low := strings.ToLower(err.Error()); strings.Contains(low, "acmecorp") || strings.Contains(low, "globex") {
					t.Errorf("le texte interdit est affiché : %v", err)
				}
			}
		})
	}
	t.Run("tag de tag", func(t *testing.T) {
		dir := repo(t)
		git(t, dir, "tag", "-a", "-m", "client zed", "inner")
		git(t, dir, "tag", "-a", "-m", "propre", "outer", "inner")
		sha := git(t, dir, "rev-parse", "outer")
		wantFailure(t, newGuard(t, dir, &scans{}).Push(strings.NewReader("refs/tags/outer "+sha+" refs/tags/outer "+zeros+"\n"), ""), "tag annoté")
	})
	// chain makes n nested tags t1..tn (t1 on the commit, message msg1) and keeps only tn as a ref,
	// so the inner messages are reachable through the outer tag object alone.
	chain := func(t *testing.T, n int, msg1 string) (dir, sha string) {
		dir = repo(t)
		git(t, dir, "tag", "-a", "-m", msg1, "t1")
		for i := 2; i <= n; i++ {
			git(t, dir, "tag", "-a", "-m", "propre", fmt.Sprintf("t%d", i), fmt.Sprintf("t%d", i-1))
			git(t, dir, "tag", "-d", fmt.Sprintf("t%d", i-1))
		}
		return dir, git(t, dir, "rev-parse", fmt.Sprintf("t%d", n))
	}
	t.Run("chaîne de 20 tags, terme dans le plus interne", func(t *testing.T) {
		dir, sha := chain(t, 20, "client zed")
		g := newGuard(t, dir, &scans{})
		wantFailure(t, g.Push(strings.NewReader("refs/tags/t20 "+sha+" refs/tags/t20 "+zeros+"\n"), ""), "tag annoté")
		wantFailure(t, g.All(), "tag annoté")
	})
	t.Run("chaîne de 20 tags propres", func(t *testing.T) {
		dir, sha := chain(t, 20, "propre")
		g := newGuard(t, dir, &scans{})
		if err := g.Push(strings.NewReader("refs/tags/t20 "+sha+" refs/tags/t20 "+zeros+"\n"), ""); err != nil {
			t.Fatal(err)
		}
		if err := g.All(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("chaîne au-delà du plafond", func(t *testing.T) {
		old := maxTagChain
		maxTagChain = 5
		t.Cleanup(func() { maxTagChain = old })
		dir, sha := chain(t, 8, "propre")
		g := newGuard(t, dir, &scans{})
		wantFailure(t, g.Push(strings.NewReader("refs/tags/t8 "+sha+" refs/tags/t8 "+zeros+"\n"), ""), "chaîne de tags trop profonde")
		wantFailure(t, g.All(), "chaîne de tags trop profonde")
	})
	t.Run("git en échec", func(t *testing.T) {
		wantFailure(t, newGuard(t, repo(t), &scans{}).tags(strings.Repeat("a", 40)), "commande en échec")
	})
}

func TestPushRangeUsesTheTargetRemote(t *testing.T) {
	dir := repo(t)
	first := git(t, dir, "rev-parse", "HEAD")
	bad := commit(t, dir, "c.txt", "client zed\n", "add")
	git(t, dir, "update-ref", "refs/remotes/origin/main", first)
	git(t, dir, "update-ref", "refs/remotes/other/main", bad) // another remote already holds it
	line := "refs/heads/feat " + bad + " refs/heads/feat " + strings.Repeat("0", 40) + "\n"
	for _, tc := range []struct{ remote, scan string }{
		{"origin", "--not --remotes=origin"},
		{"other", "--not --remotes=other"},
		{"", "--not --remotes"},
		{"o*", "--not --remotes"},
		{"a b", "--not --remotes"},
	} {
		s := &scans{}
		err := newGuard(t, dir, s).Push(strings.NewReader(line), tc.remote)
		if tc.scan == "--not --remotes=origin" {
			wantFailure(t, err, "contenu ajouté par")
			continue
		}
		if err != nil {
			t.Fatalf("remote %q : %v", tc.remote, err)
		}
		if got := strings.Join(s.calls[0], " "); got != "git --log-opts="+bad+" "+tc.scan+" ." {
			t.Errorf("remote %q : scanner appelé avec %q", tc.remote, got)
		}
	}
}

func TestStagedFileNamedLikeAStage(t *testing.T) {
	dir := repo(t) // a.txt is clean; the file "0:a.txt" must not be read as stage 0 of a.txt
	if err := os.WriteFile(filepath.Join(dir, "0:a.txt"), []byte("client zed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "--", "0:a.txt")
	wantFailure(t, newGuard(t, dir, &scans{}).Staged(), "  0:a.txt")
}

func TestForbiddenNamesAreNeverPrinted(t *testing.T) {
	dir := repo(t)
	head := git(t, dir, "rev-parse", "HEAD")
	g := newGuard(t, dir, &scans{})
	err := g.Push(strings.NewReader("refs/heads/acmecorp-sync "+head+" refs/heads/x "+strings.Repeat("0", 40)+"\n"), "")
	wantFailure(t, err, "interdit : <nom masqué> (ligne 1)")
	if strings.Contains(err.Error(), "acmecorp") {
		t.Fatalf("nom de ref affiché : %v", err)
	}
	if got := g.shown("log", "--remotes=acmecorp", "ok"); got != "log <masqué> ok" {
		t.Fatalf("shown = %q", got)
	}
}

// dotfiles.guard=secrets leaves Terms nil: a push still runs the scanner and never panics.
func TestPushSecretsOnlyModeSkipsTerms(t *testing.T) {
	dir := repo(t)
	first := git(t, dir, "rev-parse", "HEAD")
	second := commit(t, dir, "b.txt", "AcmeCorp\n", "mentions AcmeCorp")
	s := &scans{}
	g := &Guard{Dir: dir, Scan: s.run, Stderr: &bytes.Buffer{}}
	if err := g.Push(strings.NewReader("refs/heads/main "+second+" refs/heads/main "+first+"\n"), ""); err != nil || len(s.calls) != 1 {
		t.Fatalf("%v, %v", err, s.calls)
	}
}
