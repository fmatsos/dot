package guard

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Asset is one release archive of the scanner and the sha256 it must have.
type Asset struct {
	Name   string // archive file name inside the release
	SHA256 string // hex; empty = not pinned yet, the guard then refuses to run
}

// Release describes the pinned scanner release, per platform (linux-x64, macos-arm64…).
type Release struct {
	Version string
	BaseURL string // …/releases/download ; the archive is <BaseURL>/v<Version>/<Name>
	Assets  map[string]Asset
}

const (
	maxArchive = 200 << 20 // download bound
	maxBinary  = 400 << 20 // extracted binary bound
)

// Betterleaks is the release dot guard runs. The sums are compiled in: a mismatch or an empty
// entry fails closed. The v1.9.0 sums were recomputed from the downloaded archives and match
// the release's checksums.txt; bump them with scripts/pin-betterleaks.sh <version>.
var Betterleaks = Release{
	Version: "1.9.0",
	BaseURL: "https://github.com/betterleaks/betterleaks/releases/download",
	Assets: map[string]Asset{
		"linux-x64":   {Name: "betterleaks_1.9.0_linux_x64.tar.gz", SHA256: "f8b185a39ffcece2a1ca82bf3a4e7435cd81963ffd16b7a9128daf75f35f6de7"},
		"linux-arm64": {Name: "betterleaks_1.9.0_linux_arm64.tar.gz", SHA256: "1d39116e0a58dc94574715e2aa12a2dbd5062f193eee3fec011fef6ba06bd13b"},
		"macos-x64":   {Name: "betterleaks_1.9.0_darwin_x64.tar.gz", SHA256: "68dfd83458d9e7f90663daa7a81c7f944083b170dae82e4f5f8d0581ed519ef1"},
		"macos-arm64": {Name: "betterleaks_1.9.0_darwin_arm64.tar.gz", SHA256: "83dd7eaab13d44a1e8e347a17064cdef153a1478db473276d41b38d55613d3f2"},
	},
}

// Platform names the running OS and architecture the way the release table does.
func Platform() string {
	goos, arch := runtime.GOOS, runtime.GOARCH
	if goos == "darwin" {
		goos = "macos"
	}
	if arch == "amd64" {
		arch = "x64"
	}
	return goos + "-" + arch
}

// Ensure returns the path of the pinned scanner under cacheRoot (~/.cache/dot), downloading and
// verifying it when it is not there yet.
// ponytail: a cached binary is trusted as is; the sum is checked when it is fetched, not on every run.
func (r Release) Ensure(cacheRoot, platform string, client *http.Client) (string, error) {
	a, ok := r.Assets[platform]
	if !ok {
		return "", fmt.Errorf("betterleaks : plateforme non supportée (%s)", platform)
	}
	if a.SHA256 == "" {
		return "", fmt.Errorf("betterleaks : empreinte non figée pour %s", platform)
	}
	dir := filepath.Join(cacheRoot, "betterleaks-"+r.Version)
	bin := filepath.Join(dir, "betterleaks")
	if fi, err := os.Stat(bin); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o100 != 0 {
		return bin, nil
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	if err := r.fetch(client, a, dir, bin); err != nil {
		return "", err
	}
	return bin, nil
}

func (r Release) fetch(client *http.Client, a Asset, dir, bin string) error {
	url := strings.TrimRight(r.BaseURL, "/") + "/v" + r.Version + "/" + a.Name
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("betterleaks : téléchargement impossible (%s)", a.Name)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("betterleaks : téléchargement refusé (%s, HTTP %d)", a.Name, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArchive+1))
	if err != nil || len(data) > maxArchive {
		return fmt.Errorf("betterleaks : archive illisible ou trop grande (%s)", a.Name)
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), a.SHA256) {
		return fmt.Errorf("betterleaks : empreinte sha256 inattendue pour %s, refus", a.Name)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("betterleaks : %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".betterleaks-*")
	if err != nil {
		return fmt.Errorf("betterleaks : %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	err = extract(data, tmp)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("betterleaks : %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return fmt.Errorf("betterleaks : %w", err)
	}
	if err := os.Rename(tmp.Name(), bin); err != nil {
		return fmt.Errorf("betterleaks : %w", err)
	}
	return nil
}

// extract copies the regular file named betterleaks out of a tar.gz held in memory.
func extract(archive []byte, dst io.Writer) error {
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return errors.New("archive illisible")
	}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return errors.New("binaire absent de l'archive")
		}
		if err != nil {
			return errors.New("archive illisible")
		}
		if h.Typeflag != tar.TypeReg || path.Base(h.Name) != "betterleaks" {
			continue
		}
		n, err := io.Copy(dst, io.LimitReader(tr, maxBinary+1))
		if err != nil || n > maxBinary {
			return errors.New("binaire illisible ou trop grand")
		}
		return nil
	}
}
