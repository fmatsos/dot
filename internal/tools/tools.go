// Package tools installs the optional developer tools a profile asks for: the pinned mise
// binary, its tools, nvm with a pinned node and global npm packages, and the Codex plugins.
package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/fmatsos/dot/internal/manifest"
)

const (
	miseVersion = "2026.10.0"
	nvmVersion  = "v0.40.4"

	defaultMiseBase = "https://github.com/jdx/mise/releases/download"
	defaultNVMRepo  = "https://github.com/nvm-sh/nvm.git"

	miseLine = `[ -r "$HOME/.config/mise/shell.sh" ] && . "$HOME/.config/mise/shell.sh"`

	// Node and packages travel as positional arguments, never interpolated in the script.
	nvmScript = `. "$HOME/.nvm/nvm.sh" && nvm install "$1" >/dev/null && shift &&
{ [ $# -eq 0 ] || npm install -g --no-fund --no-audit "$@" >/dev/null; }`

	maxDownload = 256 << 20
)

// miseVersionTimeout bounds `mise --version` on an existing binary (a var for the tests).
var miseVersionTimeout = 10 * time.Second

// miseSums maps a release asset to the sha256 of the pinned mise binary (SHASUMS256.txt).
var miseSums = map[string]string{
	"linux-x64":   "57ced973f968b8fbab07aa8e32bd7077d4a357e200a22356d98963c723c6de0a",
	"linux-arm64": "4b8cacffac83e8493fc5d1eef25f6365edba73ccbed5a1f3987b7cb3f5079656",
	"macos-arm64": "8d2007efdae0c2b64e3955257533e6ec17197bc2fdcbc5dd8f6847f92881deea",
	"macos-x64":   "815eb7872e453dcd30ed5e2e478978d2947e34106b6cd4f9cc4d4e51f7761332",
}

// Options drives Run; the seams default to the real implementations.
type Options struct {
	Home string
	Dry  bool
	Skip bool // DOTFILES_TOOLS=0: no `mise install`, no nvm (the mise binary is still fetched)

	MiseConfig   bool // a profile links ~/.config/mise/config.toml: otherwise mise is not installed
	NVM          []manifest.NVM
	Marketplaces []manifest.Marketplace

	Out, Err io.Writer

	HTTP        *http.Client
	MiseBaseURL string
	NVMRepoURL  string
	LookPath    func(string) (string, error)
	Run         func(name string, args ...string) error // output goes to Out and Err
	// Output captures stdout, for `codex plugin marketplace list`.
	Output   func(name string, args ...string) ([]byte, error)
	Platform func() (goos, goarch string)
}

type tools struct {
	Options
	settings string
}

// Run installs what the options ask for; every step is idempotent.
func Run(o Options) error {
	if o.Home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		o.Home = h
	}
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.Err == nil {
		o.Err = io.Discard
	}
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: 5 * time.Minute}
	}
	if o.MiseBaseURL == "" {
		o.MiseBaseURL = defaultMiseBase
	}
	if o.NVMRepoURL == "" {
		o.NVMRepoURL = defaultNVMRepo
	}
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	if o.Platform == nil {
		o.Platform = func() (string, string) { return runtime.GOOS, runtime.GOARCH }
	}
	if o.Run == nil {
		o.Run = func(name string, args ...string) error {
			c := exec.Command(name, args...)
			c.Stdout, c.Stderr = o.Out, o.Err
			c.Env = append(os.Environ(), "HOME="+o.Home)
			return c.Run()
		}
	}
	if o.Output == nil {
		o.Output = func(name string, args ...string) ([]byte, error) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			c := exec.CommandContext(ctx, name, args...)
			c.Env = append(os.Environ(), "HOME="+o.Home)
			return c.Output()
		}
	}
	t := &tools{Options: o, settings: filepath.Join(o.Home, ".config", "shkit", "settings.sh")}
	if t.MiseConfig {
		if err := t.mise(); err != nil {
			return err
		}
	}
	if !t.Skip {
		if err := t.nvm(); err != nil {
			return err
		}
	}
	return t.plugins()
}

func (t *tools) say(format string, a ...any)  { fmt.Fprintf(t.Out, format+"\n", a...) }
func (t *tools) warn(format string, a ...any) { fmt.Fprintf(t.Err, format+"\n", a...) }

// tilde shortens a path under Home for messages.
func (t *tools) tilde(p string) string {
	if rest, ok := strings.CutPrefix(p, t.Home); ok && (rest == "" || rest[0] == '/') {
		return "~" + rest
	}
	return p
}

// run mirrors the bash helper: print the command in a dry run, execute it otherwise.
func (t *tools) run(name string, args ...string) error {
	if t.Dry {
		t.say("  [dry] %s", strings.Join(append([]string{name}, args...), " "))
		return nil
	}
	return t.Run(name, args...)
}

func assetFor(goos, goarch string) string {
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

func executable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0
}

func (t *tools) mise() error {
	bin := filepath.Join(t.Home, ".local", "bin", "mise")
	if !executable(bin) || !t.miseCurrent(bin) {
		if err := t.fetchMise(bin); err != nil {
			return err
		}
	}
	cfg := filepath.Join(t.Home, ".config", "mise", "config.toml")
	if _, err := os.Stat(cfg); err == nil && !t.Skip && executable(bin) {
		if err := t.run(bin, "install", "-y"); err != nil {
			return fmt.Errorf("mise install : %w", err)
		}
	}
	return t.activate()
}

// miseCurrent reports whether the existing binary is the pinned release, by its --version output.
// Otherwise (other version, failure, timeout) it says why and the caller downloads and verifies again.
// ponytail: trusts the binary's own word on its version; the sha256 check only covers what we download.
func (t *tools) miseCurrent(bin string) bool {
	type result struct {
		out []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		out, err := t.Output(bin, "--version")
		ch <- result{out, err}
	}()
	found := ""
	select {
	case r := <-ch:
		if f := strings.Fields(string(r.out)); r.err == nil && len(f) > 0 {
			if slices.ContainsFunc(f, func(x string) bool { return strings.TrimPrefix(x, "v") == miseVersion }) {
				return true
			}
			// The word comes from a foreign executable: no control characters, bounded length.
			found = strings.Map(func(r rune) rune {
				if unicode.IsControl(r) {
					return -1
				}
				return r
			}, f[0])
			found = string([]rune(found)[:min(utf8.RuneCountInString(found), 32)])
		}
	case <-time.After(miseVersionTimeout):
	}
	if found == "" {
		t.say("  mise : version illisible, %s attendue : mise à jour", miseVersion)
	} else {
		t.say("  mise : version %s trouvée, %s attendue : mise à jour", found, miseVersion)
	}
	return false
}

func (t *tools) fetchMise(bin string) error {
	goos, goarch := t.Platform()
	asset := assetFor(goos, goarch)
	if asset == "" {
		t.warn("  mise ignoré : plateforme non gérée (%s-%s)", goos, goarch)
		return nil
	}
	if t.Dry {
		t.say("  [dry] installer mise %s (%s) dans %s", miseVersion, asset, t.tilde(bin))
		return nil
	}
	t.say("  mise %s (%s) → %s", miseVersion, asset, t.tilde(bin))
	data, err := t.download(fmt.Sprintf("%s/v%s/mise-v%[2]s-%s", strings.TrimRight(t.MiseBaseURL, "/"), miseVersion, asset))
	if err != nil {
		return errors.New("mise : téléchargement impossible")
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != miseSums[asset] {
		return errors.New("mise : checksum invalide, abandon")
	}
	return writeAtomic(bin, data)
}

func (t *tools) download(url string) ([]byte, error) {
	resp, err := t.HTTP.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil || len(data) > maxDownload {
		return nil, errors.New("téléchargement invalide")
	}
	return data, nil
}

// writeAtomic writes next to dst then renames, so dst is never half written.
func writeAtomic(dst string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(dst), ".mise-*")
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
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

// settingsText returns the settings file content, or ok=false when it does not exist.
// ponytail: the file is never created, shkit owns it (ceiling: without shkit the line is added by hand).
func (t *tools) settingsText() (string, bool) {
	b, err := os.ReadFile(t.settings)
	return string(b), err == nil
}

// appendLine adds a comment and a line at the end of the settings file, after a newline if needed.
func (t *tools) appendLine(text, comment, line string) error {
	if t.Dry {
		return nil
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return os.WriteFile(t.settings, []byte(text+comment+"\n"+line+"\n"), 0o644)
}

func (t *tools) activate() error {
	text, ok := t.settingsText()
	if !ok {
		t.warn("  mise : %s absent, ajoute à ton shell : %s", t.tilde(t.settings), miseLine)
		return nil
	}
	if strings.Contains(text, "mise/shell.sh") {
		return nil
	}
	t.say("  mise : ligne d'activation ajoutée à %s", t.tilde(t.settings))
	return t.appendLine(text, "# mise (node & co), managed by dot install", miseLine)
}

// nodeSpec is one pinned node with the packages of every profile that declares it.
type nodeSpec struct {
	node string
	pkgs []string
}

func mergeNVM(all []manifest.NVM) []nodeSpec {
	var out []nodeSpec
	for _, n := range all {
		if n.Node == "" {
			continue
		}
		i := slices.IndexFunc(out, func(s nodeSpec) bool { return s.node == n.Node })
		if i < 0 {
			out = append(out, nodeSpec{node: n.Node})
			i = len(out) - 1
		}
		for _, p := range n.Packages {
			if !slices.Contains(out[i].pkgs, p) {
				out[i].pkgs = append(out[i].pkgs, p)
			}
		}
	}
	return out
}

func (t *tools) nvm() error {
	specs := mergeNVM(t.NVM)
	if len(specs) == 0 {
		return nil
	}
	dir := filepath.Join(t.Home, ".nvm")
	if fi, err := os.Stat(filepath.Join(dir, "nvm.sh")); err != nil || fi.Size() == 0 {
		t.say("  nvm %s → ~/.nvm", nvmVersion)
		if err := t.run("git", "clone", "-q", "--depth", "1", "--branch", nvmVersion, t.NVMRepoURL, dir); err != nil {
			return fmt.Errorf("nvm : clone impossible : %w", err)
		}
	}
	for _, s := range specs {
		if err := t.node(dir, s); err != nil {
			return err
		}
	}
	return nil
}

// pkgInstalled reports whether spec ("name@version", name possibly scoped) is installed at exactly
// that version: the version comes from the module's package.json, so a changed pin reinstalls.
func pkgInstalled(modules, spec string) bool {
	name, want := spec, ""
	if i := strings.LastIndex(spec, "@"); i > 0 {
		name, want = spec[:i], spec[i+1:]
	}
	data, err := os.ReadFile(filepath.Join(modules, name, "package.json"))
	if err != nil {
		return false
	}
	var pj struct {
		Version string `json:"version"`
	}
	return json.Unmarshal(data, &pj) == nil && pj.Version != "" && pj.Version == want
}

func (t *tools) node(dir string, s nodeSpec) error {
	base := filepath.Join(dir, "versions", "node", "v"+s.node)
	var missing []string
	for _, p := range s.pkgs {
		if !pkgInstalled(filepath.Join(base, "lib", "node_modules"), p) {
			missing = append(missing, p)
		}
	}
	if !executable(filepath.Join(base, "bin", "node")) || len(missing) > 0 {
		msg := "  nvm : node " + s.node
		if len(missing) > 0 {
			msg += " + " + strings.Join(missing, " ")
		}
		t.say("%s", msg)
		if !t.Dry {
			args := append([]string{"-c", nvmScript, "_", s.node}, missing...)
			if err := t.Run("bash", args...); err != nil {
				return fmt.Errorf("nvm : installation impossible : %w", err)
			}
		}
	}
	text, ok := t.settingsText()
	if ok && !strings.Contains(text, "nvm/versions/node/v"+s.node+"/bin") {
		t.say("  nvm : bin ajouté en fin de PATH dans %s", t.tilde(t.settings))
		line := fmt.Sprintf(`export PATH="$PATH:$HOME/.nvm/versions/node/v%s/bin"`, s.node)
		return t.appendLine(text, "# nvm, only for the CLIs mise cannot install (dot install)", line)
	}
	return nil
}

// plugins re-adds the plugins of each marketplace Codex knows: a local plugin is installed as a copy.
func (t *tools) plugins() error {
	if len(t.Marketplaces) == 0 {
		return nil
	}
	codex, err := t.LookPath("codex")
	if err != nil {
		return nil
	}
	var merged []manifest.Marketplace
	for _, m := range t.Marketplaces {
		i := slices.IndexFunc(merged, func(x manifest.Marketplace) bool { return x.Name == m.Name })
		if i < 0 {
			merged = append(merged, manifest.Marketplace{Name: m.Name})
			i = len(merged) - 1
		}
		for _, p := range m.Plugins {
			if !slices.Contains(merged[i].Plugins, p) {
				merged[i].Plugins = append(merged[i].Plugins, p)
			}
		}
	}
	// A failing list means no usable marketplace: skip silently, like the bash.
	out, err := t.Output(codex, "plugin", "marketplace", "list")
	if err != nil {
		return nil
	}
	known := map[string]bool{}
	for _, line := range bytes.Split(out, []byte("\n")) {
		if f := strings.Fields(string(line)); len(f) > 0 {
			known[f[0]] = true
		}
	}
	for _, m := range merged {
		if !known[m.Name] {
			continue
		}
		for _, p := range m.Plugins {
			if err := t.run(codex, "plugin", "add", p+"@"+m.Name); err != nil {
				return fmt.Errorf("codex plugin add %s@%s : %w", p, m.Name, err)
			}
		}
	}
	return nil
}
