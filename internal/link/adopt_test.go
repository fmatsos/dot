package link

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdoptSource(t *testing.T) {
	home := t.TempDir()
	mk := func(rel string) string {
		p := filepath.Join(home, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := mk(".config/a/rc")
	if abs, rel, err := AdoptSource(home, "/", good); err != nil || abs != good || rel != ".config/a/rc" {
		t.Fatalf("%q %q %v", abs, rel, err)
	}
	if _, rel, err := AdoptSource(home, home, ".config/../.config/a/rc"); err != nil || rel != ".config/a/rc" {
		t.Fatalf("relatif : %q %v", rel, err)
	}
	_ = os.Symlink(good, filepath.Join(home, "ln"))
	_ = os.Symlink(filepath.Join(home, ".config"), filepath.Join(home, "dirln"))
	for _, bad := range []string{
		filepath.Join(home, "..", "x"), home, filepath.Join(home, ".dot", "p"), mk(".dot/p/f"),
		filepath.Join(home, "ln"), filepath.Join(home, ".config"), filepath.Join(home, "absent"),
		filepath.Join(home, "dirln", "a", "rc"),
	} {
		if _, _, err := AdoptSource(home, home, bad); err == nil {
			t.Errorf("%s accepté", bad)
		}
	}
}

func TestAdoptMoveAndRollback(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "home", "f")
	_ = os.MkdirAll(filepath.Dir(src), 0o755)
	if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(d, "clone", "home", "a", "b", "f")
	if err := AdoptMove(src, dst, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.Readlink(src); got != dst {
		t.Fatalf("lien %q", got)
	}
	if fi, _ := os.Stat(dst); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	// A failing symlink (src's place taken by a directory after the move is impossible to fake,
	// so make src's parent read-only): the file comes back and the new directories go.
	src2 := filepath.Join(d, "ro", "g")
	_ = os.MkdirAll(filepath.Dir(src2), 0o755)
	_ = os.WriteFile(src2, []byte("y"), 0o644)
	dst2 := filepath.Join(d, "clone2", "home", "g")
	if err := os.Chmod(filepath.Dir(src2), 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filepath.Dir(src2), 0o755)
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	if err := AdoptMove(src2, dst2, true); err == nil {
		t.Fatal("succès inattendu")
	}
	if b, err := os.ReadFile(src2); err != nil || string(b) != "y" {
		t.Fatalf("fichier perdu : %v", err)
	}
}

func TestClaimedBy(t *testing.T) {
	d := t.TempDir()
	_ = os.MkdirAll(filepath.Join(d, "a", "home", ".x"), 0o755)
	_ = os.WriteFile(filepath.Join(d, "a", "home", ".x", "f"), nil, 0o644)
	got := ClaimedBy(".x/f", []Profile{{"a", filepath.Join(d, "a")}, {"b", filepath.Join(d, "b")}})
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("%v", got)
	}
}
