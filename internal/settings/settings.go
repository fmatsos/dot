// Package settings merges the versioned Claude Code settings base(s) into ~/.claude/settings.json.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Options drives Run. Bases are settings.base.json paths in merge order: the last one wins.
type Options struct {
	Home        string
	Bases       []string
	SkipMissing bool // multi-profile: a profile without a base file is ignored
	DryRun      bool
	Out         io.Writer
	Now         func() time.Time // defaults to time.Now
}

// Merge returns dst with src merged in recursively: objects merge, any other src value
// replaces (the semantics of jq's `dst * src`). Neither argument is modified.
func Merge(dst, src map[string]any) map[string]any {
	out := make(map[string]any, len(dst)+len(src))
	for k, v := range dst {
		out[k] = v
	}
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := out[k].(map[string]any); ok {
				out[k] = Merge(dm, sm)
				continue
			}
		}
		out[k] = v
	}
	return out
}

// Parse decodes exactly one JSON object, keeping numbers as written.
func Parse(data []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("JSON invalide")
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("objet JSON attendu")
	}
	return m, nil
}

// Normalize prints sorted keys, two-space indentation and a final newline (jq -S .).
func Normalize(m map[string]any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func readObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Run merges the bases into ~/.claude/settings.json: the base wins, extra local keys stay.
// It prints the diff (never context lines: unchanged local credentials must not appear),
// then, unless DryRun, backs up the old file and replaces it atomically.
func Run(o Options) error {
	now := o.Now
	if now == nil {
		now = time.Now
	}
	tilde := func(p string) string {
		if rest, ok := strings.CutPrefix(p, o.Home+"/"); ok {
			return "~/" + rest
		}
		return p
	}
	base := map[string]any{}
	found := 0
	for _, p := range o.Bases {
		m, err := readObject(p)
		if errors.Is(err, os.ErrNotExist) && o.SkipMissing {
			continue
		}
		if err != nil {
			return fmt.Errorf("%s illisible ou invalide (%v)", tilde(p), cleanErr(err))
		}
		base = Merge(base, m)
		found++
	}
	if found == 0 {
		return errors.New("aucun settings.base.json trouvé")
	}
	target := filepath.Join(o.Home, ".claude", "settings.json")
	oldText, newText := "{}\n", ""
	exists := false
	if st, err := os.Stat(target); err == nil && st.Mode().IsRegular() {
		exists = true
		cur, err := readObject(target)
		if err != nil {
			return fmt.Errorf("%s illisible ou invalide (%v)", tilde(target), cleanErr(err))
		}
		if oldText, err = Normalize(cur); err != nil {
			return err
		}
		newText, err = Normalize(Merge(cur, base))
		if err != nil {
			return err
		}
	} else {
		var err error
		if newText, err = Normalize(base); err != nil {
			return err
		}
	}
	if exists && oldText == newText {
		fmt.Fprintln(o.Out, "settings: à jour")
		return nil
	}
	io.WriteString(o.Out, Diff("settings.json", "settings.json+base", oldText, newText))
	if o.DryRun {
		return nil
	}
	if exists {
		dir, err := Backup(o.Home, ".claude/settings.json", now())
		if err != nil {
			return err
		}
		fmt.Fprintf(o.Out, "settings : sauvegarde → %s/.claude/settings.json\n", tilde(dir))
	}
	if err := writeAtomic(target, []byte(newText)); err != nil {
		return err
	}
	fmt.Fprintln(o.Out, "settings : mis à jour")
	return nil
}

func cleanErr(err error) string {
	var se *json.SyntaxError
	if errors.As(err, &se) {
		return "JSON invalide"
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

// Backup copies Home/<rel> to ~/.local/state/dotfiles/backup/<AAAAMMJJ-HHMMSS>-<random>/<rel>
// (private directories, mode and mtime kept like cp -p) and returns the backup directory.
func Backup(home, rel string, now time.Time) (string, error) {
	parent := filepath.Join(home, ".local", "state", "dotfiles", "backup")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(parent, now.Format("20060102-150405")+"-*")
	if err != nil {
		return "", err
	}
	src := filepath.Join(home, rel)
	dst := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return "", err
	}
	st, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(dst, st.Mode().Perm()); err != nil {
		return "", err
	}
	return dir, os.Chtimes(dst, st.ModTime(), st.ModTime())
}

// writeAtomic replaces path with a private (0600) file, creating its directory (0700) when needed.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".settings.json.*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
