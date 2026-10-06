package guard

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unborn makes an isolated repository with no commit yet.
func unborn(t *testing.T) string {
	t.Helper()
	dir := repo(t)
	_ = os.RemoveAll(filepath.Join(dir, ".git"))
	git(t, dir, "init", "-q")
	_ = os.Remove(filepath.Join(dir, "a.txt"))
	return dir
}

func stage(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "--literal-pathspecs", "add", "-f", "--", name)
}

func staged(t *testing.T, dir string) error {
	t.Helper()
	return newGuard(t, dir, &scans{}).Staged()
}

func TestStagedAwkwardFileNamesCleanPass(t *testing.T) {
	for _, name := range []string{
		"café.txt", "café.txt", // NFC and NFD
		"with space.txt", "-dash.txt", "--help", `quo"te.txt`, "it's.txt", `back\slash.txt`,
		"new\nline.txt", "tab\there.txt", "日本語.txt", "0:a.txt", "*glob[x].txt", ":(top)x.txt",
	} {
		t.Run(printable(name), func(t *testing.T) {
			dir := repo(t)
			stage(t, dir, name, "propre\n")
			if err := staged(t, dir); err != nil {
				t.Fatalf("%v", err)
			}
		})
	}
}

func TestStagedAwkwardFileNamesWithTermContent(t *testing.T) {
	for _, name := range []string{"café.txt", "café.txt", "with space", "-dash", `quo"te`, `back\slash`, "new\nline", ":(top)x", "*glob[x]"} {
		t.Run(printable(name), func(t *testing.T) {
			dir := repo(t)
			stage(t, dir, name, "tenant AcmeCorp\n")
			err := staged(t, dir)
			wantFailure(t, err, "référence interdite (nom ou contenu) dans")
			if strings.Contains(strings.ToLower(err.Error()), "acmecorp") {
				t.Fatalf("le terme est affiché : %v", err)
			}
			if strings.Contains(err.Error(), "\t") || strings.Count(err.Error(), "\n") != 1 {
				t.Fatalf("caractère de contrôle du nom affiché : %q", err.Error())
			}
		})
	}
}

func TestStagedTermOnlyInFileName(t *testing.T) {
	for _, name := range []string{"notes-acmecorp.txt", "acmecorp/x.txt", "ok\nacmecorp.txt"} {
		dir := repo(t)
		stage(t, dir, name, "propre\n")
		err := staged(t, dir)
		wantFailure(t, err, "<nom masqué>")
		if strings.Contains(err.Error(), "acmecorp") {
			t.Fatalf("%q : nom interdit affiché", name)
		}
	}
}

// NFC term, NFD name (what macOS can commit): RE2 does not normalize, so it is not matched.
// Documented blind spot, pinned so that a change of behavior is a conscious one.
func TestUnicodeNormalizationBlindSpot(t *testing.T) {
	terms, err := ParseTerms([]byte("cafécorp\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !terms.Match("cafécorp") {
		t.Fatal("NFC contre NFC doit correspondre")
	}
	if terms.Match("cafécorp") {
		t.Fatal("la normalisation est désormais gérée : retirer ce test d'angle mort")
	}
}

func TestStagedDeletionAndRename(t *testing.T) {
	dir := repo(t)
	commit(t, dir, "old-acmecorp.txt", "acmecorp\n", "legacy")
	// Deleting a forbidden file adds nothing: allowed.
	git(t, dir, "rm", "-q", "old-acmecorp.txt")
	if err := staged(t, dir); err != nil {
		t.Fatalf("suppression : %v", err)
	}
	git(t, dir, "reset", "-q", "--hard")

	// A rename to a clean name keeps the bad content: refused, by the new name.
	git(t, dir, "mv", "old-acmecorp.txt", "clean.txt")
	wantFailure(t, staged(t, dir), "clean.txt")
	git(t, dir, "reset", "-q", "--hard")

	// A clean file renamed to a forbidden name is refused by name.
	git(t, dir, "mv", "a.txt", "acmecorp.txt")
	wantFailure(t, staged(t, dir), "<nom masqué>")
	git(t, dir, "reset", "-q", "--hard")

	git(t, dir, "mv", "a.txt", "b.txt")
	if err := staged(t, dir); err != nil {
		t.Fatalf("renommage propre : %v", err)
	}
}

func TestStagedBinaryAndLargeFiles(t *testing.T) {
	dir := repo(t)
	// Actual behavior: binary files ARE scanned (git show prints the blob as is).
	stage(t, dir, "f.bin", "\x00\x01client AcmeCorp\x00\xff\n")
	wantFailure(t, staged(t, dir), "f.bin")
	git(t, dir, "rm", "-q", "--cached", "-f", "f.bin")

	filler := strings.Repeat("x", 1023) + "\n"
	stage(t, dir, "big.txt", strings.Repeat(filler, 6*1024)+"fin AcmeCorp")
	wantFailure(t, staged(t, dir), "big.txt")
	git(t, dir, "rm", "-q", "--cached", "-f", "big.txt")
	stage(t, dir, "oneline.txt", strings.Repeat("y", 5<<20)+"AcmeCorp")
	wantFailure(t, staged(t, dir), "oneline.txt")
	git(t, dir, "rm", "-q", "--cached", "-f", "oneline.txt")
	stage(t, dir, "bigclean.txt", strings.Repeat(filler, 6*1024))
	if err := staged(t, dir); err != nil {
		t.Fatalf("gros fichier propre : %v", err)
	}
}

func TestTermSplitAcrossLinesDoesNotMatch(t *testing.T) {
	// Patterns are matched line by line, like grep: a term broken by a newline is not found.
	dir := repo(t)
	stage(t, dir, "split.txt", "acme\ncorp\n")
	if err := staged(t, dir); err != nil {
		t.Fatalf("documenté : un terme coupé par un saut de ligne ne correspond pas : %v", err)
	}
}

func TestStagedGitlinkNeitherCrashesNorIsOpened(t *testing.T) {
	dir := repo(t)
	sub := t.TempDir()
	git(t, sub, "init", "-q")
	subSha := commit(t, sub, "s.txt", "tenant AcmeCorp\n", "in the submodule")
	// A gitlink: a mode 160000 entry whose commit is not in this repository.
	git(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+subSha+",vendor/lib")
	if err := staged(t, dir); err != nil {
		t.Fatalf("un gitlink ne doit pas bloquer le commit : %v", err)
	}
	// Its name is still checked.
	git(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+subSha+",acmecorp-lib")
	wantFailure(t, staged(t, dir), "<nom masqué>")
}

func TestStagedUnbornBranch(t *testing.T) {
	dir := unborn(t)
	if err := staged(t, dir); err != nil {
		t.Fatalf("index vide : %v", err)
	}
	stage(t, dir, "a.txt", "propre\n")
	if err := staged(t, dir); err != nil {
		t.Fatalf("branche sans commit : %v", err)
	}
	stage(t, dir, "b.txt", "AcmeCorp\n")
	wantFailure(t, staged(t, dir), "b.txt")
}

func TestStagedDetachedHead(t *testing.T) {
	dir := repo(t)
	git(t, dir, "checkout", "-q", "--detach")
	stage(t, dir, "ok.txt", "propre\n")
	if err := staged(t, dir); err != nil {
		t.Fatal(err)
	}
	stage(t, dir, "bad.txt", "AcmeCorp\n")
	wantFailure(t, staged(t, dir), "bad.txt")
}

func TestStagedNotARepositoryFailsClosed(t *testing.T) {
	if err := staged(t, t.TempDir()); err == nil {
		t.Fatal("hors dépôt : doit échouer")
	}
}

func TestAllOnUnbornAndDetached(t *testing.T) {
	// Nothing to log on an unborn branch: whatever the outcome, a refusal must name the command.
	if err := newGuard(t, unborn(t), &scans{}).All(); err != nil {
		wantFailure(t, err, "commande en échec")
	}
	dir := repo(t)
	git(t, dir, "checkout", "-q", "--detach")
	commit(t, dir, "d.txt", "AcmeCorp\n", "detached")
	wantFailure(t, newGuard(t, dir, &scans{}).All(), "contenu")
}

func TestPushNewBranchDeletionAndMalformed(t *testing.T) {
	dir := repo(t)
	head := git(t, dir, "rev-parse", "HEAD")
	zeros := strings.Repeat("0", 40)
	s := &scans{}
	g := newGuard(t, dir, s)
	if err := g.Push(strings.NewReader("refs/heads/n "+head+" refs/heads/n "+zeros+"\n"), "origin"); err != nil {
		t.Fatal(err)
	}
	if len(s.calls) != 1 || s.calls[0][1] != "--log-opts="+head+" --not --remotes=origin" {
		t.Fatalf("scanner : %v", s.calls)
	}
	// Deletion: local sha all zeros; the remote sha is not even looked at. Nothing is sent, nothing scanned.
	s.calls = nil
	for _, rsha := range []string{head, zeros, "nothex"} {
		if err := g.Push(strings.NewReader("(delete) "+zeros+" refs/heads/n "+rsha+"\n"), "origin"); err != nil {
			t.Fatalf("suppression %q : %v", rsha, err)
		}
	}
	if len(s.calls) != 0 {
		t.Fatalf("une suppression ne doit rien scanner : %v", s.calls)
	}
	for _, bad := range []string{"zz" + head[2:], head[:39]} {
		wantFailure(t, g.Push(strings.NewReader("refs/heads/n "+bad+" refs/heads/n "+zeros+"\n"), ""), "invalide")
	}
	wantFailure(t, g.Push(strings.NewReader("a b c\n"), ""), "invalide")
	wantFailure(t, g.Push(strings.NewReader("a b c d e\n"), ""), "invalide")
}

func TestPushDetachedHeadSource(t *testing.T) {
	dir := repo(t)
	git(t, dir, "checkout", "-q", "--detach")
	bad := commit(t, dir, "d.txt", "AcmeCorp\n", "detached")
	line := "HEAD " + bad + " refs/heads/n " + strings.Repeat("0", 40) + "\n"
	wantFailure(t, newGuard(t, dir, &scans{}).Push(strings.NewReader(line), "origin"), "contenu")
}

func TestPushRemoteNameNeverWidensTheScope(t *testing.T) {
	dir := repo(t)
	head := git(t, dir, "rev-parse", "HEAD")
	line := "refs/heads/n " + head + " refs/heads/n " + strings.Repeat("0", 40) + "\n"
	for _, remote := range []string{"or*", "o[r]", "a b", "o\\x", "o\nx"} {
		s := &scans{}
		if err := newGuard(t, dir, s).Push(strings.NewReader(line), remote); err != nil {
			t.Fatalf("%q : %v", remote, err)
		}
		if got := s.calls[0][1]; got != "--log-opts="+head+" --not --remotes" {
			t.Errorf("%q : %s", remote, got)
		}
	}
}

func TestMsgBodyAndTrailer(t *testing.T) {
	g := newGuard(t, repo(t), &scans{})
	write := func(s string) string {
		p := filepath.Join(t.TempDir(), "msg")
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for name, body := range map[string]string{
		"sujet":        "AcmeCorp fix\n",
		"corps":        "fix\n\ncontexte AcmeCorp ici\n",
		"trailer":      "fix\n\nReviewed-by: AcmeCorp Bot <b@x.org>\n",
		"trailer CRLF": "fix\r\n\r\nCo-Authored-By: acmecorp\r\n",
		"sans newline": "fix\n\nSigned-off-by: acmecorp",
		"# collé":      "fix\n\n#AcmeCorp commenté puis\nAcmeCorp\n",
	} {
		err := g.Msg(write(body))
		wantFailure(t, err, "message de commit")
		if strings.Contains(strings.ToLower(err.Error()), "acmecorp") {
			t.Errorf("%s : terme affiché", name)
		}
	}
	for name, body := range map[string]string{
		"vide":            "",
		"propre":          "fix\n\nSigned-off-by: T <t@example.org>\n",
		"commentaire git": "fix\n# AcmeCorp dans un commentaire\n",
	} {
		if err := g.Msg(write(body)); err != nil {
			t.Errorf("%s : %v", name, err)
		}
	}
	wantFailure(t, g.Msg(filepath.Join(t.TempDir(), "absent")), "illisible")
	wantFailure(t, g.Msg(t.TempDir()), "illisible")
}

func TestMsgNilTermsInSecretsMode(t *testing.T) {
	if err := (&Guard{Stderr: &bytes.Buffer{}}).Msg("/nonexistent"); err != nil {
		t.Fatal("mode secrets : aucun terme à contrôler")
	}
}
