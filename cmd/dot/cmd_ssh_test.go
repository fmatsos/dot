package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sshEnv gives a profile holding this ssh.conf, PVE_PW=pass:Infra/pve, a fake pass-cli answering
// "fake-value" and a fake ssh that records its arguments and environment in <profile>/seen.
func sshEnv(t *testing.T, conf string) (env *Env, dir string, out, errb *bytes.Buffer) {
	t.Helper()
	env, out, errb = secretsEnv(t, "PVE_PW=pass:Infra/pve\n")
	dir = os.Getenv("DOTFILES_DEPLOY")
	bin := t.TempDir()
	script := "#!/bin/sh\n{ echo \"$@\"; env; } > " + filepath.Join(dir, "seen") + "\necho ssh \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(dir, sshFile), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	return env, dir, out, errb
}

func TestSSHAlias(t *testing.T) {
	env, _, out, errb := sshEnv(t, "# servers\nprod -p 2222 deploy@10.0.0.4\nbox me@box\n")
	if code := execute(env, []string{"ssh", "prod", "uptime"}); code != 0 || out.String() != "ssh -p 2222 deploy@10.0.0.4 uptime\n" {
		t.Fatalf("connection: code %d, stdout %q, stderr %q", code, out, errb)
	}
	out.Reset()
	if code := execute(env, []string{"ssh"}); code != 0 || out.String() != "prod\t-p 2222 deploy@10.0.0.4\nbox\tme@box\n" {
		t.Fatalf("list: code %d, stdout %q", code, out)
	}
	out.Reset()
	if code := execute(env, []string{"ssh", "nope"}); code != 2 || out.Len() != 0 {
		t.Fatalf("unknown: code %d, stdout %q, stderr %q", code, out, errb)
	}
}

func TestSSHSecretsStayOutOfArgsAndEnv(t *testing.T) {
	// Host from a bare vault reference, password asked later by ssh through SSH_ASKPASS.
	env, dir, out, errb := sshEnv(t, "pve root@{pass:Infra/pve#host} password={PVE_PW}\nbad password=x\n")
	if code := execute(env, []string{"ssh", "pve"}); code != 0 {
		t.Fatalf("code %d, stderr %q", code, errb)
	}
	if !strings.HasPrefix(out.String(), "ssh -o PreferredAuthentications=keyboard-interactive,password -o NumberOfPasswordPrompts=1 root@fake-value\n") {
		t.Fatalf("unexpected arguments: %q", out)
	}
	seen, _ := os.ReadFile(filepath.Join(dir, "seen"))
	for _, want := range []string{"SSH_ASKPASS_REQUIRE=force", "DOT_SSH_ASKPASS=PVE_PW", "DOTFILES_DEPLOY=" + dir} {
		if !strings.Contains(string(seen), want) {
			t.Errorf("ssh environment without %q", want)
		}
	}
	// The password is "fake-value" too: it may only appear as the resolved host, once, in the arguments.
	if strings.Count(string(seen), "fake-value") != 1 {
		t.Errorf("a value leaks into the environment or the arguments:\n%s", seen)
	}
	// An invalid password= is a usage error, and ssh is not even launched (it would rewrite seen).
	os.Remove(filepath.Join(dir, "seen"))
	if code := execute(env, []string{"ssh", "bad"}); code != 2 {
		t.Errorf("invalid password=: code %d, stderr %q", code, errb)
	}
	if _, err := os.Stat(filepath.Join(dir, "seen")); err == nil {
		t.Error("ssh launched despite an invalid password=")
	}
}

func TestSSHAskpass(t *testing.T) {
	env, _, out, errb := sshEnv(t, "")
	t.Setenv("DOT_SSH_ASKPASS", "PVE_PW")
	if code := execute(env, []string{"root@pve's password: "}); code != 0 || out.String() != "fake-value\n" {
		t.Fatalf("password: code %d, stdout %q, stderr %q", code, out, errb)
	}
	out.Reset()
	old := promptVisible
	promptVisible = func(string) (string, error) { return "yes", nil }
	t.Cleanup(func() { promptVisible = old })
	if code := execute(env, []string{"Are you sure you want to continue connecting (yes/no)? "}); code != 0 || out.String() != "yes\n" {
		t.Fatalf("non-password question: code %d, stdout %q", code, out)
	}
}

func TestSSHKeyGoesToAnEphemeralAgent(t *testing.T) {
	env, dir, out, errb := sshEnv(t, "k root@h key={pass:Infra/pve}\nsp {pass:Infra/pve#User name}@h\n")
	bin := t.TempDir()
	// pass-cli records its arguments, ssh-agent only creates its socket, ssh-add records its stdin.
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("pass-cli", "echo \"$@\" >> "+filepath.Join(dir, "pass-args")+"\nprintf 'fake-value\\n'\n")
	write("ssh-agent", "touch \"$3\"\nexec sleep 30\n")
	write("ssh-add", "cat > "+filepath.Join(dir, "added")+"\n")
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	run := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", run)

	if code := execute(env, []string{"ssh", "k"}); code != 0 || out.String() != "ssh root@h\n" {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errb)
	}
	if added, _ := os.ReadFile(filepath.Join(dir, "added")); string(added) != "fake-value\n" {
		t.Errorf("ssh-add received %q", added)
	}
	if args, _ := os.ReadFile(filepath.Join(dir, "pass-args")); !strings.Contains(string(args), "--field private_key") {
		t.Errorf("pass-cli without --field private_key: %q", args)
	}
	seen, _ := os.ReadFile(filepath.Join(dir, "seen"))
	if !strings.Contains(string(seen), "SSH_AUTH_SOCK="+run+"/dot-ssh-") || strings.Contains(string(seen), "fake-value") {
		t.Errorf("ssh environment or arguments:\n%s", seen)
	}
	if left, _ := os.ReadDir(run); len(left) != 0 {
		t.Errorf("the agent left %d entry(ies) in %s", len(left), run)
	}

	// A {…} holding spaces stays one argument.
	out.Reset()
	if code := execute(env, []string{"ssh", "sp"}); code != 0 || out.String() != "ssh fake-value@h\n" {
		t.Fatalf("field with spaces: code %d, stdout %q, stderr %q", code, out, errb)
	}
	if args, _ := os.ReadFile(filepath.Join(dir, "pass-args")); !strings.Contains(string(args), "--field User name") {
		t.Errorf("field with spaces not passed: %q", args)
	}
}
