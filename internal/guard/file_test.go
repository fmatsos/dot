package guard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileSkipsMediaContentButNotItsNameNorSecrets(t *testing.T) {
	dir := t.TempDir()
	s := &scans{}
	g := newGuard(t, dir, s)
	img := filepath.Join(dir, "logo.webp")
	if err := os.WriteFile(img, []byte("RIFF\x00acmecorp\x00WEBP"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := g.File(img, "~/logo.webp"); err != nil {
		t.Errorf("media bytes must not be matched as text: %v", err)
	}
	if len(s.calls) != 1 || s.calls[0][0] != "dir" {
		t.Errorf("the secret scan must still read the media: %v", s.calls)
	}
	txt := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txt, []byte("tenant acmecorp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantFailure(t, g.File(txt, "~/notes.txt"), "contenu")
	wantFailure(t, g.File(img, "~/acmecorp.webp"), "nom")
}
