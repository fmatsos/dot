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
		t.Fatalf("binary = %q", got)
	}
	real, _ := filepath.EvalSymlinks(f.exe)
	if fi, _ := os.Lstat(f.exe); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced")
	}
	if fi, _ := os.Stat(real); fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v", fi.Mode())
	}
	entries, _ := os.ReadDir(filepath.Dir(real))
	if len(entries) != 2 {
		t.Fatalf("temp file left behind: %v", entries)
	}
}

func TestLatest(t *testing.T) {
	f := setup(t, nil)
	f.opts.Target = ""
	if err := Run(f.opts); err != nil {
		t.Fatal(err)
	}
	if content(t, f.exe) != string(newBin) {
		t.Fatal("not updated")
	}
}

// assertRefused wants a refusal that leaves the executable alone. The cause is told by the network
// state the callers check (what was fetched before the refusal), not by the wording.
func assertRefused(t *testing.T, f *fixture) {
	t.Helper()
	if err := Run(f.opts); err == nil {
		t.Fatal("update accepted")
	}
	if got := content(t, f.exe); got != "old" {
		t.Fatalf("executable modified: %q", got)
	}
}

func TestBadSignature(t *testing.T) {
	f := setup(t, func(_, sig, _ *[]byte) { (*sig)[0] ^= 1 })
	assertRefused(t, f)
	if f.hits["/v1.2.3/dot-linux-x64"] != 0 { // refused on the signature, before the binary is fetched
		t.Fatalf("binary downloaded despite an invalid signature: %v", f.hits)
	}
}

func TestSignatureOfOtherContent(t *testing.T) {
	f := setup(t, func(sums, _, _ *[]byte) { *sums = append(*sums, []byte("# extra\n")...) })
	assertRefused(t, f)
	if f.hits["/v1.2.3/dot-linux-x64"] != 0 {
		t.Fatalf("binary downloaded despite an invalid signature: %v", f.hits)
	}
}

func TestTamperedBinary(t *testing.T) {
	f := setup(t, func(_, _, bin *[]byte) { *bin = []byte("evil") })
	assertRefused(t, f)
	if f.hits["/v1.2.3/dot-linux-x64"] != 1 { // the signature passed: refused on the checksum of the fetched binary
		t.Fatalf("hits: %v", f.hits)
	}
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
	binHits := 0
	mux.HandleFunc("/v1.2.3/dot-linux-x64", func(w http.ResponseWriter, _ *http.Request) { binHits++; w.Write(newBin) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	f.opts.BaseURL = srv.URL
	assertRefused(t, f)
	if binHits != 0 { // no line for the asset: refused before the binary is fetched
		t.Fatal("binary downloaded without a signed sum")
	}
}

func TestEmptyOrInvalidKey(t *testing.T) {
	for _, k := range []string{"", "  \n", "!!!notbase64", base64.StdEncoding.EncodeToString([]byte("short"))} {
		f := setup(t, nil)
		releaseKey = k
		assertRefused(t, f)
		if len(f.hits) != 0 {
			t.Fatalf("network touched without a valid key: %v", f.hits)
		}
	}
}

func TestAlreadyUpToDate(t *testing.T) {
	f := setup(t, nil)
	f.opts.Current = "v1.2.3"
	if err := Run(f.opts); err != nil {
		t.Fatal(err)
	}
	if f.out.Len() == 0 || content(t, f.exe) != "old" {
		t.Fatalf("output %q", f.out.String())
	}
	if f.hits["/v1.2.3/dot-linux-x64"] != 0 {
		t.Fatal("binary downloaded")
	}
}

func TestDryRun(t *testing.T) {
	f := setup(t, nil)
	f.opts.Dry = true
	if err := Run(f.opts); err != nil {
		t.Fatal(err)
	}
	if f.out.Len() == 0 || content(t, f.exe) != "old" {
		t.Fatalf("output %q", f.out.String())
	}
	if len(f.hits) != 0 {
		t.Fatalf("downloads in a dry run: %v", f.hits)
	}
}

func TestInvalidTarget(t *testing.T) {
	for _, tag := range []string{"1.2.3", "v1.2", "v1.2.3/../x", "latest"} {
		f := setup(t, nil)
		f.opts.Target = tag
		assertRefused(t, f)
		if len(f.hits) != 0 { // refused before any request
			t.Fatalf("%q: network touched: %v", tag, f.hits)
		}
	}
}

func TestHTTPError(t *testing.T) {
	f := setup(t, nil)
	f.opts.Target = "v9.9.9"
	assertRefused(t, f)
	if f.hits["/v1.2.3/dot-linux-x64"] != 0 {
		t.Fatalf("binary downloaded: %v", f.hits)
	}
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
		t.Fatalf("committed key invalid: %v", err)
	}
}
