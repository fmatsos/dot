package mcp

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func src(t *testing.T, s string) map[string]any {
	t.Helper()
	m, err := ParseSource([]byte(s))
	if err != nil {
		t.Fatalf("%s : %v", s, err)
	}
	return m
}

var owners = []Owner{{Known: map[string]bool{"ACME_TOKEN": true}}}

// plain wraps shared files that no profile owns.
func plain(ms ...map[string]any) []Source {
	var out []Source
	for _, m := range ms {
		out = append(out, Source{Servers: m})
	}
	return out
}

func TestConvert(t *testing.T) {
	shared := src(t, `{"a":{"command":"a-mcp","args":["serve"]},"b":{"url":"https://example.com/mcp"},"c":{"command":"c-mcp","env":{"K":"v"}},"gone":{"command":"x"}}`)
	local := src(t, `{"a":{"command":"a2","secrets":["ACME_TOKEN"],"args":["x"]},"gone":null}`)
	got, err := Convert(plain(shared), local, owners, "/h/.local/bin/dot")
	if err != nil {
		t.Fatal(err)
	}
	if got["gone"] != nil {
		t.Error("null must remove")
	}
	if _, ok := got["gone"]; !ok {
		t.Error("removal must stay listed")
	}
	if want := []string{"secrets", "run", "ACME_TOKEN", "--", "a2", "x"}; got["a"].Command != "/h/.local/bin/dot" || !reflect.DeepEqual(got["a"].Args, want) {
		t.Errorf("a = %+v", got["a"])
	}
	if got["b"].JSON() != `{"type":"http","url":"https://example.com/mcp"}` {
		t.Errorf("b = %s", got["b"].JSON())
	}
	if got["c"].JSON() != `{"type":"stdio","command":"c-mcp","args":[],"env":{"K":"v"}}` {
		t.Errorf("c = %s", got["c"].JSON())
	}
}

func TestConvertInvalid(t *testing.T) {
	bad := []string{
		`{"bad name":{"command":"x"}}`, `{"-x":{"command":"x"}}`,
		`{"x":{"command":"x","url":"https://example.com"}}`, `{"x":{}}`, `{"x":"s"}`,
		`{"x":{"command":""}}`, `{"x":{"command":"x","args":"bad"}}`, `{"x":{"command":"x","args":[1]}}`,
		`{"x":{"command":"x","args":null}}`, `{"x":{"command":"x","extra":1}}`,
		`{"x":{"command":"x","secrets":["UNKNOWN"]}}`, `{"x":{"command":"x","secrets":"ACME_TOKEN"}}`,
		`{"x":{"command":"x","env":{"INVALID-NAME":"v"}}}`, `{"x":{"command":"x","env":{"K":1}}}`,
		`{"x":{"url":"https://example.com","extra":1}}`, `{"x":{"url":""}}`, `{"x":{"command":"a\u0000b"}}`,
		`{"x":null}`, // null is only valid in the local file
	}
	for _, s := range bad {
		if _, err := Convert(plain(src(t, s)), map[string]any{}, owners, "/dot"); !errors.Is(err, ErrSource) {
			t.Errorf("shared %s accepted", s)
		}
	}
	if _, err := Convert(plain(map[string]any{}), src(t, `{"x":{"command":"x","secrets":["UNKNOWN"]}}`), owners, "/dot"); err == nil {
		t.Error("unknown secret in local accepted")
	}
	if _, err := Convert(plain(map[string]any{}), src(t, `{"x":null}`), owners, "/dot"); err != nil {
		t.Error("null in local refused")
	}
	// An invalid shared entry is refused even when a later source overrides it.
	if _, err := Convert(plain(src(t, `{"x":{"command":"x","url":"u"}}`)), src(t, `{"x":null}`), owners, "/dot"); err == nil {
		t.Error("overridden invalid entry accepted")
	}
	for _, s := range []string{`broken`, `[]`, `{} {}`, ``} {
		if _, err := ParseSource([]byte(s)); err == nil {
			t.Errorf("ParseSource(%q) accepted", s)
		}
	}
}

func TestConvertSharedOrderLastWins(t *testing.T) {
	a := src(t, `{"s":{"command":"old"},"a":{"command":"a"}}`)
	b := src(t, `{"s":{"command":"new"}}`)
	got, err := Convert(plain(a, b), map[string]any{}, owners, "/dot")
	if err != nil || got["s"].Command != "new" || got["a"].Command != "a" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestClaudeEntries(t *testing.T) {
	got, err := ClaudeEntries([]byte(`{"x":1,"mcpServers":{"s":{"command":"c"},"h":{"type":"streamable-http","url":"u"},"k":{"command":"c","type":null,"extra":"stay"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if canon(got["s"]) != `{"args":[],"command":"c","env":{},"type":"stdio"}` || canon(got["h"]) != `{"type":"http","url":"u"}` ||
		canon(got["k"]) != `{"args":[],"command":"c","env":{},"extra":"stay","type":"stdio"}` {
		t.Fatalf("%v", got)
	}
	if e, err := ClaudeEntries([]byte(`{}`)); err != nil || len(e) != 0 {
		t.Fatal("no mcpServers must be empty")
	}
	for _, s := range []string{`[]`, `{"mcpServers":[]}`, `{"mcpServers":{"a":null}}`, `nope`} {
		if _, err := ClaudeEntries([]byte(s)); err == nil {
			t.Errorf("%s accepted", s)
		}
	}
}

func TestCodexEntries(t *testing.T) {
	got, err := CodexEntries([]byte(`[{"name":"a","enabled":false,"transport":{"type":"stdio","command":"c","args":null,"env":null,"cwd":"/tmp"}},{"name":"h","transport":{"type":"streamable_http","url":"u","bearer_token_env_var":null}}]`))
	if err != nil {
		t.Fatal(err)
	}
	if canon(got["a"]) != `{"args":[],"command":"c","env":{},"type":"stdio"}` || canon(got["h"]) != `{"type":"http","url":"u"}` {
		t.Fatalf("%v", got)
	}
	for _, s := range []string{`{}`, `[{"transport":{}}]`, `nope`} {
		if _, err := CodexEntries([]byte(s)); err == nil {
			t.Errorf("%s accepted", s)
		}
	}
}

func TestRenderOpenCodeKeepsForeignKeys(t *testing.T) {
	cfg := src(t, `{"theme":"demo","mcp":{"foreign":{"type":"local","extra":true},"gone":{"type":"remote"}}}`)
	desired := map[string]*Server{
		"a":    {Command: "a", Args: []string{"x"}, Env: map[string]string{"K": "v"}},
		"h":    {URL: "https://example.com/mcp"},
		"gone": nil,
	}
	got := RenderOpenCode(cfg, desired)
	if canon(got) != `{"mcp":{"a":{"command":["a","x"],"enabled":true,"environment":{"K":"v"},"type":"local"},"foreign":{"extra":true,"type":"local"},"h":{"enabled":true,"type":"remote","url":"https://example.com/mcp"}},"theme":"demo"}` {
		t.Fatalf("%s", canon(got))
	}
	if _, ok := cfg["mcp"].(map[string]any)["a"]; ok {
		t.Fatal("input modified")
	}
}

func TestPlan(t *testing.T) {
	want := map[string]any{"add": map[string]any{"v": 1}, "mod": map[string]any{"v": 2}, "del": nil, "same": map[string]any{"v": 3}, "nothing": nil}
	cur := Entries{"mod": map[string]any{"v": 1}, "del": map[string]any{}, "same": map[string]any{"v": 3}}
	got := Plan("t", want, cur)
	exp := []Step{{"t", "ajout", "add"}, {"t", "retrait", "del"}, {"t", "modification", "mod"}}
	if !reflect.DeepEqual(got, exp) {
		t.Fatalf("%v", got)
	}
}

func TestKnownSecretsReadsNamesOnly(t *testing.T) {
	f := filepath.Join(t.TempDir(), "secrets.local")
	os.WriteFile(f, []byte("ACME_TOKEN=bw:ACME\n# c\nbad name=x\n_X=pass:v/i\nlast=noeol"), 0o600)
	got := KnownSecrets([]string{f, "/nonexistent"})
	if len(got) != 3 || !got["ACME_TOKEN"] || !got["_X"] || !got["last"] {
		t.Fatalf("%v", got)
	}
}

type fake struct {
	calls [][]string
	list  string
	fail  string // first arg after "mcp" that makes the call fail
}

func (f *fake) exec(name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if args[1] == f.fail {
		return nil, errors.New("exit 1")
	}
	if name == "codex" && args[1] == "list" {
		return []byte(f.list), nil
	}
	return nil, nil
}

func home(t *testing.T, servers, local string) string {
	t.Helper()
	h := t.TempDir()
	os.MkdirAll(filepath.Join(h, ".config", "mcp"), 0o700)
	if servers != "" {
		os.WriteFile(filepath.Join(h, ".config", "mcp", "servers.json"), []byte(servers), 0o600)
	}
	if local != "" {
		os.WriteFile(filepath.Join(h, ".config", "mcp", "servers.local.json"), []byte(local), 0o600)
	}
	return h
}

func opts(h string, f *fake, tools ...string) (Options, *bytes.Buffer) {
	var out bytes.Buffer
	return Options{Home: h, Out: &out, Exec: f.exec, LookPath: func(n string) (string, error) {
		for _, t := range tools {
			if t == n {
				return "/bin/" + n, nil
			}
		}
		return "", errors.New("absent")
	}}, &out
}

func TestRunPlanApplyAndOrder(t *testing.T) {
	h := home(t, `{"b":{"command":"b","args":["--x"],"env":{"K":"v","A":"1"}},"a":{"url":"https://example.com/mcp"}}`, `{"z":null}`)
	f := &fake{list: `[{"name":"b","transport":{"type":"stdio","command":"old","args":[],"env":null}},{"name":"z","transport":{"type":"stdio","command":"z"}}]`}
	o, out := opts(h, f, "codex")
	if err := Run(o); err != nil {
		t.Fatal(err)
	}
	if out.String() != "codex : ajout a\ncodex : modification b\ncodex : retrait z\n" {
		t.Fatalf("plan:\n%s", out)
	}
	want := [][]string{
		{"codex", "mcp", "list", "--json"},
		{"codex", "mcp", "add", "a", "--url", "https://example.com/mcp"},
		{"codex", "mcp", "remove", "b"},
		{"codex", "mcp", "add", "b", "--env", "A=1", "--env", "K=v", "--", "b", "--x"},
		{"codex", "mcp", "remove", "z"},
	}
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("calls: %q", f.calls)
	}
}

func TestRunDryRunAndInvalidNeverCallTools(t *testing.T) {
	h := home(t, `{"a":{"command":"a"}}`, "")
	f := &fake{list: `[]`}
	o, out := opts(h, f, "claude", "codex", "opencode")
	o.DryRun = true
	if err := Run(o); err != nil {
		t.Fatal(err)
	}
	if out.String() != "claude : ajout a\ncodex : ajout a\nopencode : ajout a\n" {
		t.Fatalf("%s", out)
	}
	if len(f.calls) != 1 {
		t.Fatalf("dry run calls: %q", f.calls) // only the codex state read
	}
	if _, err := os.Stat(filepath.Join(h, ".config", "opencode")); err == nil {
		t.Fatal("dry run wrote the OpenCode config")
	}
	os.WriteFile(filepath.Join(h, ".config", "mcp", "servers.local.json"), []byte(`{"x":{"command":"x","secrets":["NOPE"]}}`), 0o600)
	f2 := &fake{list: `[]`}
	o2, _ := opts(h, f2, "claude", "codex", "opencode")
	if err := Run(o2); err == nil || len(f2.calls) != 0 {
		t.Fatalf("invalid source: %v, calls %q", err, f2.calls)
	}
}

func TestRunWritesOpenCodePrivately(t *testing.T) {
	h := home(t, `{"a":{"command":"a"}}`, "")
	o, _ := opts(h, &fake{}, "opencode")
	if err := Run(o); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(h, ".config", "opencode", "config.json")
	st, err := os.Stat(p)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", st, err)
	}
	data, _ := os.ReadFile(p)
	if !strings.HasSuffix(string(data), "\n") || !strings.Contains(string(data), "\n  \"mcp\": {\n") {
		t.Fatalf("format:\n%s", data)
	}
	ino := func() int64 { s, _ := os.Stat(p); return s.ModTime().UnixNano() }
	before := ino()
	o2, out := opts(h, &fake{}, "opencode")
	if err := Run(o2); err != nil || out.String() != "opencode : à jour\n" || ino() != before {
		t.Fatalf("second run: %v %q", err, out)
	}
}

func TestRunRefusesOpenCodeSymlink(t *testing.T) {
	h := home(t, `{"a":{"command":"a"}}`, "")
	os.MkdirAll(filepath.Join(h, ".config", "opencode"), 0o700)
	os.Symlink(filepath.Join(h, "real.json"), filepath.Join(h, ".config", "opencode", "config.json"))
	o, _ := opts(h, &fake{}, "opencode")
	if err := Run(o); err == nil || !strings.Contains(err.Error(), "pas un lien") {
		t.Fatalf("%v", err)
	}
}

func TestRunApplyFailureMessage(t *testing.T) {
	h := home(t, `{"a":{"command":"a"}}`, "")
	f := &fake{fail: "add"}
	o, _ := opts(h, f, "codex")
	f.list = `[]`
	if err := Run(o); err == nil || err.Error() != "Codex : ajout impossible (a)" {
		t.Fatalf("%v", err)
	}
}

func TestRunFallbackToProfiles(t *testing.T) {
	h := home(t, "", "")
	d := t.TempDir()
	p1, p2, none := filepath.Join(d, "1.json"), filepath.Join(d, "2.json"), filepath.Join(d, "none.json")
	os.WriteFile(p1, []byte(`{"a":{"command":"one"},"b":{"command":"b"}}`), 0o600)
	os.WriteFile(p2, []byte(`{"a":{"command":"two"}}`), 0o600)
	f := &fake{list: `[]`}
	o, out := opts(h, f, "codex")
	o.Profiles = []Profile{{Servers: p1}, {Servers: none}, {Servers: p2}}
	if err := Run(o); err == nil {
		t.Fatal("applying without a linked servers.json must fail when FallbackOnApply is off")
	}
	o.DryRun = true
	if err := Run(o); err != nil || out.String() != "codex : ajout a\ncodex : ajout b\n" {
		t.Fatalf("dry: %v %q", err, out)
	}
	o.DryRun, o.FallbackOnApply = false, true
	f.calls = nil
	if err := Run(o); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.calls[1], []string{"codex", "mcp", "add", "a", "--", "two"}) {
		t.Fatalf("last profile must win: %q", f.calls)
	}
	o.Profiles = []Profile{{Servers: none}}
	if err := Run(o); err == nil || err.Error() != "servers.json invalide ou absent" {
		t.Fatalf("%v", err)
	}
}

// twoProfileHome builds <home>/.dot/{a,b}: each profile has a server using the secret it declares.
func twoProfileHome(t *testing.T, serversA, serversB, secretsA, secretsB string) (string, []Profile) {
	t.Helper()
	h := home(t, "", "")
	var ps []Profile
	for _, p := range []struct{ key, servers, secrets string }{{"a", serversA, secretsA}, {"b", serversB, secretsB}} {
		d := filepath.Join(h, ".dot", p.key)
		os.MkdirAll(filepath.Join(d, "home", ".config", "mcp"), 0o700)
		os.WriteFile(filepath.Join(d, "home", ".config", "mcp", "servers.json"), []byte(p.servers), 0o600)
		os.WriteFile(filepath.Join(d, "secrets.local"), []byte(p.secrets), 0o600)
		ps = append(ps, Profile{Key: ProfileKey(h, d), Servers: filepath.Join(d, "home", ".config", "mcp", "servers.json"), Secrets: filepath.Join(d, "secrets.local")})
	}
	return h, ps
}

func TestRunSecretServersSelectTheirProfile(t *testing.T) {
	h, ps := twoProfileHome(t,
		`{"sa":{"command":"sa-mcp","args":["x"],"secrets":["ACME_A"]},"plain":{"command":"p"}}`,
		`{"sb":{"command":"sb-mcp","secrets":["ACME_B"]}}`,
		"ACME_A=bw:A\n", "ACME_B=bw:B\n")
	f := &fake{list: `[]`}
	o, _ := opts(h, f, "codex")
	o.Profiles, o.FallbackOnApply = ps, true
	if err := Run(o); err != nil {
		t.Fatal(err)
	}
	dot := filepath.Join(h, ".local", "bin", "dot")
	for _, want := range [][]string{
		{"codex", "mcp", "add", "sa", "--", dot, "-p", "a", "secrets", "run", "ACME_A", "--", "sa-mcp", "x"},
		{"codex", "mcp", "add", "sb", "--", dot, "-p", "b", "secrets", "run", "ACME_B", "--", "sb-mcp"},
		{"codex", "mcp", "add", "plain", "--", "p"},
	} {
		if !slices.ContainsFunc(f.calls, func(c []string) bool { return reflect.DeepEqual(c, want) }) {
			t.Errorf("missing %q in %q", want, f.calls)
		}
	}
}

func TestRunRefusesSecretDeclaredByTheOtherProfile(t *testing.T) {
	// Profile a uses a name only profile b declares: it would fail at start (or read b's value).
	h, ps := twoProfileHome(t, `{"sa":{"command":"sa-mcp","secrets":["ACME_B"]}}`, `{}`, "", "ACME_B=bw:B\n")
	f := &fake{list: `[]`}
	o, _ := opts(h, f, "codex")
	o.Profiles, o.FallbackOnApply = ps, true
	if err := Run(o); !errors.Is(err, ErrSource) || len(f.calls) != 0 {
		t.Fatalf("%v, calls %q", err, f.calls)
	}
}

func TestConvertLocalServerTakesFirstProfileDeclaringAllNames(t *testing.T) {
	two := []Owner{{"a", map[string]bool{"X": true}}, {"b", map[string]bool{"X": true, "Y": true}}, {"c", map[string]bool{"X": true, "Y": true}}}
	got, err := Convert(plain(map[string]any{}), src(t, `{"s":{"command":"c","secrets":["X","Y"]},"t":{"command":"c","secrets":["X"]}}`), two, "/dot")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"-p", "b", "secrets", "run", "X", "Y", "--", "c"}; !reflect.DeepEqual(got["s"].Args, want) {
		t.Errorf("s = %q", got["s"].Args)
	}
	if got["t"].Args[1] != "a" {
		t.Errorf("t = %q", got["t"].Args)
	}
	// Each name is known, but never by one profile: clear error.
	split := []Owner{{"a", map[string]bool{"X": true}}, {"b", map[string]bool{"Y": true}}}
	_, err = Convert(plain(map[string]any{}), src(t, `{"s":{"command":"c","secrets":["X","Y"]}}`), split, "/dot")
	if !errors.Is(err, ErrSource) || !strings.Contains(err.Error(), "serveur s") || !strings.Contains(err.Error(), "même profil") {
		t.Errorf("%v", err)
	}
	// An unknown name keeps the plain message.
	if _, err = Convert(plain(map[string]any{}), src(t, `{"s":{"command":"c","secrets":["Z"]}}`), split, "/dot"); err != ErrSource {
		t.Errorf("%v", err)
	}
}

func TestProfileKey(t *testing.T) {
	for dir, want := range map[string]string{"/h/.dot/acme": "acme", "/h/.dot/acme/sub": "", "/srv/deploy": "", "/h/.dot": ""} {
		if got := ProfileKey("/h", dir); got != want {
			t.Errorf("%s = %q", dir, got)
		}
	}
}
