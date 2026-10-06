package clone

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPath(t *testing.T) {
	const root = "/r/source repos"
	for _, tc := range []struct{ url, want string }{
		{"https://github.com/alice/tool.git", root + "/github.com/alice/tool"},
		{"git@git.example.com:team/sub/deep/project.git", root + "/git.example.com/team/project"},
		{"ssh://git@git.example.com:2222/team/sub/project", root + "/git.example.com/team/project"},
		{"https://user@git.example.com/team/project/", root + "/git.example.com/team/project"},
		{"https://user@GIT.EXAMPLE.COM:443/team/project.git/", root + "/git.example.com/team/project"},
		{"https://git.example.com/team/pro.ject", root + "/git.example.com/team/pro.ject"},
	} {
		if got, err := Path(tc.url, root); err != nil || got != tc.want {
			t.Errorf("Path(%q) = %q, %v (attendu %q)", tc.url, got, err, tc.want)
		}
	}
}

func TestPathRejects(t *testing.T) {
	for _, url := range []string{
		"", "project", "/team/project", "file:///team/project", "ftp://git.example.com/team/project",
		"http://git.example.com/team/project", "https://git.example.com/team",
		"git@git.example.com:project", "ssh://git@git.example.com/team",
		"https:///team/project", "https://git.example.com/team//project",
		"https://git.example.com/../project", "https://git.example.com/team/.git",
		"https://git.example.com/team/project?query", "https://git.example.com/team/pro ject",
		"https://git.example.com/team/project#frag", "https://git.example.com/./project",
		"https://git.example.com/team/project\n",
	} {
		if got, err := Path(url, "/r"); !errors.Is(err, ErrInvalidURL) || got != "" {
			t.Errorf("Path(%q) = %q, %v : rejet attendu", url, got, err)
		}
	}
}

// fixture builds a bare remote reachable at https://git.example.com/team/<name>.git.
func fixture(t *testing.T, name string) (env []string, root string) {
	t.Helper()
	s := t.TempDir()
	cfg := filepath.Join(s, "gitconfig")
	env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+cfg, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v : %v\n%s", args, err, out)
		}
	}
	seed := filepath.Join(s, "seed")
	run("init", "-q", "-b", "main", seed)
	if err := os.WriteFile(filepath.Join(seed, "marker"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("-C", seed, "add", "marker")
	run("-C", seed, "-c", "user.name=Tester", "-c", "user.email=tester@example.com", "commit", "-qm", "fixture")
	bare := filepath.Join(s, "remotes", name+".git")
	run("clone", "-q", "--bare", seed, bare)
	run("config", "--global", "url.file://"+bare+".insteadOf", "https://git.example.com/team/"+name+".git")
	return env, filepath.Join(s, "src")
}

func TestCloneAndReuse(t *testing.T) {
	env, root := fixture(t, "fresh")
	url := "https://git.example.com/team/fresh.git"
	o := Options{Root: root, Env: env, Args: []string{"--quiet", "--no-hardlinks"}}
	target, reused, err := Clone(url, o)
	if err != nil || reused || target != filepath.Join(root, "git.example.com/team/fresh") {
		t.Fatalf("clone : %q, %v, %v", target, reused, err)
	}
	if b, _ := os.ReadFile(filepath.Join(target, "marker")); string(b) != "fixture\n" {
		t.Fatalf("marker = %q", b)
	}
	if again, reused, err := Clone(url, Options{Root: root, Env: env}); err != nil || !reused || again != target {
		t.Fatalf("second run : %q, %v, %v", again, reused, err)
	}
}

func TestCloneBareIsReused(t *testing.T) {
	env, root := fixture(t, "bare")
	url := "https://git.example.com/team/bare.git"
	target, _, err := Clone(url, Options{Root: root, Env: env, Args: []string{"--quiet", "--bare"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, reused, err := Clone(url, Options{Root: root, Env: env}); err != nil || !reused {
		t.Fatalf("bare : %v, %v (%s)", reused, err, target)
	}
}

func TestCloneOccupied(t *testing.T) {
	env, root := fixture(t, "x")
	url := "https://git.example.com/team/plain.git"
	target, _ := Path(url, root)
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "marker"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Clone(url, Options{Root: root, Env: env}); !errors.Is(err, ErrOccupied) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, ".git")); err == nil {
		t.Fatal(".git créé")
	}
}

func TestCloneDifferentOrigin(t *testing.T) {
	env, root := fixture(t, "x")
	url := "https://git.example.com/team/project.git"
	target, _ := Path(url, root)
	for _, args := range [][]string{
		{"init", "-q", target},
		{"-C", target, "remote", "add", "origin", "https://git.example.com/other/project.git"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	if _, _, err := Clone(url, Options{Root: root, Env: env}); !errors.Is(err, ErrOccupied) {
		t.Fatalf("err = %v", err)
	}
}

func TestCloneInvalidURL(t *testing.T) {
	if _, _, err := Clone("nope", Options{Root: t.TempDir()}); !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("err = %v", err)
	}
}
