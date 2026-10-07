// Package selfupdate replaces the running dot binary with a release whose SHA256SUMS is signed
// by the release key (Ed25519) compiled into the binary.
package selfupdate

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	DefaultBase = "https://github.com/fmatsos/dot/releases/download"
	DefaultAPI  = "https://api.github.com/repos/fmatsos/dot/releases/latest"

	maxDownload = 256 << 20
	maxSmall    = 1 << 20 // SHA256SUMS, signature, API answer
)

// releaseKey is the base64 of the raw 32-byte Ed25519 public key; empty until a key is generated.
// Tests replace it; there is deliberately no way to override it at run time.
//
//go:embed release.pub
var releaseKey string

var tagRe = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// Options drives Run.
type Options struct {
	Current string // running version (main.version)
	Target  string // vX.Y.Z, empty for the latest release
	Dry     bool
	Out     io.Writer

	HTTP     *http.Client
	BaseURL  string // <base>/<tag>/<asset>
	APIURL   string // answers {"tag_name": "vX.Y.Z"}
	Exe      string // executable to replace; defaults to the running one
	Platform func() (goos, goarch string)
}

// Asset is the release asset name of a platform, or "" when unsupported.
func Asset(goos, goarch string) string {
	switch goos + "/" + goarch {
	case "linux/amd64":
		return "linux-x64"
	case "linux/arm64":
		return "linux-arm64"
	case "darwin/arm64":
		return "macos-arm64"
	case "darwin/amd64":
		return "macos-x64"
	}
	return ""
}

func publicKey() (ed25519.PublicKey, error) {
	s := strings.TrimSpace(releaseKey)
	if s == "" {
		return nil, errors.New("clé de signature des releases absente de ce binaire : mise à jour refusée")
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("clé de signature des releases invalide dans ce binaire : mise à jour refusée")
	}
	return ed25519.PublicKey(raw), nil
}

// Run updates the executable to the target release, failing closed on any verification problem.
func Run(o Options) error {
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: 5 * time.Minute}
	}
	if o.BaseURL == "" {
		o.BaseURL = DefaultBase
	}
	if o.APIURL == "" {
		o.APIURL = DefaultAPI
	}
	if o.Platform == nil {
		o.Platform = func() (string, string) { return runtime.GOOS, runtime.GOARCH }
	}
	key, err := publicKey()
	if err != nil {
		return err
	}
	goos, goarch := o.Platform()
	asset := Asset(goos, goarch)
	if asset == "" {
		return fmt.Errorf("plateforme non gérée (%s-%s)", goos, goarch)
	}
	tag := o.Target
	if tag == "" {
		if tag, err = latest(o); err != nil {
			return err
		}
	}
	if !tagRe.MatchString(tag) {
		return fmt.Errorf("version invalide : %q (attendu vX.Y.Z)", tag)
	}
	if tag == o.Current {
		fmt.Fprintf(o.Out, "dot est déjà en %s\n", tag)
		return nil
	}
	exe := o.Exe
	if exe == "" {
		if exe, err = os.Executable(); err != nil {
			return err
		}
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	if o.Dry {
		fmt.Fprintf(o.Out, "[dry] remplacerait %s (%s) par dot-%s %s, après vérification de la signature\n", exe, o.Current, asset, tag)
		return nil
	}

	base := strings.TrimRight(o.BaseURL, "/") + "/" + tag + "/"
	sums, err := download(o.HTTP, base+"SHA256SUMS", maxSmall)
	if err != nil {
		return fmt.Errorf("SHA256SUMS : %w", err)
	}
	sig, err := download(o.HTTP, base+"SHA256SUMS.sig", maxSmall)
	if err != nil {
		return fmt.Errorf("SHA256SUMS.sig : %w", err)
	}
	if !ed25519.Verify(key, sums, sig) {
		return errors.New("signature de SHA256SUMS invalide : abandon")
	}
	name := "dot-" + asset
	want, err := sumFor(sums, name)
	if err != nil {
		return err
	}
	data, err := download(o.HTTP, base+name, maxDownload)
	if err != nil {
		return fmt.Errorf("%s : %w", name, err)
	}
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("%s : checksum invalide, abandon", name)
	}
	if err := replace(exe, data); err != nil {
		return err
	}
	fmt.Fprintf(o.Out, "dot mis à jour : %s → %s (%s)\n", o.Current, tag, exe)
	return nil
}

func latest(o Options) (string, error) {
	b, err := download(o.HTTP, o.APIURL, maxSmall)
	if err != nil {
		return "", fmt.Errorf("dernière release introuvable : %w", err)
	}
	var r struct {
		Tag string `json:"tag_name"`
	}
	if err := json.Unmarshal(b, &r); err != nil || r.Tag == "" {
		return "", errors.New("dernière release introuvable : réponse illisible")
	}
	return r.Tag, nil
}

// sumFor finds the sha256 of name in `sha256sum` output.
func sumFor(sums []byte, name string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || strings.TrimPrefix(f[1], "*") != name {
			continue
		}
		if _, err := hex.DecodeString(f[0]); err != nil || len(f[0]) != 64 {
			break
		}
		return strings.ToLower(f[0]), nil
	}
	return "", fmt.Errorf("%s absent de SHA256SUMS : abandon", name)
}

func download(c *http.Client, url string, limit int64) ([]byte, error) {
	resp, err := c.Get(url)
	if err != nil {
		return nil, errors.New("téléchargement impossible")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("téléchargement invalide")
	}
	return data, nil
}

// replace writes next to dst then renames over it, so dst is never half written.
func replace(dst string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(dst), ".dot-update-*")
	if err != nil {
		return err
	}
	_, werr := io.Copy(f, bytes.NewReader(data))
	cerr := f.Close()
	if err := errors.Join(werr, cerr, os.Chmod(f.Name(), 0o755)); err != nil {
		os.Remove(f.Name())
		return err
	}
	if err := os.Rename(f.Name(), dst); err != nil {
		os.Remove(f.Name())
		return err
	}
	return nil
}
