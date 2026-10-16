package doctor

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const manifestJSON = `{"repo":"https://git.example.com/acme/dots.git","deploy":{"sparse":["home"]},` +
	`"profiles":{"demo":"home/.config/git/profiles/demo.gitconfig"},"deployProfile":"demo",` +
	`"marketplace":{"name":"acme","plugins":["acme"]},"modules":["settings"]}`

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// commitAll commits whatever the clone holds, so that it reports no local change.
func commitAll(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-qm", "change", "--allow-empty")
}

// sandbox isolates HOME, git and PATH, with a fictional mise and no claude or codex.
func sandbox(t *testing.T) (home string) {
	t.Helper()
	root := t.TempDir()
	home = filepath.Join(root, "home")
	bin := filepath.Join(root, "bin")
	write(t, filepath.Join(bin, "mise"), "#!/bin/sh\nexit 0\n", 0o755)
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("MISE_DATA_DIR", filepath.Join(root, "mise"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("DOTFILES_FORBIDDEN", "")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+filepath.Join(root, "mise", "shims")+string(os.PathListSeparator)+"/usr/bin:/bin")
	write(t, filepath.Join(home, ".config/git/profiles/demo.gitconfig"), "[user]\n  email = tester@example.com\n", 0o644)
	write(t, filepath.Join(home, ".config/git/profiles.gitconfig"), "# profiles\n", 0o644)
	if err := os.WriteFile(os.Getenv("GIT_CONFIG_GLOBAL"), []byte("[include]\n\tpath = ~/.config/git/profiles.gitconfig\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

// profile creates a healthy clone with one linked home file; it declares no mise config.
func profile(t *testing.T, home, key string) Profile { return newProfile(t, home, key, false) }

// miseProfile is profile plus a linked home/.config/mise/config.toml, which makes mise expected.
func miseProfile(t *testing.T, home, key string) Profile { return newProfile(t, home, key, true) }

func newProfile(t *testing.T, home, key string, mise bool) Profile {
	t.Helper()
	dir := filepath.Join(home, ".dot", key)
	write(t, filepath.Join(dir, "dot.json"), manifestJSON, 0o644)
	write(t, filepath.Join(dir, "home", "file-"+key), "linked\n", 0o644)
	if mise {
		write(t, filepath.Join(dir, "home", ".config", "mise", "config.toml"), "[tools]\n", 0o644)
	}
	write(t, filepath.Join(dir, "forbidden.local"), "acmecorp\n", 0o600)
	run(t, dir, "init", "-q", "-b", "main")
	run(t, dir, "config", "core.hooksPath", ".githooks")
	run(t, dir, "config", "user.email", "tester@example.com")
	run(t, dir, "config", "user.name", "Tester")
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-qm", "fixture")
	if err := os.Symlink(filepath.Join(dir, "home", "file-"+key), filepath.Join(home, "file-"+key)); err != nil {
		t.Fatal(err)
	}
	if mise {
		link := filepath.Join(home, ".config", "mise", "config.toml")
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(dir, "home", ".config", "mise", "config.toml"), link); err != nil {
			t.Fatal(err)
		}
	}
	return Profile{Key: key, Dir: dir}
}

func check(home string, ps ...Profile) []Line {
	return Run(Options{Home: home, Profiles: ps, Headers: len(ps) > 1,
		SrcRoot: filepath.Join(home, "src"), SandboxRoot: filepath.Join(home, "sandbox")})
}

// has and hasPrefix match the wording of a line. message kept: a Line carries a level and a text, no
// check id, so a check that is only present (an OK line) can be told only by its text. Needs a product
// change (a stable id on Line). The failures and warnings are matched by level instead: see problems.
func has(lines []Line, l Level, msg string) bool {
	return slices.Contains(lines, Line{l, msg})
}

func hasPrefix(lines []Line, l Level, prefix string) bool {
	return slices.ContainsFunc(lines, func(x Line) bool { return x.Level == l && strings.HasPrefix(x.Message, prefix) })
}

// problems lists the level of every line that reports something wrong, sorted. A healthy
// sandbox has none, so a single fault makes exactly one entry: the level (and the exit code) tell it.
func problems(lines []Line) []Level {
	var out []Level
	for _, l := range lines {
		if l.Level == Warn || l.Level == Fail {
			out = append(out, l.Level)
		}
	}
	slices.Sort(out) // by level: the order of the report is not the point
	return out
}

// levelLines returns the lines of one level.
func levelLines(lines []Line, l Level) (out []Line) {
	for _, x := range lines {
		if x.Level == l {
			out = append(out, x)
		}
	}
	return out
}

func text(lines []Line) string {
	var b bytes.Buffer
	_ = Write(&b, lines, false)
	return b.String()
}

func TestHealthySingleProfileKeepsHistoricalOrder(t *testing.T) {
	home := sandbox(t)
	lines := check(home, miseProfile(t, home, "alpha"))
	if HasFail(lines) {
		t.Fatalf("unexpected failure:\n%s", text(lines))
	}
	want := []string{
		"deployed clone present", "deployed clone hooks", "clone email: profile demo", "clone has no changes",
		"clone neither ahead nor behind, as far as known", "profile demo", "home links up to date", "no dead links in ~/.local/bin",
		"guard enabled", "profiles included in the global git config", "mise present", "mise tools installed",
		"mise shims in PATH", "no secrets declared", "repository identities consistent",
	}
	var got []string
	for _, l := range lines {
		if l.Level == Raw {
			t.Errorf("unexpected raw line: %q", l.Message)
		}
		got = append(got, l.Message)
	}
	// message kept: the order of the checks is only visible through their lines (no check id on Line).
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("different order or wording:\n%s", text(lines))
	}
}

func TestFailuresAndWarnings(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, home string, p Profile)
		level Level
	}{
		{"detached file", func(t *testing.T, home string, p Profile) {
			os.Remove(filepath.Join(home, "file-alpha"))
			write(t, filepath.Join(home, "file-alpha"), "x", 0o644)
		}, Fail},
		{"empty list", func(t *testing.T, _ string, p Profile) {
			write(t, filepath.Join(p.Dir, "forbidden.local"), "  # note\n\n \t\n", 0o600)
		}, Fail},
		{"invalid list", func(t *testing.T, _ string, p Profile) {
			write(t, filepath.Join(p.Dir, "forbidden.local"), "acmecorp\n[\n", 0o600)
		}, Fail},
		{"list with GNU operator", func(t *testing.T, _ string, p Profile) {
			write(t, filepath.Join(p.Dir, "forbidden.local"), "acmecorp\\|globex\n", 0o600)
		}, Fail},
		{"clone email", func(t *testing.T, _ string, p Profile) {
			run(t, p.Dir, "config", "user.email", "other@example.com")
		}, Fail},
		{"hooks", func(t *testing.T, _ string, p Profile) {
			run(t, p.Dir, "config", "core.hooksPath", ".husky")
		}, Fail},
		{"modified clone", func(t *testing.T, _ string, p Profile) {
			write(t, filepath.Join(p.Dir, "new"), "x", 0o644)
		}, Warn},
		{"dead link", func(t *testing.T, home string, _ Profile) {
			write(t, filepath.Join(home, ".local/bin/.keep"), "", 0o644)
			os.Symlink(filepath.Join(home, "absent"), filepath.Join(home, ".local/bin/dead"))
		}, Fail},
		{"git profile without email", func(t *testing.T, home string, _ Profile) {
			write(t, filepath.Join(home, ".config/git/profiles/demo.gitconfig"), "[user]\n  name = T\n", 0o644)
		}, Fail},
		{"shims outside PATH", func(t *testing.T, _ string, _ Profile) { t.Setenv("MISE_DATA_DIR", "/nonexistent") },
			Warn},
		{"missing include", func(t *testing.T, _ string, _ Profile) {
			write(t, os.Getenv("GIT_CONFIG_GLOBAL"), "", 0o644)
		}, Fail},
		{"locked secrets", func(t *testing.T, _ string, p Profile) {
			t.Setenv("BW_SESSION", "")
			write(t, filepath.Join(p.Dir, "secrets.local"), "FAKE_TOKEN=bw:fictional-item\n", 0o600)
		}, Warn},
		{"malformed secrets.local", func(t *testing.T, _ string, p Profile) {
			write(t, filepath.Join(p.Dir, "secrets.local"), "this is not a mapping\n", 0o600)
		}, Fail},
		{"duplicate secrets.local", func(t *testing.T, _ string, p Profile) {
			write(t, filepath.Join(p.Dir, "secrets.local"), "FAKE=bw:one\nFAKE=bw:two\n", 0o600)
		}, Fail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := sandbox(t)
			p := profile(t, home, "alpha")
			tc.setup(t, home, p)
			if tc.name != "modified clone" { // the fault alone: the files it touched are committed, the clone stays clean
				commitAll(t, p.Dir)
			}
			lines := check(home, p)
			// a fault may be reported by several checks (a profile without email also breaks the clone's), never at another level
			got := problems(lines)
			if len(got) == 0 || got[0] != tc.level || got[len(got)-1] != tc.level {
				t.Fatalf("levels %v, want [%s]:\n%s", got, tc.level.Symbol(), text(lines))
			}
			if HasFail(lines) != (tc.level == Fail) {
				t.Fatalf("HasFail = %v :\n%s", HasFail(lines), text(lines))
			}
			if out := text(lines); strings.Contains(out, "fictional-item") || strings.Contains(out, "acmecorp") {
				t.Errorf("sensitive value shown:\n%s", out)
			}
		})
	}
}

func TestGuardUsesTheGuardValidationAndNeverShowsPatterns(t *testing.T) {
	home := sandbox(t)
	p := profile(t, home, "alpha")
	write(t, filepath.Join(p.Dir, "forbidden.local"), "globex-secret(\n", 0o600)
	commitAll(t, p.Dir)
	lines := check(home, p)
	if got := problems(lines); !slices.Equal(got, []Level{Fail}) {
		t.Fatalf("invalid list accepted:\n%s", text(lines))
	}
	if strings.Contains(text(lines), "globex") {
		t.Errorf("pattern shown:\n%s", text(lines))
	}
	// $DOTFILES_FORBIDDEN is validated the same way.
	list := filepath.Join(home, "list")
	write(t, list, "acmecorp\n", 0o600)
	t.Setenv("DOTFILES_FORBIDDEN", list)
	if lines := check(home, p); problems(lines) != nil {
		t.Errorf("valid list via DOTFILES_FORBIDDEN:\n%s", text(lines))
	}
	write(t, list, "[\n", 0o600)
	if lines := check(home, p); !slices.Equal(problems(lines), []Level{Fail}) {
		t.Errorf("invalid list via DOTFILES_FORBIDDEN:\n%s", text(lines))
	}
}

func TestDetachedListIsCappedAtTen(t *testing.T) {
	home := sandbox(t)
	p := profile(t, home, "alpha")
	for i := 0; i < 12; i++ {
		write(t, filepath.Join(p.Dir, "home", "extra", string(rune('a'+i))), "x", 0o644)
	}
	lines := check(home, p)
	if fails := levelLines(lines, Fail); len(fails) != 1 || !strings.Contains(fails[0].Message, "12") {
		t.Fatalf("count missing:\n%s", text(lines))
	}
	raw := 0
	for _, l := range lines {
		if l.Level == Raw && strings.Contains(l.Message, "~/extra/") {
			raw++
		}
	}
	if raw != 10 {
		t.Errorf("%d files listed, want 10", raw)
	}
}

func TestMissingCloneAndMiseTools(t *testing.T) {
	home := sandbox(t)
	lines := check(home, Profile{Key: "gone", Dir: filepath.Join(home, ".dot", "gone")})
	// a missing clone: no dot.json, no home, no list of terms; the manifest-dependent checks are skipped
	if got := problems(lines); !slices.Equal(got, []Level{Warn, Fail, Fail, Fail, Fail}) {
		t.Errorf("missing clone, levels %v:\n%s", got, text(lines))
	}
	write(t, filepath.Join(filepath.Dir(os.Getenv("MISE_DATA_DIR")), "bin", "mise"),
		"#!/bin/sh\n[ \"$MISE_OFFLINE\" = true ] || exit 1\nprintf 'node 1\\nnode 2\\npython 3\\n'\n", 0o755)
	lines = check(home)
	if fails := levelLines(lines, Fail); len(fails) != 1 || !strings.Contains(fails[0].Message, "node") || !strings.Contains(fails[0].Message, "python") {
		t.Errorf("missing tools:\n%s", text(lines))
	}
	write(t, filepath.Join(filepath.Dir(os.Getenv("MISE_DATA_DIR")), "bin", "mise"), "#!/bin/sh\nexit 1\n", 0o755)
	if lines := check(home); !slices.Equal(problems(lines), []Level{Fail}) {
		t.Errorf("mise failing:\n%s", text(lines))
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	os.Remove(filepath.Join(filepath.Dir(os.Getenv("MISE_DATA_DIR")), "bin", "mise"))
	if lines := check(home); hasMise(lines) {
		t.Errorf("mise without profile or binary, nothing expected:\n%s", text(lines))
	}
	if lines := check(home, miseProfile(t, home, "alpha")); !HasFail(lines) {
		t.Errorf("mise missing:\n%s", text(lines))
	}
}

// hasMise: message kept, like has (no check id on Line).
func hasMise(lines []Line) bool {
	return slices.ContainsFunc(lines, func(l Line) bool { return strings.Contains(l.Message, "mise") })
}

// noMise removes the sandbox mise from the PATH, as on a machine whose profiles do not ask for it.
func noMise(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", "/usr/bin:/bin")
}

func TestMiseChecksFollowTheProfileConfig(t *testing.T) {
	t.Run("no config and no mise: silent, code 0", func(t *testing.T) {
		home := sandbox(t)
		noMise(t)
		lines := check(home, profile(t, home, "alpha"))
		if hasMise(lines) {
			t.Errorf("unexpected mise line:\n%s", text(lines))
		}
		if HasFail(lines) {
			t.Errorf("unexpected failure:\n%s", text(lines))
		}
	})
	t.Run("config without mise: failure", func(t *testing.T) {
		home := sandbox(t)
		noMise(t)
		lines := check(home, miseProfile(t, home, "alpha"))
		// the config asks for mise: it is missing (a failure) and the shims are checked too (a warning)
		if got := problems(lines); !slices.Equal(got, []Level{Warn, Fail}) {
			t.Errorf("mise missing expected:\n%s", text(lines))
		}
	})
	t.Run("no config, mise in ~/.local/bin: checked", func(t *testing.T) {
		home := sandbox(t)
		noMise(t)
		write(t, filepath.Join(home, ".local", "bin", "mise"), "#!/bin/sh\nexit 0\n", 0o755)
		lines := check(home, profile(t, home, "alpha"))
		if !has(lines, OK, "mise present") || !has(lines, OK, "mise tools installed") {
			t.Errorf("mise present expected:\n%s", text(lines))
		}
	})
	t.Run("no config, mise in PATH: checked", func(t *testing.T) {
		home := sandbox(t)
		if lines := check(home, profile(t, home, "alpha")); !has(lines, OK, "mise present") {
			t.Errorf("mise present expected:\n%s", text(lines))
		}
	})
	t.Run("only one of two profiles declares the config", func(t *testing.T) {
		home := sandbox(t)
		noMise(t)
		lines := check(home, profile(t, home, "alpha"), miseProfile(t, home, "beta"))
		if !HasFail(lines) {
			t.Errorf("mise missing expected:\n%s", text(lines))
		}
	})
	t.Run("no profile and no mise: silent", func(t *testing.T) {
		home := sandbox(t)
		noMise(t)
		if lines := check(home); hasMise(lines) {
			t.Errorf("unexpected mise line:\n%s", text(lines))
		}
	})
}

func TestMultiProfileHeadersAndMachineChecksOnce(t *testing.T) {
	home := sandbox(t)
	a, b := profile(t, home, "alpha"), profile(t, home, "beta")
	write(t, filepath.Join(b.Dir, "forbidden.local"), "", 0o600)
	lines := check(home, a, b)
	var order []string
	for _, l := range lines {
		if l.Level != Raw {
			continue
		}
		for _, key := range []string{"alpha", "beta"} {
			if strings.Contains(l.Message, key) {
				order = append(order, key)
			}
		}
	}
	if !slices.Equal(order, []string{"alpha", "beta"}) {
		t.Fatalf("headers: %v", order)
	}
	// message kept below: which check ran once or per profile is only visible through the lines.
	count := func(msg string) (n int) {
		for _, l := range lines {
			if l.Message == msg {
				n++
			}
		}
		return n
	}
	for _, once := range []string{"no dead links in ~/.local/bin", "mise present", "mise shims in PATH",
		"profiles included in the global git config", "repository identities consistent"} {
		if n := count(once); n != 1 {
			t.Errorf("%q: %d times", once, n)
		}
	}
	if count("deployed clone present") != 2 || count("guard enabled") != 1 || count("guard disabled: restore forbidden.local") != 1 {
		t.Errorf("per-profile checks:\n%s", text(lines))
	}
	idx := func(msg string) int { return slices.IndexFunc(lines, func(l Line) bool { return l.Message == msg }) }
	if idx("no dead links in ~/.local/bin") < idx("profile beta") || idx("guard disabled: restore forbidden.local") < idx("profile beta") {
		t.Errorf("profiles come before the machine checks:\n%s", text(lines))
	}
}

func TestTargetErrorAndInvalidManifest(t *testing.T) {
	home := sandbox(t)
	resolveErr := errors.New("empty registry (cause supplied by the caller)")
	lines := Run(Options{Home: home, Err: resolveErr,
		SrcRoot: filepath.Join(home, "src"), SandboxRoot: filepath.Join(home, "sandbox")})
	if lines[0] != (Line{Fail, resolveErr.Error()}) || !has(lines, OK, "mise present") { // message kept: the machine checks still run, only their lines tell
		t.Errorf("empty registry:\n%s", text(lines))
	}
	p := profile(t, home, "alpha")
	write(t, filepath.Join(p.Dir, "dot.json"), `{"unexpected":true}`, 0o644)
	lines = check(home, p)
	if got := problems(lines); !slices.Equal(got, []Level{Warn, Warn, Fail}) || !strings.Contains(levelLines(lines, Fail)[0].Message, "unexpected") {
		t.Errorf("invalid manifest:\n%s", text(lines))
	}
	for _, l := range lines { // message kept: the checks that depend on the manifest are only known by their lines
		if strings.HasPrefix(l.Message, "clone email") || strings.HasPrefix(l.Message, "profile demo") {
			t.Errorf("manifest-dependent check run: %q", l.Message)
		}
	}
}

func TestWrite(t *testing.T) {
	lines := []Line{{OK, "a"}, {Warn, "b"}, {Fail, "c"}, {Raw, "  d"}}
	if got := text(lines); got != "✓ a\n! b\n✗ c\n  d\n" {
		t.Errorf("no color: %q", got)
	}
	var b bytes.Buffer
	_ = Write(&b, lines, true)
	if want := "\033[32m✓\033[0m a\n\033[33m!\033[0m b\n\033[31m✗\033[0m c\n  d\n"; b.String() != want {
		t.Errorf("with color: %q", b.String())
	}
	if HasFail(lines[:2]) || !HasFail(lines) {
		t.Error("HasFail")
	}
}

func TestSkillsModuleNeedsNpx(t *testing.T) {
	home := sandbox(t)
	p := profile(t, home, "demo")
	write(t, filepath.Join(p.Dir, "dot.json"), strings.Replace(manifestJSON, `["settings"]`, `["skills"]`, 1), 0o644)
	commitAll(t, p.Dir)
	if _, err := exec.LookPath("npx"); err != nil {
		if !has(check(home, p), Fail, "skills module: npx not found, install Node (nvm key of dot.json)") {
			t.Fatal("missing npx not reported")
		}
	}
	write(t, filepath.Join(filepath.Dir(home), "bin", "npx"), "#!/bin/sh\nexit 0\n", 0o755)
	if !has(check(home, p), OK, "npx found for the skills module") {
		t.Fatal("npx not found")
	}
	if has(check(home, profile(t, home, "other")), OK, "npx found for the skills module") {
		t.Fatal("checked without the skills module")
	}
}
