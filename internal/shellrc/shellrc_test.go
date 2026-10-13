package shellrc

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const blk = begin + "\nx\n" + end + "\n"

func TestSplice(t *testing.T) {
	sk := "# >>> shellkit >>>\nsk\n" + after + "\n"
	for name, c := range map[string]struct{ in, want string }{
		"empty file":       {"", blk},
		"top":              {"alias a=b\n", blk + "\nalias a=b\n"},
		"after shellkit":   {sk + "\nmine\n", sk + "\n" + blk + "\nmine\n"},
		"only shellkit":    {sk, sk + "\n" + blk},
		"replace in place": {sk + "\n" + begin + "\nold\n" + end + "\n\nmine\n", sk + "\n" + blk + "\nmine\n"},
		"no final newline": {"mine", blk + "\nmine"},
	} {
		got, err := Splice(c.in, blk)
		if err != nil || got != c.want {
			t.Errorf("%s: %q, %v; want %q", name, got, err, c.want)
		}
		again, _ := Splice(got, blk)
		if again != got {
			t.Errorf("%s: not idempotent: %q", name, again)
		}
	}
	if _, err := Splice("a\n"+begin+"\nb\n", blk); err == nil {
		t.Error("begin without end must fail")
	}
}

func run(t *testing.T, home string, dry bool, have ...string) (out, errw string) {
	t.Helper()
	var o, e bytes.Buffer
	look := func(n string) (string, error) {
		for _, h := range have {
			if h == n {
				return n, nil
			}
		}
		return "", errors.New("absent")
	}
	if err := Ensure(Options{Home: home, Dry: dry, Out: &o, Err: &e, LookPath: look}); err != nil {
		t.Fatal(err)
	}
	return o.String(), e.String()
}

func TestEnsure(t *testing.T) {
	home := t.TempDir()
	zsh, bash := filepath.Join(home, ".zshrc"), filepath.Join(home, ".bashrc")

	run(t, home, true, "zsh", "bash")
	if _, err := os.Stat(zsh); err == nil {
		t.Fatal("dry run wrote a file")
	}
	run(t, home, false, "zsh") // bash not installed: no .bashrc
	if _, err := os.Stat(bash); err == nil {
		t.Fatal(".bashrc created without bash")
	}
	z, _ := os.ReadFile(zsh)
	if !strings.Contains(string(z), "(sh|zsh)(N)") || !strings.Contains(string(z), "mise/shell.sh") {
		t.Fatalf("zsh block: %q", z)
	}

	os.WriteFile(bash, []byte("export MINE=1\n"), 0o600)
	run(t, home, false, "zsh", "bash")
	b, _ := os.ReadFile(bash)
	if !strings.HasPrefix(string(b), begin) || !strings.HasSuffix(string(b), "export MINE=1\n") || !strings.Contains(string(b), "*.sh|*.bash") {
		t.Fatalf("bash block: %q", b)
	}
	if fi, _ := os.Stat(bash); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	if out, _ := run(t, home, false, "zsh", "bash"); out != "" {
		t.Fatalf("second run rewrote: %q", out)
	}
}

func TestEnsureSkipsSymlink(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	target := filepath.Join(repo, "zshrc")
	os.WriteFile(target, []byte("shellkit\n"), 0o644)
	os.Symlink(target, filepath.Join(home, ".zshrc"))
	_, errw := run(t, home, false, "zsh")
	if got, _ := os.ReadFile(target); string(got) != "shellkit\n" {
		t.Fatalf("written through the link: %q", got)
	}
	if !strings.Contains(errw, "est un lien") {
		t.Fatalf("no warning: %q", errw)
	}
}

func TestBlockNodes(t *testing.T) {
	got := block("loop", []string{"22.11.0", "20.0.0"})
	for _, w := range []string{`PATH="$PATH:$HOME/.nvm/versions/node/v22.11.0/bin"`, `/node/v20.0.0/bin"`} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in %q", w, got)
		}
	}
	if strings.Contains(block("loop", nil), ".nvm") {
		t.Error("nvm line without a node")
	}
}
