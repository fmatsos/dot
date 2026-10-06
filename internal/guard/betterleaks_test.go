package guard

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// serve returns a server that serves archive at the path of the release and counts the requests.
func serve(t *testing.T, archive []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if r.URL.Path != "/v9.9.9/bl_linux_x64.tar.gz" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(archive)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func release(srv *httptest.Server, pin string) Release {
	return Release{Version: "9.9.9", BaseURL: srv.URL, Assets: map[string]Asset{
		"linux-x64": {Name: "bl_linux_x64.tar.gz", SHA256: pin},
	}}
}

func TestEnsureDownloadsAndVerifies(t *testing.T) {
	archive := tarGz(t, map[string]string{"LICENSE": "x", "betterleaks": "#!/bin/sh\nexit 0\n"})
	srv, hits := serve(t, archive)
	cache := t.TempDir()
	bin, err := release(srv, sum(archive)).Ensure(cache, "linux-x64", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if bin != filepath.Join(cache, "betterleaks-9.9.9", "betterleaks") {
		t.Fatalf("chemin = %s", bin)
	}
	fi, err := os.Stat(bin)
	if err != nil || fi.Mode()&0o111 == 0 {
		t.Fatalf("binaire non exécutable : %v, %v", fi, err)
	}
	if b, _ := os.ReadFile(bin); string(b) != "#!/bin/sh\nexit 0\n" {
		t.Fatalf("contenu = %q", b)
	}
	// Second call: the cached copy is used, nothing is downloaded again.
	if _, err := release(srv, sum(archive)).Ensure(cache, "linux-x64", srv.Client()); err != nil || hits.Load() != 1 {
		t.Fatalf("cache : %v, %d requêtes", err, hits.Load())
	}
}

func TestEnsureRefusesWrongSum(t *testing.T) {
	archive := tarGz(t, map[string]string{"betterleaks": "x"})
	srv, _ := serve(t, archive)
	cache := t.TempDir()
	_, err := release(srv, sum([]byte("autre"))).Ensure(cache, "linux-x64", srv.Client())
	if err == nil || !strings.Contains(err.Error(), "empreinte sha256 inattendue") {
		t.Fatal(err)
	}
	if _, serr := os.Stat(filepath.Join(cache, "betterleaks-9.9.9", "betterleaks")); serr == nil {
		t.Fatal("un binaire non vérifié ne doit pas être installé")
	}
}

func TestEnsureRefusesMissingSumWithoutDownloading(t *testing.T) {
	srv, hits := serve(t, nil)
	_, err := release(srv, "").Ensure(t.TempDir(), "linux-x64", srv.Client())
	if err == nil || err.Error() != "betterleaks : empreinte non figée pour linux-x64" {
		t.Fatal(err)
	}
	if hits.Load() != 0 {
		t.Fatal("aucun téléchargement sans empreinte")
	}
}

func TestEnsureUnknownPlatform(t *testing.T) {
	srv, _ := serve(t, nil)
	if _, err := release(srv, "").Ensure(t.TempDir(), "plan9-mips", srv.Client()); err == nil || !strings.Contains(err.Error(), "plateforme non supportée") {
		t.Fatal(err)
	}
}

func TestEnsureArchiveWithoutBinary(t *testing.T) {
	archive := tarGz(t, map[string]string{"README": "x", "../betterleaks-evil": "x"})
	srv, _ := serve(t, archive)
	if _, err := release(srv, sum(archive)).Ensure(t.TempDir(), "linux-x64", srv.Client()); err == nil || !strings.Contains(err.Error(), "binaire absent") {
		t.Fatal(err)
	}
}

func TestEnsureHTTPErrorAndBadArchive(t *testing.T) {
	notGz := []byte("pas une archive")
	srv, _ := serve(t, notGz)
	if _, err := release(srv, sum(notGz)).Ensure(t.TempDir(), "linux-x64", srv.Client()); err == nil || !strings.Contains(err.Error(), "archive illisible") {
		t.Fatal(err)
	}
	r := release(srv, "00")
	r.Version = "0.0.1" // 404
	if _, err := r.Ensure(t.TempDir(), "linux-x64", srv.Client()); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatal(err)
	}
}

func TestBetterleaksTableIsNeverInvented(t *testing.T) {
	for p, a := range Betterleaks.Assets {
		if a.SHA256 != "" && len(a.SHA256) != 64 {
			t.Errorf("%s : somme mal formée", p)
		}
	}
	if _, ok := Betterleaks.Assets[Platform()]; !ok {
		t.Skipf("plateforme %s hors de la table", Platform())
	}
}
