package main

import (
	"strings"
	"testing"
)

func TestColorFlags(t *testing.T) {
	const usage = "  -h, --help             show the help\n  -p, --profile string   target profile\n      --dry-run          do not write\n"
	if got := colorFlags(usage); got != usage {
		t.Fatalf("colors off a terminal: %q", got)
	}
	helpColor.Store(true)
	t.Cleanup(func() { helpColor.Store(false) })
	const want = "  \x1b[36m-h, --help\x1b[0m             show the help\n" +
		"  \x1b[36m-p, --profile string\x1b[0m   target profile\n" +
		"      \x1b[36m--dry-run\x1b[0m          do not write\n"
	if got := colorFlags(usage); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestHelpIsPlainOffATerminal(t *testing.T) {
	env, out, _ := testEnv(t)
	if code := execute(env, nil); code != 0 || out.Len() == 0 {
		t.Fatalf("code %d, %d bytes", code, out.Len())
	}
	if helpColor.Load() || strings.Contains(out.String(), "\x1b[") {
		t.Fatal("escape sequences in a buffer")
	}
}
