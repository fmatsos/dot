package crontab

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApply(t *testing.T) {
	block := "# >>> dot >>>\n0 9 * * 1 $HOME/bin/acme-task\n# <<< dot <<<\n"
	old := "# >>> dot >>>\n0 8 * * 1 $HOME/bin/acme-task\n# <<< dot <<<\n"
	for _, tc := range []struct {
		name, current, block, want string
	}{
		{"empty current", "", block, block},
		{"foreign lines", "# foreign\n0 7 * * * acme-task\n", block, "# foreign\n0 7 * * * acme-task\n" + block},
		{"replace between foreign lines", "# before\n" + old + "# after\n\n", block, "# before\n" + block + "# after\n\n"},
		{"remove", "# before\n" + old + "# after\n", "", "# before\n# after\n"},
		{"no final newline", "# foreign", block, "# foreign\n" + block},
		{"empty new content", "# foreign", "", "# foreign"},
		{"remove only block", old, "", ""},
		{"end marker without newline", strings.TrimSuffix(old, "\n"), block, block},
		{"foreign suffix without newline", "# before\n" + old + "# after", block, "# before\n" + block + "# after"},
		{"markers must occupy a line", "# >>> dot >>> foreign\n# <<< dot <<< foreign\n", block, "# >>> dot >>> foreign\n# <<< dot <<< foreign\n" + block},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Apply(tc.current, tc.block); got != tc.want {
				t.Fatalf("Apply = %q, want %q", got, tc.want)
			}
		})
	}
}

// sandbox puts a crontab stub first in PATH and isolates all user state.
func sandbox(t *testing.T) string {
	t.Helper()
	s := t.TempDir()
	bin := filepath.Join(s, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", s)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(s, ".config"))
	t.Setenv("CODEX_HOME", filepath.Join(s, ".codex"))
	t.Setenv("S", s)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	stub := `#!/bin/sh
set -eu
case "$1" in
  -l)
    if [ -e "$S/read-error" ]; then
      cat "$S/read-error" >&2
      exit 1
    fi
    if [ -f "$S/crontab.txt" ]; then
      cat "$S/crontab.txt"
    else
      echo 'no crontab for test' >&2
      exit 1
    fi ;;
  -)
    if [ -e "$S/write-error" ]; then
      echo 'installation refusée' >&2
      exit 1
    fi
    cat >"$S/crontab.txt"
    echo write >>"$S/writes" ;;
  *) exit 2 ;;
esac
`
	write(t, filepath.Join(bin, "crontab"), stub, 0o700)
	return s
}

func write(t *testing.T, path, text string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRunSourcesAndIdempotence(t *testing.T) {
	s := sandbox(t)
	a, b, empty := filepath.Join(s, "a"), filepath.Join(s, "b"), filepath.Join(s, "empty")
	write(t, a, "0 9 * * 1 $HOME/bin/acme-task\n\n\n", 0o600)
	write(t, b, "0 10 * * 2 $HOME/bin/acme-task", 0o600)
	write(t, empty, "\n\n", 0o600)
	var out bytes.Buffer
	o := Options{Sources: []string{a, filepath.Join(s, "missing"), empty, b}, Out: &out}
	if err := Run(o); err != nil {
		t.Fatal(err)
	}
	want := "# >>> dot >>>\n0 9 * * 1 $HOME/bin/acme-task\n0 10 * * 2 $HOME/bin/acme-task\n# <<< dot <<<\n"
	if got := read(t, filepath.Join(s, "crontab.txt")); got != want {
		t.Fatalf("crontab = %q, want %q", got, want)
	}
	if out.String() != "crontab: mis à jour\n" {
		t.Fatalf("output = %q", out.String())
	}
	out.Reset()
	if err := Run(o); err != nil {
		t.Fatal(err)
	}
	if out.String() != "crontab: à jour\n" || read(t, filepath.Join(s, "writes")) != "write\n" {
		t.Fatalf("second run = %q, crontab rewritten", out.String())
	}
}

func TestRunDryRunAndRemoval(t *testing.T) {
	s := sandbox(t)
	source := filepath.Join(s, "source")
	write(t, source, "0 9 * * 1 $HOME/bin/acme-task\n", 0o600)
	current := "# foreign\n# >>> dot >>>\n0 8 * * 1 $HOME/bin/acme-task\n# <<< dot <<<\n# after"
	target := filepath.Join(s, "crontab.txt")
	write(t, target, current, 0o600)
	var out bytes.Buffer
	o := Options{Sources: []string{source}, DryRun: true, Out: &out}
	if err := Run(o); err != nil {
		t.Fatal(err)
	}
	if out.String() != "[dry] crontab:\n# >>> dot >>>\n0 9 * * 1 $HOME/bin/acme-task\n# <<< dot <<<\n" || read(t, target) != current {
		t.Fatalf("dry run = %q, current crontab changed", out.String())
	}
	write(t, source, "", 0o600)
	out.Reset()
	if err := Run(o); err != nil {
		t.Fatal(err)
	}
	if out.String() != "[dry] crontab: bloc retiré\n" || read(t, target) != current {
		t.Fatalf("dry removal = %q, current crontab changed", out.String())
	}
	if _, err := os.Stat(filepath.Join(s, "writes")); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote: %v", err)
	}
	o.DryRun = false
	if err := Run(o); err != nil {
		t.Fatal(err)
	}
	if got := read(t, target); got != "# foreign\n# after" {
		t.Fatalf("removal = %q", got)
	}
}

func TestRunMissingCommand(t *testing.T) {
	s := sandbox(t)
	t.Setenv("PATH", t.TempDir())
	var out bytes.Buffer
	o := Options{Sources: []string{filepath.Join(s, "missing")}, Out: &out}
	if err := Run(o); err != nil || out.Len() != 0 {
		t.Fatalf("empty sources require crontab: %v, %q", err, out.String())
	}
	source := filepath.Join(s, "source")
	write(t, source, "\n\n", 0o600)
	o.Sources = []string{source}
	if err := Run(o); err != nil {
		t.Fatal(err)
	}
	write(t, source, "0 9 * * 1 $HOME/bin/acme-task\n", 0o600)
	if err := Run(o); err == nil || !strings.Contains(err.Error(), "crontab introuvable") {
		t.Fatalf("missing command error = %v", err)
	}
}

func TestRunFailuresPreserveCurrent(t *testing.T) {
	for _, failure := range []string{"read", "write", "source"} {
		t.Run(failure, func(t *testing.T) {
			s := sandbox(t)
			target := filepath.Join(s, "crontab.txt")
			write(t, target, "# foreign\n", 0o600)
			source := filepath.Join(s, "source")
			write(t, source, "0 9 * * 1 $HOME/bin/acme-task\n", 0o600)
			switch failure {
			case "read":
				write(t, filepath.Join(s, "read-error"), "lecture refusée\n", 0o600)
			case "write":
				write(t, filepath.Join(s, "write-error"), "", 0o600)
			case "source":
				source = s // reading a directory must fail before calling crontab
			}
			var out bytes.Buffer
			if err := Run(Options{Sources: []string{source}, Out: &out}); err == nil {
				t.Fatal("failure ignored")
			}
			if read(t, target) != "# foreign\n" || out.Len() != 0 {
				t.Fatalf("failure changed crontab or printed success: %q", out.String())
			}
			if _, err := os.Stat(filepath.Join(s, "writes")); !os.IsNotExist(err) {
				t.Fatalf("failure wrote: %v", err)
			}
		})
	}
}
