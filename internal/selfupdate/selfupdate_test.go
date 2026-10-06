package selfupdate

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var newBin = []byte("#!/bin/sh\necho new\n")

type fixture struct {
	srv  *httptest.Server
	exe  string
	out  *bytes.Buffer
	opts Options
	hits map[string]int
}

// setup serves v1.2.3 signed with a fresh key and installs that key as the embedded one.
func setup(t *testing.T, mutate func(sums, sig, bin *[]byte)) *fixture {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	old := releaseKey
	releaseKey = base64.StdEncoding.EncodeToString(pub) + "\n"
	t.Cleanup(func() { releaseKey = old })

	h := sha256.Sum256(newBin)
	sums := []byte(hex.EncodeToString(h[:]) + "  dot-linux-x64\n" + strings.Repeat("0", 64) + "  dot-macos-x64\n")
	sig := ed25519.Sign(priv, sums)
	bin := append([]byte{}, newBin...)
	if mutate != nil {
		mutate(&sums, &sig, &bin)
	}
	f := &fixture{out: &bytes.Buffer{}, hits: map[string]int{}}
	mux := http.NewServeMux()
	serve := func(path string, b []byte) {
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			f.hits[path]++
			w.Write(b)
		})
	}
	serve("/v1.2.3/SHA256SUMS", sums)
	serve("/v1.2.3/SHA256SUMS.sig", sig)
	serve("/v1.2.3/dot-linux-x64", bin)
	serve("/latest", []byte(`{"tag_name":"v1.2.3"}`))
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)

	dir := t.TempDir()
	real := filepath.Join(dir, "real-dot")
	if err := os.WriteFile(real, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.exe = filepath.Join(dir, "dot")
	if err := os.Symlink(real, f.exe); err != nil {
		t.Fatal(err)
	}
	f.opts = Options{
		Current: "v1.0.0", Target: "v1.2.3", Out: f.out, Exe: f.exe,
		BaseURL: f.srv.URL, APIURL: f.srv.URL + "/latest",
		Platform: func() (string, string) { return "linux", "amd64" },
	}
	return f
}

func content(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUpdateOK(t *testing.T) {
	f := setup(t, nil)
	if err := Run(f.opts); err != nil {
		t.Fatal(err)
	}
	if got := content(t, f.exe); got != string(newBin) {
		t.Fatalf("binaire = %q", got)
	}
	real, _ := filepath.EvalSymlinks(f.exe)
	if fi, _ := os.Lstat(f.exe); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("le lien symbolique a été remplacé")
	}
	if fi, _ := os.Stat(real); fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v", fi.Mode())
	}
	entries, _ := os.ReadDir(filepath.Dir(real))
	if len(entries) != 2 {
		t.Fatalf("fichier temporaire laissé : %v", entries)
	}
}

func TestLatest(t *testing.T) {
	f := setup(t, nil)
	f.opts.Target = ""
	if err := Run(f.opts); err != nil {
		t.Fatal(err)
	}
	if content(t, f.exe) != string(newBin) {
		t.Fatal("pas mis à jour")
	}
}

func assertRefused(t *testing.T, f *fixture, want string) {
	t.Helper()
	err := Run(f.opts)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("erreur = %v, attendu %q", err, want)
	}
	if got := content(t, f.exe); got != "old" {
		t.Fatalf("exécutable modifié : %q", got)
	}
}

func TestBadSignature(t *testing.T) {
	f := setup(t, func(_, sig, _ *[]byte) { (*sig)[0] ^= 1 })
	assertRefused(t, f, "signature")
}

func TestSignatureOfOtherContent(t *testing.T) {
	f := setup(t, func(sums, _, _ *[]byte) { *sums = append(*sums, []byte("# extra\n")...) })
	assertRefused(t, f, "signature")
}

func TestTamperedBinary(t *testing.T) {
	f := setup(t, func(_, _, bin *[]byte) { *bin = []byte("evil") })
	assertRefused(t, f, "checksum invalide")
}

func TestMissingAssetLine(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	f := setup(t, nil)
	releaseKey = base64.StdEncoding.EncodeToString(pub)
	// Serve a signed SHA256SUMS that lacks dot-linux-x64.
	sums := []byte(strings.Repeat("0", 64) + "  dot-macos-x64\n")
	sig := ed25519.Sign(priv, sums)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.2.3/SHA256SUMS", func(w http.ResponseWriter, _ *http.Request) { w.Write(sums) })
	mux.HandleFunc("/v1.2.3/SHA256SUMS.sig", func(w http.ResponseWriter, _ *http.Request) { w.Write(sig) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	f.opts.BaseURL = srv.URL
	assertRefused(t, f, "absent de SHA256SUMS")
}

func TestEmptyOrInvalidKey(t *testing.T) {
	for _, k := range []string{"", "  \n", "!!!notbase64", base64.StdEncoding.EncodeToString([]byte("short"))} {
		f := setup(t, nil)
		releaseKey = k
		assertRefused(t, f, "mise à jour refusée")
		if len(f.hits) != 0 {
			t.Fatalf("réseau touché sans clé valide : %v", f.hits)
		}
	}
}

func TestAlreadyUpToDate(t *testing.T) {
	f := setup(t, nil)
	f.opts.Current = "v1.2.3"
	if err := Run(f.opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "déjà en v1.2.3") || content(t, f.exe) != "old" {
		t.Fatalf("sortie %q", f.out.String())
	}
	if f.hits["/v1.2.3/dot-linux-x64"] != 0 {
		t.Fatal("binaire téléchargé")
	}
}

func TestDryRun(t *testing.T) {
	f := setup(t, nil)
	f.opts.Dry = true
	if err := Run(f.opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "[dry]") || content(t, f.exe) != "old" {
		t.Fatalf("sortie %q", f.out.String())
	}
	if len(f.hits) != 0 {
		t.Fatalf("téléchargements en dry run : %v", f.hits)
	}
}

func TestInvalidTarget(t *testing.T) {
	for _, tag := range []string{"1.2.3", "v1.2", "v1.2.3/../x", "latest"} {
		f := setup(t, nil)
		f.opts.Target = tag
		assertRefused(t, f, "version invalide")
	}
}

func TestHTTPError(t *testing.T) {
	f := setup(t, nil)
	f.opts.Target = "v9.9.9"
	assertRefused(t, f, "HTTP 404")
}

func TestAsset(t *testing.T) {
	for in, want := range map[string]string{
		"linux/amd64": "linux-x64", "linux/arm64": "linux-arm64",
		"darwin/amd64": "macos-x64", "darwin/arm64": "macos-arm64", "windows/amd64": "",
	} {
		goos, goarch, _ := strings.Cut(in, "/")
		if got := Asset(goos, goarch); got != want {
			t.Errorf("%s = %q", in, got)
		}
	}
}

func TestEmbeddedKeyPlaceholder(t *testing.T) {
	if _, err := publicKey(); err != nil && strings.TrimSpace(releaseKey) != "" {
		t.Fatalf("clé commitée invalide : %v", err)
	}
}
