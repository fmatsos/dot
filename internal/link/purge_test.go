package link

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseBackupDays(t *testing.T) {
	tests := []struct {
		in   string
		want int
		bad  bool
	}{
		{"", 30, false}, {"0", 0, false}, {"00", 0, false}, {"5", 5, false}, {"365", 365, false},
		{"-1", 0, true}, {"abc", 0, true}, {"1.5", 0, true}, {" 3", 0, true}, {"99999999999999999999", 0, true},
	}
	for _, tc := range tests {
		got, err := ParseBackupDays(tc.in)
		if (err != nil) != tc.bad || got != tc.want {
			t.Errorf("%q = %d, %v", tc.in, got, err)
		}
	}
}

// purgeFixture mirrors tests/backups.sh: valid old dirs, kept dirs, and traps.
func purgeFixture(t *testing.T) (home, base, outside string) {
	t.Helper()
	root := t.TempDir()
	home = filepath.Join(root, "home")
	base = BackupBase(home)
	outside = filepath.Join(root, "outside")
	write(t, outside+"/payload", "keep\n", 0o644)
	for _, d := range []string{
		"20000101-010203", "20000102-010203-extra", "20261006-120000",
		"unrelated/19990101-000000", "20001399-250099", "20000231-010203",
	} {
		must(t, os.MkdirAll(filepath.Join(base, d), 0o755))
	}
	write(t, base+"/20000103-010203", "file\n", 0o644)
	must(t, os.Symlink(outside, base+"/20000104-010203"))
	must(t, os.Symlink(outside, base+"/20000101-010203/link"))
	return
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func TestPurgeBackups(t *testing.T) {
	tests := []struct {
		name      string
		days      int
		dry       bool
		extra     string // extra directory created in base, with whether it should survive
		extraKept bool
		oldKept   bool
		out, err  string
	}{
		{name: "default retention purges old valid names only", days: 30, out: "sauvegardes : 2 dossier(s) purgé(s)\n"},
		{name: "zero keeps everything", days: 0, oldKept: true},
		{name: "dry run lists and deletes nothing", days: 30, dry: true, oldKept: true,
			out: "  [dry] purge : ~/.local/state/dotfiles/backup/20000101-010203\n" +
				"  [dry] purge : ~/.local/state/dotfiles/backup/20000102-010203-extra\n"},
		{name: "10 days old kept at 30", days: 30, extra: "20260926-120000", extraKept: true, out: "sauvegardes : 2 dossier(s) purgé(s)\n"},
		{name: "10 days old purged at 5", days: 5, extra: "20260926-120000", out: "sauvegardes : 3 dossier(s) purgé(s)\n"},
		{name: "exactly at cutoff is kept", days: 5, extra: "20261001-120000", extraKept: true, out: "sauvegardes : 2 dossier(s) purgé(s)\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home, base, outside := purgeFixture(t)
			if tc.extra != "" {
				must(t, os.MkdirAll(filepath.Join(base, tc.extra), 0o755))
			}
			var out, errOut bytes.Buffer
			must(t, PurgeBackups(home, tc.days, tc.dry, &out, &errOut, clock))
			if out.String() != tc.out || errOut.String() != tc.err {
				t.Errorf("out=%q err=%q", out.String(), errOut.String())
			}
			for _, d := range []string{"20000101-010203", "20000102-010203-extra"} {
				if exists(filepath.Join(base, d)) != tc.oldKept {
					t.Errorf("%s kept = %v", d, !tc.oldKept)
				}
			}
			if tc.extra != "" && exists(filepath.Join(base, tc.extra)) != tc.extraKept {
				t.Errorf("%s kept = %v", tc.extra, !tc.extraKept)
			}
			for _, d := range []string{
				"20261006-120000", "unrelated/19990101-000000", "20001399-250099", "20000231-010203",
				"20000103-010203", "20000104-010203", // file and symlink
			} {
				if !exists(filepath.Join(base, d)) {
					t.Errorf("%s removed", d)
				}
			}
			if !exists(outside + "/payload") {
				t.Error("symlink target followed and deleted")
			}
		})
	}
}

func TestPurgeBackupsFromInvalid(t *testing.T) {
	for _, raw := range []string{"-1", "abc", "1.5"} {
		home, base, _ := purgeFixture(t)
		var out, errOut bytes.Buffer
		must(t, PurgeBackupsFrom(home, raw, false, &out, &errOut, clock))
		if errOut.String() != "sauvegardes : DOTFILES_BACKUP_DAYS invalide, purge ignorée\n" || out.Len() != 0 {
			t.Errorf("%s: out=%q err=%q", raw, out.String(), errOut.String())
		}
		if !exists(base + "/20000101-010203") {
			t.Errorf("%s: purged anyway", raw)
		}
	}
	// empty means the default 30 days
	home, base, _ := purgeFixture(t)
	var out bytes.Buffer
	must(t, PurgeBackupsFrom(home, "", false, &out, &out, clock))
	if exists(base+"/20000101-010203") || !strings.Contains(out.String(), "2 dossier(s)") {
		t.Errorf("default not applied: %q", out.String())
	}
}

func TestPurgeBackupsSymlinkedBase(t *testing.T) {
	home, base, _ := purgeFixture(t)
	target := filepath.Join(filepath.Dir(home), "backup-target")
	must(t, os.Rename(base, target))
	must(t, os.Symlink(target, base))
	var out bytes.Buffer
	must(t, PurgeBackups(home, 30, false, &out, &out, clock))
	if !exists(target+"/20000101-010203") || out.Len() != 0 {
		t.Errorf("symlinked base followed: %q", out.String())
	}
}

func TestPurgeBackupsEdges(t *testing.T) {
	// missing base: nothing, no error
	var out bytes.Buffer
	must(t, PurgeBackups(t.TempDir(), 30, false, &out, &out, clock))
	if out.Len() != 0 {
		t.Errorf("out = %q", out.String())
	}
	// absurd retention: cutoff unavailable, purge skipped with a warning
	home, base, _ := purgeFixture(t)
	var errOut bytes.Buffer
	must(t, PurgeBackups(home, 9_000_000, false, &out, &errOut, clock))
	if errOut.String() != "sauvegardes : date limite indisponible, purge ignorée\n" || !exists(base+"/20000101-010203") {
		t.Errorf("err = %q", errOut.String())
	}
	// the stamp is read in the clock's zone, not UTC
	zone := time.FixedZone("x", 2*3600)
	home, base, _ = purgeFixture(t)
	must(t, os.MkdirAll(base+"/20261005-150000", 0o755))
	must(t, PurgeBackups(home, 1, false, &out, &errOut, func() time.Time { return time.Date(2026, 10, 6, 15, 0, 1, 0, zone) }))
	if exists(base + "/20261005-150000") {
		t.Error("stamp 1 day + 1s old not purged")
	}
}
