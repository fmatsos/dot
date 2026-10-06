package link

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
)

// A profile holds home/ and, beside it, home@<os>/ and home@<host>/ variants. For one path
// relative to ~, home@<host> beats home@<os> beats home/; variants of other systems are ignored.

// Seams of the machine identity, replaced in tests. HostFn is the hostname, "" when unknown;
// $DOT_HOSTNAME replaces the real one (for tests only).
var (
	GOOS   = runtime.GOOS
	HostFn = func() string {
		h := os.Getenv("DOT_HOSTNAME")
		if h == "" {
			h, _ = os.Hostname()
		}
		return h
	}
)

var (
	hostRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	knownOS = []string{"darwin", "linux"}
)

// OSLayer is the directory name of the variant of this operating system.
func OSLayer() string { return "home@" + GOOS }

// HostLayer is the directory name of the variant of this machine; false when the hostname is
// unknown, invalid or named like an OS (home@linux would then be ambiguous).
func HostLayer() (string, bool) {
	h, _, _ := strings.Cut(HostFn(), ".")
	h = strings.ToLower(h)
	if !hostRe.MatchString(h) || slices.Contains(knownOS, h) {
		return "", false
	}
	return "home@" + h, true
}

// activeLayers lists the layer directory names that apply here, lowest precedence first.
func activeLayers() []string {
	l := []string{"home", OSLayer()}
	if h, ok := HostLayer(); ok {
		l = append(l, h)
	}
	return l
}

// allLayers lists every home layer present in dir, active or not, for uninstall.
func allLayers(dir string) []string {
	l := []string{"home"}
	if m, err := filepath.Glob(filepath.Join(dir, "home@*")); err == nil {
		for _, p := range m {
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				l = append(l, filepath.Base(p))
			}
		}
	}
	return l
}

// layerLinks merges the given layers (lowest precedence first) into one link per path of ~.
// multi leaves out the ModuleSources. The result is sorted by Dst.
func layerLinks(dir, home string, layers []string, multi bool) ([]Link, error) {
	byRel := map[string]Link{}
	rank := map[string]int{} // layer index of each entry, higher wins
	var firstErr error
	for i, layer := range layers {
		root := filepath.Join(dir, layer)
		fl, err := files(root, func(fs.DirEntry) bool { return true })
		if err != nil && firstErr == nil {
			firstErr = err
		}
		for _, f := range fl {
			rel, _ := filepath.Rel(root, f)
			if multi && slices.Contains(ModuleSources, filepath.ToSlash(rel)) {
				continue
			}
			byRel[rel], rank[rel] = Link{Src: f, Dst: filepath.Join(home, rel), Layer: layer}, i
		}
	}
	// A file in one layer and a path below it in another cannot both exist in ~: the higher
	// layer wins, as for identical paths.
	for rel := range byRel {
		for a := filepath.Dir(rel); a != "." && a != string(filepath.Separator); a = filepath.Dir(a) {
			if _, ok := byRel[a]; !ok {
				continue
			}
			if rank[a] > rank[rel] {
				delete(byRel, rel)
			} else {
				delete(byRel, a)
			}
			break
		}
	}
	links := make([]Link, 0, len(byRel))
	for _, k := range byRel {
		links = append(links, k)
	}
	slices.SortFunc(links, func(x, y Link) int { return strings.Compare(x.Dst, y.Dst) })
	return links, firstErr
}

// IsHomeLayer tells whether name is a home layer directory (home or home@<name>).
func IsHomeLayer(name string) bool {
	return name == "home" || (strings.HasPrefix(name, "home@") && len(name) > len("home@"))
}

// inProfile tells whether the link target t lies in a layer of dir's home or in dir/bin.
func inProfile(t, dir string) bool {
	rel, err := filepath.Rel(dir, t)
	if err != nil {
		return false
	}
	top, rest, ok := strings.Cut(rel, string(filepath.Separator))
	return ok && rest != "" && (top == "bin" || IsHomeLayer(top))
}

// ProvidedBy tells whether the profile in dir gives ~/rel on this machine, from any active layer.
func ProvidedBy(dir, rel string) bool {
	for _, l := range activeLayers() {
		if _, err := os.Lstat(filepath.Join(dir, l, rel)); err == nil {
			return true
		}
	}
	return false
}

// inLayerAt tells whether the link target t is <dir>/<any home layer>/rel.
func inLayerAt(t, dir, rel string) bool {
	r, err := filepath.Rel(dir, t)
	if err != nil {
		return false
	}
	top, rest, ok := strings.Cut(r, string(filepath.Separator))
	return ok && IsHomeLayer(top) && rest == filepath.Clean(rel)
}

// ShadowedBy names the active layer of the profile in dir, above layer, that already gives
// ~/rel: a file adopted into layer would never be linked here.
func ShadowedBy(dir, layer, rel string) (string, bool) {
	active := activeLayers()
	i := slices.Index(active, layer)
	if i < 0 {
		return "", false
	}
	for _, l := range active[i+1:] {
		if _, err := os.Lstat(filepath.Join(dir, l, rel)); err == nil {
			return l, true
		}
	}
	return "", false
}
