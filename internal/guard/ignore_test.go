package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseIgnore(t *testing.T) {
	got, err := ParseIgnore([]string{"bun.lock", "/web/package-lock.json"})
	if err != nil || strings.Join(got, " ") != "bun.lock /web/package-lock.json" {
		t.Fatalf("%v, %v", got, err)
	}
	for _, bad := range []string{"", "/", "**/bun.lock", "[x"} {
		if _, err := ParseIgnore([]string{"ok", bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestIgnoredMatching(t *testing.T) {
	g := &Guard{Ignore: []string{"bun.lock", "web/*.json", "/top.lock"}}
	for name, want := range map[string]bool{
		"bun.lock": true, "sub/deep/bun.lock": true, "bun.lockb": false,
		"top.lock": true, "sub/top.lock": false, "web/package.json": true, "web/sub/package.json": false, "x/web/package.json": false,
	} {
		if g.ignored(name) != want {
			t.Errorf("%s: want %v", name, want)
		}
	}
}

// An ignored file holding a term passes staged, push and all; the same term elsewhere, or in the
// ignored file's name, still blocks, and the scanner still runs.
func TestIgnoreKeepsContentOutOfTermsOnly(t *testing.T) {
	dir := repo(t)
	base := git(t, dir, "rev-parse", "HEAD")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"bun.lock", "sub/bun.lock", "web.json"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("sha512-client zed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git(t, dir, "add", "bun.lock", "sub/bun.lock")
	s := &scans{}
	g := newGuard(t, dir, s)
	g.Ignore = []string{"bun.lock"}
	if err := g.Staged(); err != nil || len(s.calls) != 1 {
		t.Fatalf("staged: %v, scanner %v", err, s.calls)
	}
	git(t, dir, "commit", "-q", "-m", "lock")
	head := git(t, dir, "rev-parse", "HEAD")
	if err := g.Push(strings.NewReader("refs/heads/main "+head+" refs/heads/main "+base+"\n"), ""); err != nil {
		t.Fatalf("push: %v", err)
	}
	if err := g.All(); err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(s.calls) != 3 {
		t.Fatalf("scanner calls: %v", s.calls)
	}
	// Not ignored: blocks.
	git(t, dir, "add", "web.json")
	wantFailure(t, g.Staged(), "web.json")
	git(t, dir, "commit", "-q", "-m", "json")
	wantCause(t, g.All(), "content of the history")
	git(t, dir, "reset", "-q", "--hard", head)
	// A root-only pattern does not reach a nested file.
	g.Ignore = []string{"/bun.lock"}
	wantCause(t, g.All(), "content of the history")
	// An ignored file whose name holds a term is still refused.
	g.Ignore = []string{"*.lock"}
	if err := os.WriteFile(filepath.Join(dir, "acmecorp.lock"), []byte("clean\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "acmecorp.lock")
	wantFailure(t, g.Staged(), "<masked name>")
}
