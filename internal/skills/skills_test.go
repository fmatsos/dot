package skills

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseRejectsWhatTheCLIWouldMisread(t *testing.T) {
	for _, bad := range []string{`[]`, `null`, `{"-g":["a"]}`, `{"o/r":[]}`, `{"o/r":["-y"]}`, `{"o/r":["*"]}`, `{"o/r":"a"}`} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	l, err := Parse([]byte(`{"o/r#v1":["a","b"],"x/y":null}`))
	if err != nil || !reflect.DeepEqual(l, List{"o/r#v1": {"a", "b"}, "x/y": nil}) {
		t.Fatalf("%v %v", l, err)
	}
}

func TestMergeUnionsProfilesThenLocalWins(t *testing.T) {
	want, err := Merge([]List{{"o/r": {"a"}, "x/y#v1": {"c"}}, {"o/r": {"b", "a"}}}, List{"x/y": nil, "z/w": {"d"}})
	if err != nil || !reflect.DeepEqual(want, map[string]string{"a": "o/r", "b": "o/r", "d": "z/w"}) {
		t.Fatalf("%v %v", want, err)
	}
	if _, err := Merge([]List{{"o/r": {"a"}}, {"x/y": {"a"}}}, nil); err == nil {
		t.Fatal("one name from two sources accepted")
	}
}

func TestPlanLeavesSkillsWithoutSourceAlone(t *testing.T) {
	steps := Plan(map[string]string{"a": "https://github.com/O/R.git#main", "b": "o/r", "c": "n/m"},
		[]Installed{{"a", "o/r"}, {"b", "other/r"}, {"hand", ""}, {"old", "o/r"}})
	exp := []Step{{"add", "b", "o/r"}, {"add", "c", "n/m"}, {"unmanaged", "hand", ""}, {"remove", "old", ""}}
	if !reflect.DeepEqual(steps, exp) {
		t.Fatalf("%v", steps)
	}
}

type fake struct {
	list  string
	calls []string
	fail  string
}

func (f *fake) opts(t *testing.T, sources ...string) (Options, *bytes.Buffer) {
	out := &bytes.Buffer{}
	return Options{
		Home: t.TempDir(), Sources: sources, Out: out,
		LookPath: func(string) (string, error) { return "/bin/npx", nil },
		Exec: func(name string, args ...string) ([]byte, error) {
			call := name + " " + strings.Join(args, " ")
			f.calls = append(f.calls, call)
			if f.fail != "" && strings.Contains(call, f.fail) {
				return nil, errors.New("exit 1")
			}
			if strings.Contains(call, " list ") {
				return []byte(f.list), nil
			}
			return nil, nil
		},
	}, out
}

func write(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunAppliesThePlanThroughThePinnedCLI(t *testing.T) {
	f := &fake{list: `[{"name":"a","source":"o/r"},{"name":"old","source":"o/r"},{"name":"hand","source":null}]`}
	dir := t.TempDir()
	p1 := write(t, filepath.Join(dir, "p1.json"), `{"o/r":["a","b"]}`)
	o, out := f.opts(t, p1, filepath.Join(dir, "missing.json"))
	write(t, Local(o.Home), `{"n/m":["c"]}`)

	o.DryRun = true
	if err := Run(o); err != nil {
		t.Fatal(err)
	}
	if out.String() != "skills: add b (o/r)\nskills: add c (n/m)\nskills: unmanaged hand\nskills: remove old\n" || len(f.calls) != 1 {
		t.Fatalf("%q %v", out, f.calls)
	}
	f.calls, o.DryRun = nil, false
	if err := Run(o); err != nil {
		t.Fatal(err)
	}
	exp := []string{
		"npx -y " + CLI + " list -g --json",
		"npx -y " + CLI + " remove old -g -y",
		"npx -y " + CLI + " add n/m -g -y -s c",
		"npx -y " + CLI + " add o/r -g -y -s b",
	}
	if !reflect.DeepEqual(f.calls, exp) {
		t.Fatalf("%q", f.calls)
	}
}

func TestRunUpToDateAndErrors(t *testing.T) {
	f := &fake{list: `[{"name":"a","source":"o/r"},{"name":"hand","source":null}]`}
	dir := t.TempDir()
	o, out := f.opts(t, write(t, filepath.Join(dir, "p.json"), `{"o/r":["a"]}`))
	if err := Run(o); err != nil || out.String() != "skills: unmanaged hand\nskills: up to date\n" || len(f.calls) != 1 {
		t.Fatalf("%v %q %v", err, out, f.calls)
	}

	bad := write(t, filepath.Join(dir, "bad.json"), `{"o/r":"a"}`)
	f.calls = nil
	o, _ = f.opts(t, bad)
	if err := Run(o); err == nil || len(f.calls) != 0 {
		t.Fatalf("invalid list: %v %v", err, f.calls)
	}

	o, _ = f.opts(t)
	o.LookPath = func(string) (string, error) { return "", errors.New("none") }
	if err := Run(o); err == nil || !strings.Contains(err.Error(), "npx") {
		t.Fatalf("no npx: %v", err)
	}

	f = &fake{list: `[]`, fail: " add "}
	o, _ = f.opts(t, write(t, filepath.Join(dir, "p2.json"), `{"o/r":["a"]}`))
	if err := Run(o); err == nil || err.Error() != "cannot add (a from o/r)" {
		t.Fatalf("failed add: %v", err)
	}
}

func TestListEditing(t *testing.T) {
	l := List{"https://github.com/o/r#v1": {"a"}}
	AddTo(l, "o/r", "b", "a")
	Adopt(l, []Installed{{"c", "x/y"}, {"hand", ""}, {"a", "o/r"}})
	if !reflect.DeepEqual(l, List{"https://github.com/o/r#v1": {"a", "b"}, "x/y": {"c"}}) {
		t.Fatalf("%v", l)
	}
	if missing := RemoveFrom(l, "c", "a", "zz"); !reflect.DeepEqual(missing, []string{"zz"}) ||
		!reflect.DeepEqual(l, List{"https://github.com/o/r#v1": {"b"}}) {
		t.Fatalf("%v %v", missing, l)
	}
	p := filepath.Join(t.TempDir(), "sub", "skills.json")
	if err := WriteList(p, l); err != nil {
		t.Fatal(err)
	}
	if back, err := ReadList(p); err != nil || !reflect.DeepEqual(back, l) {
		t.Fatalf("%v %v", back, err)
	}
	if empty, err := ReadList(p + ".missing"); err != nil || len(empty) != 0 {
		t.Fatalf("%v %v", empty, err)
	}
}
