package settings

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustParse(t *testing.T, s string) map[string]any {
	t.Helper()
	m, err := Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMergeBaseWinsObjectsDeep(t *testing.T) {
	local := mustParse(t, `{"env":{"A":"local","B":"keep"},"theme":"dark","extra":true,"list":["x","y"],"n":{"a":1}}`)
	base := mustParse(t, `{"env":{"A":"base"},"theme":"auto","list":["z"],"n":null}`)
	got, err := Normalize(Merge(local, base))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"env\": {\n    \"A\": \"base\",\n    \"B\": \"keep\"\n  },\n  \"extra\": true,\n  \"list\": [\n    \"z\"\n  ],\n  \"n\": null,\n  \"theme\": \"auto\"\n}\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if local["theme"] != "dark" {
		t.Fatal("Merge modified its input")
	}
}

func TestNormalizeKeepsLiteralsAndNoHTMLEscape(t *testing.T) {
	m := mustParse(t, `{"b":12345678901234567890,"a":"<&>","c":1.50}`)
	got, _ := Normalize(m)
	for _, s := range []string{`12345678901234567890`, `"<&>"`, `1.50`} {
		if !strings.Contains(got, s) {
			t.Errorf("%q missing from %s", s, got)
		}
	}
	if strings.Index(got, `"a"`) > strings.Index(got, `"b"`) {
		t.Error("keys not sorted")
	}
}

func TestParseRejects(t *testing.T) {
	for _, s := range []string{`invalid json`, `[]`, `"s"`, `{} {}`, `{"a":1}x`, ``} {
		if _, err := Parse([]byte(s)); err == nil {
			t.Errorf("Parse(%q) accepted", s)
		}
	}
}

func TestDiffMatchesSystemDiff(t *testing.T) {
	// Diff follows GNU diff's hunk headers; BSD diff (macOS) writes +1,0 where GNU writes +0,0.
	if v, err := exec.Command("diff", "--version").Output(); err != nil || !strings.Contains(string(v), "GNU") {
		t.Skip("GNU diff absent")
	}
	cases := [][2]string{
		{"{}\n", "{\n  \"a\": 1\n}\n"},
		{"a\nb\nc\nd\ne\n", "a\nB\nc\nd\ne\nf\n"},
		{"a\nb\nc\n", "c\n"},
		{"a\nb\nc\n", "a\nb\nc\nd\ne\n"},
		{"x\ny\n", "x\nz\ny\n"},
		{"1\n2\n3\n4\n5\n6\n", "1\n3\n4\n6\n7\n"},
		{"a\n", "b\n"},
	}
	dir := t.TempDir()
	for i, c := range cases {
		fa, fb := filepath.Join(dir, "a"), filepath.Join(dir, "b")
		os.WriteFile(fa, []byte(c[0]), 0o600)
		os.WriteFile(fb, []byte(c[1]), 0o600)
		want, err := exec.Command("diff", "-U0", "--label", "settings.json", "--label", "settings.json+base", fa, fb).Output()
		if err != nil && err.(*exec.ExitError).ExitCode() != 1 {
			t.Fatal(err)
		}
		if got := Diff("settings.json", "settings.json+base", c[0], c[1]); got != string(want) {
			t.Errorf("case %d:\n got %q\nwant %q", i, got, want)
		}
	}
	if Diff("a", "b", "same\n", "same\n") != "" {
		t.Error("equal texts must give an empty diff")
	}
}

func setup(t *testing.T) (home string, base string) {
	t.Helper()
	home = t.TempDir()
	base = filepath.Join(t.TempDir(), "settings.base.json")
	os.WriteFile(base, []byte(`{"env":{"GENERIC_PREF":"base"},"theme":"auto"}`), 0o600)
	return
}

func run(t *testing.T, o Options) (string, error) {
	t.Helper()
	var out bytes.Buffer
	o.Out = &out
	o.Now = func() time.Time { return time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC) }
	err := Run(o)
	return out.String(), err
}

func TestRunLifecycle(t *testing.T) {
	home, base := setup(t)
	target := filepath.Join(home, ".claude", "settings.json")

	out, err := run(t, Options{Home: home, Bases: []string{base}, DryRun: true})
	if err != nil || !strings.Contains(out, "+    \"GENERIC_PREF\": \"base\"") {
		t.Fatalf("dry: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); err == nil {
		t.Fatal("dry run wrote something")
	}

	if _, err := run(t, Options{Home: home, Bases: []string{base}}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(target, []byte(`{"env":{"GENERIC_PREF":"local","LOCAL_PREF":"secret-keep"},"theme":"dark","extra":true}`), 0o644)
	before, _ := os.ReadFile(target)

	out, err = run(t, Options{Home: home, Bases: []string{base}, DryRun: true})
	if err != nil || strings.Contains(out, "LOCAL_PREF") || strings.Contains(out, "extra") {
		t.Fatalf("dry diff must not show context: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".local")); err == nil {
		t.Fatal("dry run made a backup")
	}

	out, err = run(t, Options{Home: home, Bases: []string{base}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "settings : sauvegarde → ~/.local/state/dotfiles/backup/20261006-120000-") || !strings.HasSuffix(out, "/.claude/settings.json\nsettings : mis à jour\n") {
		t.Fatalf("messages:\n%s", out)
	}
	m, _ := filepath.Glob(filepath.Join(home, ".local/state/dotfiles/backup/*/.claude/settings.json"))
	if len(m) != 1 {
		t.Fatalf("backups: %v", m)
	}
	if got, _ := os.ReadFile(m[0]); string(got) != string(before) {
		t.Fatal("backup differs from the original")
	}
	st, _ := os.Stat(target)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	cur := mustParse(t, string(mustRead(t, target)))
	if cur["theme"] != "auto" || cur["extra"] != true {
		t.Fatalf("merge: %v", cur)
	}

	out, err = run(t, Options{Home: home, Bases: []string{base}})
	if err != nil || out != "settings: à jour\n" {
		t.Fatalf("second run: %v %q", err, out)
	}
	m, _ = filepath.Glob(filepath.Join(home, ".local/state/dotfiles/backup/*/.claude/settings.json"))
	if len(m) != 1 {
		t.Fatal("second run made a backup")
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRunRejectsInvalidTargetWithoutReplacing(t *testing.T) {
	home, base := setup(t)
	target := filepath.Join(home, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(target), 0o700)
	os.WriteFile(target, []byte("invalid json\n"), 0o600)
	if _, err := run(t, Options{Home: home, Bases: []string{base}}); err == nil {
		t.Fatal("invalid target accepted")
	}
	if string(mustRead(t, target)) != "invalid json\n" {
		t.Fatal("invalid target replaced")
	}
}

func TestRunMultiProfileOrderAndSkip(t *testing.T) {
	home := t.TempDir()
	d := t.TempDir()
	a, b, missing := filepath.Join(d, "a.json"), filepath.Join(d, "b.json"), filepath.Join(d, "none.json")
	os.WriteFile(a, []byte(`{"theme":"a","only_a":1,"env":{"X":"a"}}`), 0o600)
	os.WriteFile(b, []byte(`{"theme":"b","env":{"Y":"b"}}`), 0o600)
	if _, err := run(t, Options{Home: home, Bases: []string{a, missing, b}, SkipMissing: true}); err != nil {
		t.Fatal(err)
	}
	got := mustParse(t, string(mustRead(t, filepath.Join(home, ".claude", "settings.json"))))
	env := got["env"].(map[string]any)
	if got["theme"] != "b" || env["X"] != "a" || env["Y"] != "b" {
		t.Fatalf("merged: %v", got)
	}
	if _, err := run(t, Options{Home: t.TempDir(), Bases: []string{a, missing}}); err == nil {
		t.Fatal("missing base accepted without SkipMissing")
	}
	if _, err := run(t, Options{Home: t.TempDir(), Bases: []string{missing}, SkipMissing: true}); err == nil {
		t.Fatal("no base at all accepted")
	}
}
