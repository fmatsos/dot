package link

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, p, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
}

func TestListBackupsOrderAndSymlinks(t *testing.T) {
	home := t.TempDir()
	base := BackupBase(home)
	writeFile(t, filepath.Join(base, "20260101-000000", ".a"), "x", 0o644)
	writeFile(t, filepath.Join(base, "20260301-000000", ".config", "b"), "x", 0o644)
	writeFile(t, filepath.Join(base, "20260301-000000", ".z"), "x", 0o644)
	if err := os.Symlink(filepath.Join(base, "20260101-000000"), filepath.Join(base, "20260901-000000")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(base, "notarun", "f"), "x", 0o644)
	runs, err := ListBackups(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].ID != "20260301-000000" || runs[1].ID != "20260101-000000" {
		t.Fatalf("runs = %+v", runs)
	}
	if got := strings.Join(runs[0].Files, ","); got != ".config/b,.z" {
		t.Fatalf("files = %s", got)
	}
	if r, err := ListBackups(t.TempDir()); err != nil || len(r) != 0 {
		t.Fatalf("no base: %v %v", r, err)
	}
}

func TestRunDirRejectsTraversal(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(BackupBase(home), "20260101-000000", "f"), "x", 0o644)
	for _, id := range []string{"", ".", "..", "../backup", "20260101-000000/..", "/etc", "a/b"} {
		if _, err := RunFiles(home, id); err == nil {
			t.Errorf("id %q accepted", id)
		}
	}
}

func restoreReq(home string, paths ...string) (RestoreRequest, *bytes.Buffer, *bytes.Buffer) {
	var o, e bytes.Buffer
	return RestoreRequest{Home: home, Run: "20260101-000000", Paths: paths, Out: &o, Err: &e}, &o, &e
}

func TestRestoreAbsentAndManagedLink(t *testing.T) {
	home := t.TempDir()
	clone := filepath.Join(home, ".dot", "p")
	run := filepath.Join(BackupBase(home), "20260101-000000")
	writeFile(t, filepath.Join(run, ".absent"), "A", 0o600)
	writeFile(t, filepath.Join(run, ".config", "app", "linked"), "L", 0o755)
	writeFile(t, filepath.Join(clone, "home", ".config", "app", "linked"), "profile", 0o644)
	if err := os.MkdirAll(filepath.Join(home, ".config", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(clone, "home", ".config", "app", "linked"), filepath.Join(home, ".config", "app", "linked")); err != nil {
		t.Fatal(err)
	}
	req, out, errb := restoreReq(home)
	req.Clones = []string{clone}
	n, refused, err := Restore(req)
	if err != nil || n != 2 || refused != 0 || errb.Len() != 0 {
		t.Fatalf("n=%d refused=%d err=%v stderr=%q", n, refused, err, errb)
	}
	b, _ := os.ReadFile(filepath.Join(home, ".config", "app", "linked"))
	fi, _ := os.Lstat(filepath.Join(home, ".config", "app", "linked"))
	if string(b) != "L" || fi.Mode().Type() != 0 || fi.Mode().Perm() != 0o755 {
		t.Fatalf("linked not restored: %q %v", b, fi.Mode())
	}
	if fi, _ := os.Lstat(filepath.Join(home, ".absent")); fi == nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode lost: %v", fi)
	}
	if _, err := os.Stat(filepath.Join(clone, "home", ".config", "app", "linked")); err != nil {
		t.Fatal("profile file touched")
	}
	if _, err := os.Lstat(run); !os.IsNotExist(err) {
		t.Fatalf("run dir kept: %v", err)
	}
	if !strings.Contains(out.String(), "restauré : ~/.absent") {
		t.Fatalf("out = %q", out)
	}
}

func TestRestoreRefusesUserFilesAndContinues(t *testing.T) {
	home := t.TempDir()
	run := filepath.Join(BackupBase(home), "20260101-000000")
	writeFile(t, filepath.Join(run, ".real"), "old", 0o644)
	writeFile(t, filepath.Join(run, ".foreign"), "old", 0o644)
	writeFile(t, filepath.Join(run, ".ok"), "old", 0o644)
	writeFile(t, filepath.Join(home, ".real"), "user", 0o644)
	if err := os.Symlink("/etc/hostname", filepath.Join(home, ".foreign")); err != nil {
		t.Fatal(err)
	}
	req, _, errb := restoreReq(home)
	n, refused, err := Restore(req)
	if err != nil || n != 1 || refused != 2 {
		t.Fatalf("n=%d refused=%d err=%v", n, refused, err)
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".real")); string(b) != "user" {
		t.Fatal("user file overwritten")
	}
	if _, err := os.Stat(filepath.Join(run, ".real")); err != nil {
		t.Fatal("refused backup lost")
	}
	if _, err := os.Lstat(run); err != nil {
		t.Fatal("run dir removed while not empty")
	}
	if !strings.Contains(errb.String(), "refusé") {
		t.Fatalf("stderr = %q", errb)
	}
}

func TestRestoreRefusesBelowLinkedDir(t *testing.T) {
	home := t.TempDir()
	clone := filepath.Join(home, ".dot", "p")
	run := filepath.Join(BackupBase(home), "20260101-000000")
	writeFile(t, filepath.Join(run, ".config", "f"), "old", 0o644)
	writeFile(t, filepath.Join(clone, "home", ".config", "keep"), "k", 0o644)
	if err := os.Symlink(filepath.Join(clone, "home", ".config"), filepath.Join(home, ".config")); err != nil {
		t.Fatal(err)
	}
	req, _, _ := restoreReq(home)
	req.Clones = []string{clone}
	n, refused, _ := Restore(req)
	if n != 0 || refused != 1 {
		t.Fatalf("n=%d refused=%d", n, refused)
	}
	if _, err := os.Lstat(filepath.Join(clone, "home", ".config", "f")); err == nil {
		t.Fatal("wrote through a linked directory into the profile")
	}
}

func TestRestoreDryAndPaths(t *testing.T) {
	home := t.TempDir()
	run := filepath.Join(BackupBase(home), "20260101-000000")
	writeFile(t, filepath.Join(run, ".a"), "a", 0o644)
	writeFile(t, filepath.Join(run, ".d", "x"), "x", 0o644)
	writeFile(t, filepath.Join(run, ".d", "y"), "y", 0o644)
	req, out, _ := restoreReq(home, "~/.d")
	req.Dry = true
	if n, refused, err := Restore(req); err != nil || n != 2 || refused != 0 || !strings.Contains(out.String(), "[dry]") {
		t.Fatalf("dry: %d %d %v %q", n, refused, err, out)
	}
	if _, err := os.Lstat(filepath.Join(home, ".d")); err == nil {
		t.Fatal("dry run wrote")
	}
	req, _, _ = restoreReq(home, ".d/x")
	if n, _, err := Restore(req); err != nil || n != 1 {
		t.Fatalf("path restore: %d %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(run, ".d", "y")); err != nil {
		t.Fatal("unselected file moved")
	}
	req, _, errb := restoreReq(home, ".nope")
	if _, refused, _ := Restore(req); refused != 1 || !strings.Contains(errb.String(), "absent de la sauvegarde") {
		t.Fatalf("missing path: %q", errb)
	}
}

func TestRestoreRejectsTraversalPaths(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(BackupBase(home), "20260101-000000", ".a"), "a", 0o644)
	for _, p := range []string{"../x", ".d/../../x", "~/../x", "/etc/passwd", "~", "."} {
		req, _, _ := restoreReq(home, p)
		if _, _, err := Restore(req); err == nil {
			t.Errorf("path %q accepted", p)
		}
	}
}
