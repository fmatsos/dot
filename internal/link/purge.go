package link

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

// DefaultBackupDays is the retention when DOTFILES_BACKUP_DAYS is unset or empty.
const DefaultBackupDays = 30

// ErrBackupDays is the invalid DOTFILES_BACKUP_DAYS warning (printed on stderr, purge skipped).
var ErrBackupDays = errors.New("sauvegardes : DOTFILES_BACKUP_DAYS invalide, purge ignorée")

var (
	digitsRe = regexp.MustCompile(`^[0-9]+$`)
	stampRe  = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}`)
)

// ParseBackupDays reads DOTFILES_BACKUP_DAYS: empty gives the default, non-negative integers
// only (0 keeps everything). ponytail: a value beyond int64 counts as invalid, not "too old".
func ParseBackupDays(s string) (int, error) {
	if s == "" {
		return DefaultBackupDays, nil
	}
	n, err := strconv.Atoi(s)
	if !digitsRe.MatchString(s) || err != nil {
		return 0, ErrBackupDays
	}
	return n, nil
}

// PurgeBackupsFrom parses raw (the env value) then purges; an invalid value warns on errOut.
func PurgeBackupsFrom(home, raw string, dry bool, out, errOut io.Writer, now func() time.Time) error {
	days, err := ParseBackupDays(raw)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return nil
	}
	return PurgeBackups(home, days, dry, out, errOut, now)
}

// PurgeBackups removes the backup directories older than days, judged by the YYYYMMDD-HHMMSS
// prefix of their name (never mtime). Only real direct subdirectories whose stamp is a valid,
// round-tripping date are considered; symlinks, files and a symlinked base are left alone.
// days == 0 keeps everything; dry only lists.
func PurgeBackups(home string, days int, dry bool, out, errOut io.Writer, now func() time.Time) error {
	if days <= 0 {
		return nil
	}
	base := BackupBase(home)
	if i, err := os.Lstat(base); err != nil || !i.IsDir() {
		return nil
	}
	n := now()
	cutoff := n.AddDate(0, 0, -days)
	if cutoff.Year() < 1 || cutoff.After(n) { // calendar overflow
		fmt.Fprintln(errOut, "sauvegardes : date limite indisponible, purge ignorée")
		return nil
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return err
	}
	count := 0
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !stampRe.MatchString(name) { // e.IsDir is false for symlinks
			continue
		}
		when, err := time.ParseInLocation(stampLayout, name[:15], n.Location())
		if err != nil || when.Format(stampLayout) != name[:15] || !when.Before(cutoff) {
			continue
		}
		p := filepath.Join(base, name)
		if dry {
			fmt.Fprintf(out, "  [dry] purge : %s\n", Tilde(home, p))
		} else if err := os.RemoveAll(p); err != nil {
			return err
		}
		count++
	}
	if count > 0 && !dry {
		fmt.Fprintf(out, "sauvegardes : %d dossier(s) purgé(s)\n", count)
	}
	return nil
}
