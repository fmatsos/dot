package tools

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fmatsos/dot/internal/manifest"
)

type env struct {
	t        *testing.T
	home     string
	out, err bytes.Buffer
	calls    []string // "name arg arg"
	listOut  string
	miseOut  string // `mise --version` output of an existing binary
	miseErr  error
	miseWait chan struct{} // when set, `mise --version` blocks until it is closed
	nvmTag   string        // `git describe` output of ~/.nvm
	nvmErr   error
	runErr   func(call string) error // fails the matching Run calls
	hits     atomic.Int32
	srv      *httptest.Server
	body     []byte
	opts     Options
}

// newEnv builds options with every seam faked: no network, no real tool.
func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, home: t.TempDir(), miseOut: miseVersion + " linux-x64 (2026-10-01)\n", body: []byte("#!/bin/sh\necho fake mise\n"), listOut: "acme /tmp/fictional-market\n"}
	sum := sha256.Sum256(e.body)
	old := miseSums
	miseSums = map[string]string{"linux-x64": hex.EncodeToString(sum[:])}
	t.Cleanup(func() { miseSums = old })
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.hits.Add(1)
		if r.URL.Path != "/v"+miseVersion+"/mise-v"+miseVersion+"-linux-x64" {
			http.NotFound(w, r)
			return
		}
		w.Write(e.body)
	}))
	t.Cleanup(e.srv.Close)
	e.opts = Options{
		Home: e.home, MiseConfig: true, Out: &e.out, Err: &e.err,
		HTTP: e.srv.Client(), MiseBaseURL: e.srv.URL, NVMRepoURL: "file:///unused",
		LookPath: func(n string) (string, error) { return "/fake/" + n, nil },
		Run: func(n string, a ...string) error {
			call := strings.Join(append([]string{n}, a...), " ")
			e.calls = append(e.calls, call)
			if e.runErr != nil {
				return e.runErr(call)
			}
			return nil
		},
		Output: func(n string, a ...string) ([]byte, error) {
			e.calls = append(e.calls, strings.Join(append([]string{n}, a...), " "))
			if n == "git" && len(a) > 2 && a[2] == "describe" {
				return []byte(e.nvmTag + "\n"), e.nvmErr
			}
			if filepath.Base(n) == "mise" && len(a) == 1 && a[0] == "--version" {
				if e.miseWait != nil {
					<-e.miseWait
				}
				return []byte(e.miseOut), e.miseErr
			}
			return []byte(e.listOut), nil
		},
		Platform: func() (string, string) { return "linux", "amd64" },
	}
	return e
}

func (e *env) run() error { e.t.Helper(); return Run(e.opts) }

func (e *env) path(p ...string) string { return filepath.Join(append([]string{e.home}, p...)...) }

func (e *env) write(rel, content string) {
	e.t.Helper()
	p := e.path(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) read(rel string) string {
	b, _ := os.ReadFile(e.path(rel))
	return string(b)
}

func (e *env) reset() { e.calls = nil; e.out.Reset(); e.err.Reset() }

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestPinnedTable(t *testing.T) {
	want := map[string]string{
		"linux-x64":   "57ced973f968b8fbab07aa8e32bd7077d4a357e200a22356d98963c723c6de0a",
		"linux-arm64": "4b8cacffac83e8493fc5d1eef25f6365edba73ccbed5a1f3987b7cb3f5079656",
		"macos-arm64": "8d2007efdae0c2b64e3955257533e6ec17197bc2fdcbc5dd8f6847f92881deea",
		"macos-x64":   "815eb7872e453dcd30ed5e2e478978d2947e34106b6cd4f9cc4d4e51f7761332",
	}
	for k, v := range want {
		if miseSums[k] != v {
			t.Errorf("sum %s differs", k)
		}
	}
	for _, p := range [][3]string{{"linux", "amd64", "linux-x64"}, {"linux", "arm64", "linux-arm64"}, {"darwin", "arm64", "macos-arm64"}, {"darwin", "amd64", "macos-x64"}, {"windows", "amd64", ""}} {
		if assetFor(p[0], p[1]) != p[2] {
			t.Errorf("asset %v", p)
		}
	}
}

func TestMiseInstallsVerifiedBinary(t *testing.T) {
	e := newEnv(t)
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	bin := e.path(".local/bin/mise")
	fi, err := os.Stat(bin)
	if err != nil || fi.Mode().Perm() != 0o755 {
		t.Fatalf("mise: %v %v", fi, err)
	}
	if got, _ := os.ReadFile(bin); !bytes.Equal(got, e.body) {
		t.Fatal("content differs")
	}
	if want := "  mise " + miseVersion + " (linux-x64) → ~/.local/bin/mise\n"; !strings.HasPrefix(e.out.String(), want) {
		t.Fatalf("out %q", e.out.String())
	}
	ents, _ := os.ReadDir(e.path(".local/bin"))
	if len(ents) != 1 {
		t.Fatalf("temporary file left behind: %v", ents)
	}
	// No config.toml: no `mise install`.
	eq(t, e.calls, nil)
}

func TestMiseNotInstalledWithoutConfig(t *testing.T) {
	e := newEnv(t)
	e.opts.MiseConfig = false
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	if e.hits.Load() != 0 || e.out.Len()+e.err.Len() != 0 {
		t.Fatal("mise touched without a config.toml")
	}
	if _, err := os.Stat(e.path(".local")); err == nil {
		t.Fatal("files written")
	}
}

func TestMiseBadChecksum(t *testing.T) {
	e := newEnv(t)
	e.body = []byte("tampered")
	err := e.run()
	if err == nil || err.Error() != "mise : checksum invalide, abandon" {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(e.path(".local/bin/mise")); err == nil {
		t.Fatal("tampered binary installed")
	}
	if ents, _ := os.ReadDir(e.path(".local/bin")); len(ents) != 0 {
		t.Fatalf("leftover %v", ents)
	}
}

func TestMiseDownloadFailure(t *testing.T) {
	e := newEnv(t)
	e.opts.MiseBaseURL = e.srv.URL + "/missing"
	err := e.run()
	if err == nil || err.Error() != "mise : téléchargement impossible" {
		t.Fatalf("err %v", err)
	}
	e = newEnv(t)
	e.srv.Close()
	if err := e.run(); err == nil || err.Error() != "mise : téléchargement impossible" {
		t.Fatalf("err %v", err)
	}
}

func TestMiseUnsupportedPlatform(t *testing.T) {
	e := newEnv(t)
	e.opts.Platform = func() (string, string) { return "plan9", "mips" }
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	if want := "  mise ignoré : plateforme non gérée (plan9-mips)\n"; !strings.HasPrefix(e.err.String(), want) {
		t.Fatalf("err %q", e.err.String())
	}
	if e.hits.Load() != 0 {
		t.Fatal("download attempted")
	}
}

func TestMiseDryRunWritesNothing(t *testing.T) {
	e := newEnv(t)
	e.opts.Dry = true
	e.write(".config/mise/config.toml", "[tools]\n")
	e.write(".config/shkit/settings.sh", "export A=1\n")
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	if e.hits.Load() != 0 || len(e.calls) != 0 {
		t.Fatal("dry run acted")
	}
	if _, err := os.Stat(e.path(".local")); err == nil {
		t.Fatal("dry run wrote the binary")
	}
	if e.read(".config/shkit/settings.sh") != "export A=1\n" {
		t.Fatal("settings modified")
	}
	out := e.out.String()
	for _, w := range []string{"  [dry] installer mise " + miseVersion + " (linux-x64) dans ~/.local/bin/mise\n", "  mise : ligne d'activation ajoutée à ~/.config/shkit/settings.sh\n"} {
		if !strings.Contains(out, w) {
			t.Fatalf("missing %q in %q", w, out)
		}
	}
}

func TestMiseInstallArgvAndIdempotence(t *testing.T) {
	e := newEnv(t)
	e.write(".config/mise/config.toml", "[tools]\n")
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	eq(t, e.calls, []string{e.path(".local/bin/mise") + " install -y"})
	hits := e.hits.Load()
	e.reset()
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	if e.hits.Load() != hits {
		t.Fatal("binary downloaded twice")
	}
	if strings.Contains(e.out.String(), "mise 2026") {
		t.Fatalf("reinstalled: %q", e.out.String())
	}
}

// existingMise installs an executable stand-in for mise and returns its path.
func (e *env) existingMise() string {
	e.t.Helper()
	e.write(".local/bin/mise", "#!/bin/sh\necho foreign\n")
	bin := e.path(".local/bin/mise")
	if err := os.Chmod(bin, 0o755); err != nil {
		e.t.Fatal(err)
	}
	return bin
}

func TestMiseExistingPinnedVersionIsKept(t *testing.T) {
	e := newEnv(t)
	bin := e.existingMise()
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	if e.hits.Load() != 0 || e.out.Len() != 0 {
		t.Fatalf("pinned binary replaced: %d hits, out %q", e.hits.Load(), e.out.String())
	}
	if got, _ := os.ReadFile(bin); string(got) != "#!/bin/sh\necho foreign\n" {
		t.Fatal("binary rewritten")
	}
	eq(t, e.calls, []string{bin + " --version"})
}

func TestMiseExistingOtherVersionIsReplaced(t *testing.T) {
	for _, tc := range []struct{ name, out, msg string }{
		{"older", "2026.9.0 linux-x64 (2026-09-01)\n", "  mise : version 2026.9.0 trouvée, " + miseVersion + " attendue : mise à jour\n"},
		{"look-alike", miseVersion + "1 linux-x64\n", "  mise : version " + miseVersion + "1 trouvée, " + miseVersion + " attendue : mise à jour\n"},
		{"empty", "", "  mise : version illisible, " + miseVersion + " attendue : mise à jour\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.existingMise()
			e.miseOut = tc.out
			if err := e.run(); err != nil {
				t.Fatal(err)
			}
			if e.hits.Load() != 1 {
				t.Fatalf("%d downloads", e.hits.Load())
			}
			if got := e.read(".local/bin/mise"); got != string(e.body) {
				t.Fatalf("binary not replaced: %q", got)
			}
			if !strings.HasPrefix(e.out.String(), tc.msg+"  mise "+miseVersion+" (linux-x64)") {
				t.Fatalf("out %q", e.out.String())
			}
		})
	}
}

func TestMiseVersionFailureOrHangReplacesBinary(t *testing.T) {
	e := newEnv(t)
	e.existingMise()
	e.miseErr = errors.New("boom")
	e.miseOut = miseVersion // even a plausible output counts for nothing when the command failed
	if err := e.run(); err != nil || e.hits.Load() != 1 || e.read(".local/bin/mise") != string(e.body) {
		t.Fatalf("failure: %v, %d downloads", err, e.hits.Load())
	}
	e = newEnv(t)
	e.existingMise()
	e.miseWait = make(chan struct{})
	t.Cleanup(func() { close(e.miseWait) })
	old := miseVersionTimeout
	miseVersionTimeout = 20 * time.Millisecond
	t.Cleanup(func() { miseVersionTimeout = old })
	if err := e.run(); err != nil || e.hits.Load() != 1 || e.read(".local/bin/mise") != string(e.body) {
		t.Fatalf("hang: %v, %d downloads", err, e.hits.Load())
	}
}

func TestMiseOtherVersionDryAndBadChecksum(t *testing.T) {
	e := newEnv(t)
	e.opts.Dry = true
	e.existingMise()
	e.miseOut = "2026.9.0\n"
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	if e.hits.Load() != 0 || e.read(".local/bin/mise") != "#!/bin/sh\necho foreign\n" {
		t.Fatal("dry run acted")
	}
	for _, w := range []string{"  mise : version 2026.9.0 trouvée, " + miseVersion + " attendue : mise à jour\n", "  [dry] installer mise " + miseVersion + " (linux-x64) dans ~/.local/bin/mise\n"} {
		if !strings.Contains(e.out.String(), w) {
			t.Fatalf("missing %q in %q", w, e.out.String())
		}
	}
	// The replacement is verified like a first install: a bad sum leaves the old binary alone.
	e = newEnv(t)
	e.existingMise()
	e.miseOut = "2026.9.0\n"
	e.body = []byte("tampered")
	if err := e.run(); err == nil || err.Error() != "mise : checksum invalide, abandon" {
		t.Fatalf("err %v", err)
	}
	if e.read(".local/bin/mise") != "#!/bin/sh\necho foreign\n" {
		t.Fatal("old binary replaced by an unverified one")
	}
}

func TestSkipStillFetchesButDoesNotInstall(t *testing.T) {
	e := newEnv(t)
	e.opts.Skip = true
	e.opts.NVM = []manifest.NVM{{Node: "9.8.7", Packages: []string{"demo-cli@2.3.4"}}}
	e.write(".config/mise/config.toml", "[tools]\n")
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	if e.hits.Load() != 1 {
		t.Fatal("binary not fetched")
	}
	eq(t, e.calls, nil)
}

func TestActivationLineAddedOnce(t *testing.T) {
	e := newEnv(t)
	e.write(".config/shkit/settings.sh", "export A=1") // no trailing newline
	for range 2 {
		if err := e.run(); err != nil {
			t.Fatal(err)
		}
	}
	want := "export A=1\n# mise (node & co), managed by dot install\n" + miseLine + "\n"
	if got := e.read(".config/shkit/settings.sh"); got != want {
		t.Fatalf("settings %q", got)
	}
	// Already activated by hand: untouched.
	e.write(".config/shkit/settings.sh", ". \"$HOME/.config/mise/shell.sh\"\n")
	e.reset()
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	if e.read(".config/shkit/settings.sh") != ". \"$HOME/.config/mise/shell.sh\"\n" || e.out.Len() != 0 {
		t.Fatalf("rewritten: %q", e.out.String())
	}
}

func TestSettingsNeverCreated(t *testing.T) {
	e := newEnv(t)
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(e.path(".config/shkit")); err == nil {
		t.Fatal("settings created")
	}
	if want := "  mise : ~/.config/shkit/settings.sh absent, ajoute à ton shell : " + miseLine + "\n"; e.err.String() != want {
		t.Fatalf("err %q", e.err.String())
	}
}

func TestNVMCloneInstallAndPath(t *testing.T) {
	e := newEnv(t)
	e.opts.MiseConfig = false
	e.opts.NVM = []manifest.NVM{{Node: "9.8.7", Packages: []string{"demo-cli@2.3.4", "@acme/other-cli@5.6.7"}}}
	e.write(".config/shkit/settings.sh", "export A=1\n")
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	eq(t, e.calls, []string{
		"git clone -q --depth 1 --branch v0.40.4 file:///unused " + e.path(".nvm"),
		"bash -c " + nvmScript + " _ 9.8.7 demo-cli@2.3.4 @acme/other-cli@5.6.7",
	})
	wantOut := "  nvm v0.40.4 → ~/.nvm\n  nvm : node 9.8.7 + demo-cli@2.3.4 @acme/other-cli@5.6.7\n  nvm : bin ajouté en fin de PATH dans ~/.config/shkit/settings.sh\n"
	if e.out.String() != wantOut {
		t.Fatalf("out %q", e.out.String())
	}
	line := `export PATH="$PATH:$HOME/.nvm/versions/node/v9.8.7/bin"`
	if got := e.read(".config/shkit/settings.sh"); got != "export A=1\n# nvm, only for the CLIs mise cannot install (dot install)\n"+line+"\n" {
		t.Fatalf("settings %q", got)
	}
}

func TestNVMIdempotentAndPartial(t *testing.T) {
	e := newEnv(t)
	e.opts.MiseConfig = false
	e.opts.NVM = []manifest.NVM{{Node: "9.8.7", Packages: []string{"demo-cli@2.3.4", "@acme/other-cli@5.6.7"}}}
	e.write(".config/shkit/settings.sh", "")
	e.write(".nvm/nvm.sh", "# stub\n")
	base := ".nvm/versions/node/v9.8.7"
	e.write(base+"/bin/node", "")
	if err := os.Chmod(e.path(base, "bin/node"), 0o755); err != nil {
		t.Fatal(err)
	}
	e.write(base+"/lib/node_modules/demo-cli/package.json", `{"name":"demo-cli","version":"2.3.4"}`)
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	// Only the missing package is installed; no clone.
	eq(t, e.calls, []string{"bash -c " + nvmScript + " _ 9.8.7 @acme/other-cli@5.6.7"})
	e.reset()
	e.write(base+"/lib/node_modules/@acme/other-cli/package.json", `{"version":"5.6.7"}`)
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	eq(t, e.calls, nil)
	if n := strings.Count(e.read(".config/shkit/settings.sh"), "nvm/versions/node/v9.8.7/bin"); n != 1 {
		t.Fatalf("PATH line written %d times", n)
	}
	if e.out.Len() != 0 {
		t.Fatalf("out %q", e.out.String())
	}
}

func TestNVMPinChangeReinstalls(t *testing.T) {
	base := ".nvm/versions/node/v9.8.7"
	for _, tc := range []struct {
		name, pkg string // pkg is the pinned spec; the package.json content below is on disk
		json      string
		want      bool // reinstalled
	}{
		{"same version", "demo-cli@2.0.0", `{"version":"2.0.0"}`, false},
		{"other version", "demo-cli@2.0.0", `{"version":"1.0.0"}`, true},
		{"unreadable", "demo-cli@2.0.0", `{not json`, true},
		{"no version field", "demo-cli@2.0.0", `{"name":"demo-cli"}`, true},
		{"scoped same", "@acme/other-cli@3.1.0", `{"version":"3.1.0"}`, false},
		{"scoped other", "@acme/other-cli@3.1.0", `{"version":"3.0.0"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.opts.MiseConfig = false
			e.opts.NVM = []manifest.NVM{{Node: "9.8.7", Packages: []string{tc.pkg}}}
			e.write(".nvm/nvm.sh", "# stub\n")
			e.write(base+"/bin/node", "")
			if err := os.Chmod(e.path(base, "bin/node"), 0o755); err != nil {
				t.Fatal(err)
			}
			name := tc.pkg[:strings.LastIndex(tc.pkg, "@")]
			e.write(base+"/lib/node_modules/"+name+"/package.json", tc.json)
			if err := e.run(); err != nil {
				t.Fatal(err)
			}
			if tc.want {
				eq(t, e.calls, []string{"bash -c " + nvmScript + " _ 9.8.7 " + tc.pkg})
			} else {
				eq(t, e.calls, nil)
			}
		})
	}
	// No module directory at all, or a directory without package.json: installed again.
	e := newEnv(t)
	e.opts.MiseConfig = false
	e.opts.NVM = []manifest.NVM{{Node: "9.8.7", Packages: []string{"demo-cli@2.0.0"}}}
	e.write(".nvm/nvm.sh", "# stub\n")
	e.write(base+"/bin/node", "")
	os.Chmod(e.path(base, "bin/node"), 0o755)
	e.write(base+"/lib/node_modules/demo-cli/README", "x")
	if err := e.run(); err != nil || len(e.calls) != 1 {
		t.Fatalf("err %v calls %v", err, e.calls)
	}
}

func TestNVMMergesProfiles(t *testing.T) {
	e := newEnv(t)
	e.opts.MiseConfig = false
	e.opts.NVM = []manifest.NVM{
		{Node: "9.8.7", Packages: []string{"a-cli@1.0.0", "b-cli@1.0.0"}},
		{Node: "9.8.7", Packages: []string{"b-cli@1.0.0", "c-cli@1.0.0"}},
		{Node: "8.0.0"},
	}
	e.write(".config/shkit/settings.sh", "")
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	eq(t, e.calls, []string{
		"git clone -q --depth 1 --branch v0.40.4 file:///unused " + e.path(".nvm"),
		"bash -c " + nvmScript + " _ 9.8.7 a-cli@1.0.0 b-cli@1.0.0 c-cli@1.0.0",
		"bash -c " + nvmScript + " _ 8.0.0",
	})
	s := e.read(".config/shkit/settings.sh")
	if strings.Count(s, "v9.8.7/bin") != 1 || strings.Count(s, "v8.0.0/bin") != 1 {
		t.Fatalf("settings %q", s)
	}
}

func TestNVMSkipAndDry(t *testing.T) {
	e := newEnv(t)
	e.opts.MiseConfig = false
	e.opts.NVM = []manifest.NVM{{Node: "9.8.7", Packages: []string{"demo-cli@2.3.4"}}}
	e.write(".config/shkit/settings.sh", "x\n")
	e.opts.Skip = true
	if err := e.run(); err != nil || len(e.calls) != 0 || e.out.Len() != 0 {
		t.Fatalf("skip acted: %v %v", err, e.calls)
	}
	e.opts.Skip, e.opts.Dry = false, true
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	eq(t, e.calls, nil)
	if _, err := os.Stat(e.path(".nvm")); err == nil || e.read(".config/shkit/settings.sh") != "x\n" {
		t.Fatal("dry run wrote")
	}
	for _, w := range []string{"  [dry] git clone -q --depth 1 --branch v0.40.4 file:///unused " + e.path(".nvm") + "\n", "  nvm : node 9.8.7 + demo-cli@2.3.4\n", "  nvm : bin ajouté en fin de PATH"} {
		if !strings.Contains(e.out.String(), w) {
			t.Fatalf("missing %q in %q", w, e.out.String())
		}
	}
}

func TestNVMNoSettingsFileIsSilent(t *testing.T) {
	e := newEnv(t)
	e.opts.MiseConfig = false
	e.opts.NVM = []manifest.NVM{{Node: "9.8.7"}}
	e.write(".nvm/nvm.sh", "#\n")
	e.write(".nvm/.git/HEAD", "ref: refs/heads/main\n")
	e.nvmTag = nvmVersion
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(e.path(".config")); err == nil || e.err.Len() != 0 {
		t.Fatal("settings created or warned")
	}
}

func TestNVMCloneFromLocalRepoAndRealScript(t *testing.T) {
	for _, bin := range []string{"git", "bash"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " missing")
		}
	}
	e := newEnv(t)
	e.opts.MiseConfig = false
	// A local repo stands for the nvm remote: its tag is the pinned one and nvm.sh a stub.
	src := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", src, "-c", "user.name=T", "-c", "user.email=t@example.com", "-c", "tag.gpgsign=false", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(src, "nvm.sh"), []byte("nvm() { printf 'node %s\\n' \"$2\" >>\"$HOME/nvm-log\"; }\nnpm() { printf '%s\\n' \"$*\" >>\"$HOME/nvm-log\"; }\n"), 0o644)
	git("add", ".")
	git("commit", "-q", "-m", "fixture")
	git("tag", nvmVersion)
	e.opts.NVMRepoURL = "file://" + src
	e.opts.Run, e.opts.Output = nil, nil // real exec, scoped to the temp HOME
	e.opts.NVM = []manifest.NVM{{Node: "9.8.7", Packages: []string{"demo-cli@2.3.4", "$(touch " + e.home + "/pwned)@1"}}}
	e.write(".config/shkit/settings.sh", "")
	if err := e.run(); err != nil {
		t.Fatalf("%v\n%s", err, e.err.String())
	}
	want := "node 9.8.7\ninstall -g --no-fund --no-audit demo-cli@2.3.4 $(touch " + e.home + "/pwned)@1\n"
	if got := e.read("nvm-log"); got != want {
		t.Fatalf("nvm log %q", got)
	}
	if _, err := os.Stat(e.path("pwned")); err == nil {
		t.Fatal("package name was interpreted by the shell")
	}
	if _, err := os.Stat(e.path(".nvm/.git")); err != nil {
		t.Fatal("not cloned")
	}
}

func TestPlugins(t *testing.T) {
	e := newEnv(t)
	e.opts.MiseConfig = false
	e.opts.Marketplaces = []manifest.Marketplace{{Name: "acme", Plugins: []string{"one", "two"}}, {Name: "acme", Plugins: []string{"two", "three"}}, {Name: "other", Plugins: []string{"x"}}}
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	eq(t, e.calls, []string{
		"/fake/codex plugin marketplace list",
		"/fake/codex plugin add one@acme",
		"/fake/codex plugin add two@acme",
		"/fake/codex plugin add three@acme",
	})
}

func TestPluginsSkips(t *testing.T) {
	e := newEnv(t)
	e.opts.MiseConfig = false
	e.opts.Marketplaces = []manifest.Marketplace{{Name: "acme", Plugins: []string{"one"}}}
	e.listOut = "other /tmp/x\nacmecorp /tmp/y\n"
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	eq(t, e.calls, []string{"/fake/codex plugin marketplace list"})
	e.reset()
	e.opts.LookPath = func(string) (string, error) { return "", errors.New("absent") }
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	eq(t, e.calls, nil)
	e.opts.LookPath = func(n string) (string, error) { return n, nil }
	e.opts.Marketplaces = nil
	if err := e.run(); err != nil || len(e.calls) != 0 {
		t.Fatal("no marketplace must do nothing")
	}
}

func TestPluginsDryStillListsButDoesNotAdd(t *testing.T) {
	e := newEnv(t)
	e.opts.MiseConfig = false
	e.opts.Dry = true
	e.opts.Marketplaces = []manifest.Marketplace{{Name: "acme", Plugins: []string{"one"}}}
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	eq(t, e.calls, []string{"/fake/codex plugin marketplace list"})
	if e.out.String() != "  [dry] /fake/codex plugin add one@acme\n" {
		t.Fatalf("out %q", e.out.String())
	}
}

func TestCommandFailuresAreReported(t *testing.T) {
	e := newEnv(t)
	e.opts.MiseConfig = false
	e.opts.NVM = []manifest.NVM{{Node: "9.8.7"}}
	e.opts.Run = func(string, ...string) error { return errors.New("boom") }
	if err := e.run(); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err %v", err)
	}
}

// nvmRepo fakes an existing ~/.nvm checkout, with the tag git reports for it.
func (e *env) nvmRepo(tag string) {
	e.t.Helper()
	e.opts.MiseConfig = false
	e.opts.NVM = []manifest.NVM{{Node: "9.8.7"}}
	e.write(".nvm/nvm.sh", "# stub\n")
	e.write(".nvm/.git/HEAD", "ref: refs/heads/main\n")
	e.write(".nvm/versions/node/v9.8.7/bin/node", "")
	os.Chmod(e.path(".nvm/versions/node/v9.8.7/bin/node"), 0o755)
	e.nvmTag = tag
}

func TestNVMPinnedTagIsLeftAlone(t *testing.T) {
	e := newEnv(t)
	e.nvmRepo(nvmVersion)
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	eq(t, e.calls, []string{"git -C " + e.path(".nvm") + " describe --tags --exact-match HEAD"})
	if e.out.Len() != 0 || e.err.Len() != 0 {
		t.Fatalf("out %q err %q", e.out.String(), e.err.String())
	}
}

func TestNVMOtherTagIsUpdated(t *testing.T) {
	d := func(e *env) string { return e.path(".nvm") }
	for name, setup := range map[string]func(*env){
		"other tag":  func(e *env) { e.nvmRepo("v0.39.0") },
		"no tag":     func(e *env) { e.nvmRepo(""); e.nvmErr = errors.New("fatal: no tag exactly matches") },
		"empty tags": func(e *env) { e.nvmRepo("") },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			setup(e)
			if err := e.run(); err != nil {
				t.Fatal(err)
			}
			eq(t, e.calls, []string{
				"git -C " + d(e) + " describe --tags --exact-match HEAD",
				"git -C " + d(e) + " fetch --depth 1 origin tag " + nvmVersion,
				"git -C " + d(e) + " checkout -q " + nvmVersion,
			})
			if !strings.Contains(e.out.String(), "nvm : mise à jour vers "+nvmVersion) {
				t.Fatalf("out %q", e.out.String())
			}
			if _, err := os.Stat(e.path(".nvm/versions/node/v9.8.7/bin/node")); err != nil {
				t.Fatal("installed node touched")
			}
		})
	}
}

func TestNVMNotAGitRepoIsReportedAndUntouched(t *testing.T) {
	e := newEnv(t)
	e.nvmRepo("v0.39.0")
	if err := os.RemoveAll(e.path(".nvm/.git")); err != nil {
		t.Fatal(err)
	}
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	eq(t, e.calls, nil)
	if e.err.String() != "  nvm : ~/.nvm n'est pas un dépôt git, version non vérifiée\n" {
		t.Fatalf("err %q", e.err.String())
	}
}

func TestNVMUpdateDryRunAnnouncesOnly(t *testing.T) {
	e := newEnv(t)
	e.nvmRepo("v0.39.0")
	e.opts.Dry = true
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	eq(t, e.calls, []string{"git -C " + e.path(".nvm") + " describe --tags --exact-match HEAD"})
	for _, w := range []string{
		"  [dry] git -C " + e.path(".nvm") + " fetch --depth 1 origin tag " + nvmVersion + "\n",
		"  [dry] git -C " + e.path(".nvm") + " checkout -q " + nvmVersion + "\n",
	} {
		if !strings.Contains(e.out.String(), w) {
			t.Fatalf("missing %q in %q", w, e.out.String())
		}
	}
}

func TestNVMUpdateFailure(t *testing.T) {
	e := newEnv(t)
	e.nvmRepo("v0.39.0")
	e.runErr = func(call string) error {
		if strings.Contains(call, " fetch ") {
			return errors.New("network down")
		}
		return nil
	}
	err := e.run()
	if err == nil || err.Error() != "nvm : mise à jour impossible : network down" {
		t.Fatalf("err %v", err)
	}
	for _, c := range e.calls {
		if strings.Contains(c, "checkout") || strings.HasPrefix(c, "bash") {
			t.Fatalf("went on after the failed fetch: %q", e.calls)
		}
	}
}

func TestSkipRunsNoCodexNorNVMCheck(t *testing.T) {
	e := newEnv(t)
	e.nvmRepo("v0.39.0")
	e.opts.Skip = true
	e.opts.Marketplaces = []manifest.Marketplace{{Name: "acme", Plugins: []string{"one"}}}
	if err := e.run(); err != nil {
		t.Fatal(err)
	}
	eq(t, e.calls, nil)
	if e.out.Len() != 0 || e.err.Len() != 0 {
		t.Fatalf("out %q err %q", e.out.String(), e.err.String())
	}
}

func TestNVMUpdatesRealCheckoutToPinnedTag(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	e := newEnv(t)
	e.opts.MiseConfig = false
	src := t.TempDir()
	git := func(dir string, args ...string) string {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@example.com", "-c", "tag.gpgsign=false", "-c", "commit.gpgsign=false"}, args...)...)
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git(src, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(src, "nvm.sh"), []byte("nvm() { :; }\n"), 0o644)
	git(src, "add", ".")
	git(src, "commit", "-q", "-m", "old")
	git(src, "tag", "v0.0.1")
	os.WriteFile(filepath.Join(src, "nvm.sh"), []byte("nvm() { :; }\n# new\n"), 0o644)
	git(src, "commit", "-q", "-am", "pinned")
	git(src, "tag", nvmVersion)
	git(e.home, "clone", "-q", "--depth", "1", "--branch", "v0.0.1", "file://"+src, e.path(".nvm"))
	e.write(".nvm/versions/node/v1.0.0/bin/node", "kept")
	e.opts.NVM = []manifest.NVM{{Node: "9.8.7"}}
	e.opts.Run, e.opts.Output = nil, nil // real exec, scoped to the temp HOME
	if err := e.run(); err != nil {
		t.Fatalf("%v\n%s", err, e.err.String())
	}
	if got := git(e.path(".nvm"), "describe", "--tags", "--exact-match", "HEAD"); got != nvmVersion {
		t.Fatalf("checkout at %q", got)
	}
	if e.read(".nvm/versions/node/v1.0.0/bin/node") != "kept" {
		t.Fatal("installed node removed")
	}
}
