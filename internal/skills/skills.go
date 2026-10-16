// Package skills renders the agent skills the profiles version into the machine through the
// skills CLI (npx skills): the merged list is authoritative for every skill installed from a
// source; a skill without a source (copied by hand) is never touched.
package skills

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// CLI is the pinned skills package npx runs: raise it on purpose, never float on latest.
const CLI = "skills@1.7.2"

// Options drives Run.
type Options struct {
	Home string
	// Sources are the profiles' skills.json files in merge order; a missing file is skipped.
	Sources  []string
	DryRun   bool
	Out      io.Writer
	LookPath func(string) (string, error)                      // defaults to exec.LookPath
	Exec     func(name string, args ...string) ([]byte, error) // stdout only; defaults to exec.Command
}

// List maps a source (as npx skills add takes it, with an optional #ref) to its skill names.
type List map[string][]string

// Installed is one entry of `skills list -g --json`; Source is empty for a skill without one.
type Installed struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

// Step is one change of the plan.
type Step struct {
	Action string // add, remove or unmanaged (reported only)
	Name   string
	Source string // add: the source key to install from
}

// Local is the machine's own list, applied over the profiles: a source set to null is dropped.
func Local(home string) string {
	return filepath.Join(home, ".config", "skills", "skills.local.json")
}

func (o *Options) defaults() {
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	if o.Exec == nil {
		o.Exec = func(name string, args ...string) ([]byte, error) { return exec.Command(name, args...).Output() }
	}
}

// Npx runs `npx -y skills@<pin> args…` and returns its stdout.
func (o Options) Npx(args ...string) ([]byte, error) {
	o.defaults()
	return o.Exec("npx", append([]string{"-y", CLI}, args...)...)
}

// Parse reads a skills.json or skills.local.json; null values are kept as nil lists (removals).
func Parse(data []byte) (List, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return nil, errors.New("not a JSON object")
	}
	l := List{}
	for src, v := range raw {
		if err := checkSource(src); err != nil {
			return nil, err
		}
		if string(bytes.TrimSpace(v)) == "null" {
			l[src] = nil
			continue
		}
		var names []string
		if err := json.Unmarshal(v, &names); err != nil || len(names) == 0 {
			return nil, fmt.Errorf("%s: a non-empty list of skill names expected", src)
		}
		for _, n := range names {
			if n == "" || n == "*" || strings.HasPrefix(n, "-") {
				return nil, fmt.Errorf("%s: invalid skill name %q", src, n)
			}
		}
		l[src] = names
	}
	return l, nil
}

var shorthandRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// checkSource refuses the source forms whose lock entry would never match the key (a sub path, an
// @skill suffix, a tree URL, a gitlab: prefix): the skill would be added again on every run.
func checkSource(src string) error {
	repo, _, _ := strings.Cut(src, "#")
	if gh, ok := strings.CutPrefix(strings.TrimSuffix(repo, ".git"), "https://github.com/"); ok {
		repo = gh
	} else if strings.Contains(repo, "://") || strings.HasPrefix(repo, "git@") {
		return nil // another host: the lock keeps the URL as given
	}
	if !shorthandRe.MatchString(strings.TrimPrefix(repo, "github:")) {
		return fmt.Errorf("invalid source %q: owner/repo[#ref] or a repository URL expected", src)
	}
	return nil
}

func readList(path string) (List, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	l, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %v", filepath.Base(path), err)
	}
	return l, nil
}

// Merge unions the profiles' lists source by source, then applies the local list: a local source
// replaces the profiles' one (the same repository, whatever its ref), null drops it. It returns
// the wanted skill name → source key.
func Merge(profiles []List, local List) (map[string]string, error) {
	merged := List{}
	for _, l := range profiles {
		for src, names := range l {
			if names == nil {
				continue // null only means something in the local list
			}
			for _, n := range names {
				if !slices.Contains(merged[src], n) {
					merged[src] = append(merged[src], n)
				}
			}
		}
	}
	for src, names := range local {
		for k := range merged {
			if SameSource(k, src) {
				delete(merged, k) // whatever ref or spelling the profiles used
			}
		}
		if names != nil {
			merged[src] = names
		}
	}
	want := map[string]string{}
	for _, src := range slices.Sorted(maps.Keys(merged)) {
		for _, n := range merged[src] {
			if prev, ok := want[n]; ok && prev != src {
				return nil, fmt.Errorf("skill %s declared by two sources (%s, %s)", n, prev, src)
			}
			want[n] = src
		}
	}
	return want, nil
}

// SameSource tells whether a list key and a lock source name the same repository: the lock keeps
// owner/repo for GitHub, the list may say it as a URL, with a #ref the lock stores apart.
func SameSource(key, locked string) bool {
	norm := func(s string) string {
		s, _, _ = strings.Cut(s, "#")
		s = strings.TrimPrefix(strings.TrimPrefix(s, "https://github.com/"), "github:")
		return strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git")
	}
	return strings.EqualFold(norm(key), norm(locked))
}

// Plan compares the wanted skills with the installed ones. A skill whose source changed is added
// again from the new one (add overwrites). A ref change is not seen: dot ai skills update.
func Plan(want map[string]string, installed []Installed) []Step {
	var steps []Step
	have := map[string]Installed{}
	for _, s := range installed {
		have[s.Name] = s
	}
	for _, n := range slices.Sorted(maps.Keys(want)) {
		if s, ok := have[n]; !ok || !SameSource(want[n], s.Source) {
			steps = append(steps, Step{"add", n, want[n]})
		}
	}
	for _, n := range slices.Sorted(maps.Keys(have)) {
		switch {
		case have[n].Source == "":
			steps = append(steps, Step{"unmanaged", n, ""})
		case want[n] == "":
			steps = append(steps, Step{"remove", n, ""})
		}
	}
	return steps
}

// State reads the global skills through the CLI.
func (o Options) State() ([]Installed, error) {
	data, err := o.Npx("list", "-g", "--json")
	if err != nil {
		return nil, errors.New("cannot list the installed skills")
	}
	var out []Installed
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, errors.New("unreadable skills list")
	}
	return out, nil
}

// Run validates every list, reads the installed skills, prints the plan ("skills: <action> <name>"
// or "skills: up to date"), then applies it unless DryRun. Nothing is called before the lists are valid.
func Run(o Options) error {
	o.defaults()
	var lists []List
	read := false
	for _, p := range o.Sources {
		l, err := readList(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		lists, read = append(lists, l), true
	}
	local, err := readList(Local(o.Home))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// No list at all is no wish: it would remove every skill. An explicit {} still does.
	if !read && local == nil {
		return errors.New("no skills.json in the profiles: run dot ai skills adopt first")
	}
	want, err := Merge(lists, local)
	if err != nil {
		return err
	}
	if _, err := o.LookPath("npx"); err != nil {
		return errors.New("npx not found: install Node (the nvm key of dot.json)")
	}
	installed, err := o.State()
	if err != nil {
		return err
	}
	steps := Plan(want, installed)
	changed := false
	for _, s := range steps {
		if s.Action == "add" {
			fmt.Fprintf(o.Out, "skills: add %s (%s)\n", s.Name, s.Source)
		} else {
			fmt.Fprintf(o.Out, "skills: %s %s\n", s.Action, s.Name)
		}
		changed = changed || s.Action != "unmanaged"
	}
	if !changed {
		fmt.Fprintln(o.Out, "skills: up to date")
	}
	if o.DryRun {
		return nil
	}
	return o.apply(steps)
}

// apply removes first (a skill may move between sources), then adds source by source.
func (o Options) apply(steps []Step) error {
	var gone []string
	adds := map[string][]string{}
	for _, s := range steps {
		switch s.Action {
		case "remove":
			gone = append(gone, s.Name)
		case "add":
			adds[s.Source] = append(adds[s.Source], s.Name)
		}
	}
	if len(gone) > 0 {
		if _, err := o.Npx(append(append([]string{"remove"}, gone...), "-g", "-y")...); err != nil {
			return fmt.Errorf("cannot remove (%s)", strings.Join(gone, ", "))
		}
	}
	for _, src := range slices.Sorted(maps.Keys(adds)) {
		// -s takes every value up to the next flag: it comes last.
		if _, err := o.Npx(append([]string{"add", src, "-g", "-y", "-s"}, adds[src]...)...); err != nil {
			return fmt.Errorf("cannot add (%s from %s)", strings.Join(adds[src], ", "), src)
		}
	}
	return nil
}

// ReadList reads a profile's list; a missing file is an empty list.
func ReadList(path string) (List, error) {
	l, err := readList(path)
	if errors.Is(err, os.ErrNotExist) {
		return List{}, nil
	}
	return l, err
}

// Encode renders a list as written in the profile: sorted sources, a trailing newline.
func Encode(l List) []byte {
	data, _ := json.MarshalIndent(l, "", "  ")
	return append(data, '\n')
}

// WriteList replaces a profile's list, creating its directory when needed.
func WriteList(path string, l List) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, Encode(l), 0o644)
}

// AddTo adds names to a source of the list, under the key the list already uses for that
// repository if any, without duplicates.
func AddTo(l List, src string, names ...string) {
	for k := range l {
		if SameSource(k, src) {
			src = k
		}
	}
	for _, n := range names {
		if !slices.Contains(l[src], n) {
			l[src] = append(l[src], n)
		}
	}
}

// RemoveFrom drops names from every source of the list, and a source left empty; it returns
// the names the list did not hold.
func RemoveFrom(l List, names ...string) (missing []string) {
	for _, n := range names {
		found := false
		for src, have := range l {
			if i := slices.Index(have, n); i >= 0 {
				found = true
				if l[src] = slices.Delete(have, i, i+1); len(l[src]) == 0 {
					delete(l, src)
				}
			}
		}
		if !found {
			missing = append(missing, n)
		}
	}
	return missing
}

// Adopt adds every installed skill that has a source to the list.
func Adopt(l List, installed []Installed) {
	for _, s := range installed {
		if s.Source != "" {
			AddTo(l, s.Source, s.Name)
		}
	}
}
