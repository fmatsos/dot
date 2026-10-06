package link

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// machine fakes the OS and hostname for a test.
func machine(t *testing.T, goos, host string) {
	t.Helper()
	o, h := GOOS, HostFn
	GOOS, HostFn = goos, func() string { return host }
	t.Cleanup(func() { GOOS, HostFn = o, h })
}

// variants builds a profile with the same file in every layer plus single-layer files.
func variants(t *testing.T) (home, dir string) {
	t.Helper()
	root := t.TempDir()
	home, dir = filepath.Join(root, "home"), filepath.Join(root, "p")
	must(t, os.MkdirAll(home, 0o755))
	for _, l := range []string{"home", "home@darwin", "home@linux", "home@work", "home@other"} {
		write(t, filepath.Join(dir, l, ".rc"), l, 0o644)
	}
	write(t, dir+"/home/.base", "base", 0o644)
	write(t, dir+"/home@darwin/.mac", "mac", 0o644)
	write(t, dir+"/home@linux/.lin", "lin", 0o644)
	write(t, dir+"/home@other/.else", "else", 0o644)
	return
}

func srcOf(t *testing.T, home string, links []Link, rel string) string {
	t.Helper()
	for _, k := range links {
		if k.Dst == filepath.Join(home, rel) {
			return strings.TrimPrefix(k.Src, filepath.Dir(filepath.Dir(k.Src))+"/") // <layer>/<file>
		}
	}
	return ""
}

func TestLayerPrecedence(t *testing.T) {
	home, dir := variants(t)
	for _, c := range []struct{ goos, host, rc string }{
		{"darwin", "work", "home@work/.rc"},
		{"darwin", "laptop", "home@darwin/.rc"},
		{"linux", "laptop", "home@linux/.rc"},
		{"freebsd", "laptop", "home/.rc"},
	} {
		machine(t, c.goos, c.host)
		got, err := Plan(dir, home)
		must(t, err)
		if s := srcOf(t, home, got, ".rc"); s != c.rc {
			t.Errorf("%s/%s: .rc from %q, want %q", c.goos, c.host, s, c.rc)
		}
		if srcOf(t, home, got, ".base") == "" {
			t.Errorf("%s/%s: .base missing", c.goos, c.host)
		}
		if srcOf(t, home, got, ".else") != "" {
			t.Errorf("%s/%s: other host variant linked", c.goos, c.host)
		}
	}
	machine(t, "darwin", "laptop")
	got, _ := Plan(dir, home)
	if srcOf(t, home, got, ".mac") == "" || srcOf(t, home, got, ".lin") != "" {
		t.Errorf("OS variants not filtered: %v", got)
	}
}

func TestInvalidHostIgnored(t *testing.T) {
	home, dir := variants(t)
	for _, h := range []string{"", "Bad_Name", "-x", "linux", "darwin", "a b"} {
		machine(t, "darwin", h)
		got, err := Plan(dir, home)
		must(t, err)
		if s := srcOf(t, home, got, ".rc"); s != "home@darwin/.rc" {
			t.Errorf("host %q: .rc from %q", h, s)
		}
	}
	machine(t, "linux", "WORK.example.com") // short, lowercased
	if l, ok := HostLayer(); !ok || l != "home@work" {
		t.Errorf("HostLayer = %q %v", l, ok)
	}
}

func TestConflictsAfterResolution(t *testing.T) {
	home, a := variants(t)
	b := filepath.Join(filepath.Dir(a), "b")
	// b only has the .rc of another OS: no conflict with a here.
	write(t, b+"/home@linux/.rc", "x", 0o644)
	machine(t, "darwin", "laptop")
	if _, err := Conflicts(home, []Profile{{"a", a}, {"b", b}}); err != nil {
		t.Fatalf("unexpected conflict: %v", err)
	}
	machine(t, "linux", "laptop")
	cs, err := Conflicts(home, []Profile{{"a", a}, {"b", b}})
	if err == nil || len(cs) != 1 || cs[0].Dst != filepath.Join(home, ".rc") {
		t.Fatalf("want one conflict on .rc, got %v %v", cs, err)
	}
	if !strings.Contains(err.Error(), "home@linux/.rc") {
		t.Errorf("message should name the winning layers: %v", err)
	}
	// a's own layers never conflict with each other.
	if _, err := Conflicts(home, []Profile{{"a", a}}); err != nil {
		t.Fatal(err)
	}
}

func TestApplyShowsLayerAndRepointsOrphans(t *testing.T) {
	home, dir := variants(t)
	machine(t, "linux", "work")
	var out bytes.Buffer
	l := New(home, false, &out, nil, clock)
	must(t, l.Apply(dir))
	if !strings.Contains(out.String(), "lien : ~/.rc (home@work)\n") || !strings.Contains(out.String(), "lien : ~/.base\n") {
		t.Fatalf("output: %s", out.String())
	}
	if got, _ := os.Readlink(filepath.Join(home, ".rc")); got != dir+"/home@work/.rc" {
		t.Fatalf("link = %s", got)
	}
	// The host variant disappears: the link goes down to the OS layer, nothing is backed up.
	must(t, os.Remove(dir+"/home@work/.rc"))
	out.Reset()
	l = New(home, false, &out, nil, clock)
	must(t, l.Apply(dir))
	if got, _ := os.Readlink(filepath.Join(home, ".rc")); got != dir+"/home@linux/.rc" {
		t.Fatalf("re-pointed link = %s", got)
	}
	if strings.Contains(out.String(), "sauvegarde") {
		t.Errorf("a dot link must not be backed up: %s", out.String())
	}
	if d := Detached(dir, home); len(d) != 0 {
		t.Errorf("detached = %v", d)
	}
	// A file that only lived in a variant is removed when the variant loses it.
	write(t, dir+"/home@work/.only", "o", 0o644)
	must(t, New(home, false, nil, nil, clock).Apply(dir))
	must(t, os.Remove(dir+"/home@work/.only"))
	out.Reset()
	must(t, New(home, false, &out, nil, clock).Apply(dir))
	if _, err := os.Lstat(filepath.Join(home, ".only")); err == nil || !strings.Contains(out.String(), "lien mort retiré : ~/.only") {
		t.Errorf("orphan kept: %s", out.String())
	}
}

func TestDetachedIgnoresOtherVariants(t *testing.T) {
	home, dir := variants(t)
	machine(t, "darwin", "laptop")
	must(t, New(home, false, nil, nil, clock).Apply(dir))
	if d := Detached(dir, home); len(d) != 0 {
		t.Fatalf("detached = %v", d)
	}
	for _, rel := range []string{".lin", ".else"} {
		if _, err := os.Lstat(filepath.Join(home, rel)); err == nil {
			t.Errorf("%s linked", rel)
		}
	}
	must(t, os.Remove(filepath.Join(home, ".rc")))
	write(t, filepath.Join(home, ".rc"), "own", 0o644)
	if d := Detached(dir, home); len(d) != 1 || d[0] != "~/.rc" {
		t.Errorf("detached = %v", d)
	}
}

func TestUnlinkRemovesInactiveLayerLinks(t *testing.T) {
	home, dir := variants(t)
	machine(t, "linux", "work")
	must(t, New(home, false, nil, nil, clock).Apply(dir))
	// A link into a layer that is not active here (left by a previous host name).
	must(t, os.Symlink(dir+"/home@other/.else", filepath.Join(home, ".else")))
	must(t, Unlink(dir, home, nil))
	for _, rel := range []string{".rc", ".base", ".lin", ".else"} {
		if _, err := os.Lstat(filepath.Join(home, rel)); err == nil {
			t.Errorf("%s still there", rel)
		}
	}
}

func TestRestoreSeesVariantLinksAsManaged(t *testing.T) {
	home, dir := variants(t)
	if why := restoreBlockerFor(home, dir); why != "" {
		t.Errorf("blocker = %q", why)
	}
}

func restoreBlockerFor(home, dir string) string {
	dst := filepath.Join(home, ".rc")
	_ = os.Symlink(dir+"/home@work/.rc", dst)
	return restoreBlocker(home, dst, []string{dir})
}

func TestClaimedByUsesActiveLayers(t *testing.T) {
	_, dir := variants(t)
	machine(t, "darwin", "laptop")
	if got := ClaimedBy(".mac", []Profile{{"a", dir}}); len(got) != 1 {
		t.Errorf("claimed = %v", got)
	}
	if got := ClaimedBy(".lin", []Profile{{"a", dir}}); len(got) != 0 {
		t.Errorf("claimed = %v", got)
	}
}

func TestMultiProfileDropsModuleLinkFromVariant(t *testing.T) {
	root := t.TempDir()
	home, a, b := filepath.Join(root, "home"), filepath.Join(root, "a"), filepath.Join(root, "b")
	must(t, os.MkdirAll(home, 0o755))
	machine(t, "linux", "work")
	write(t, a+"/home/.a", "a", 0o644)
	write(t, a+"/home@work/.config/mcp/servers.json", "{}", 0o644)
	write(t, b+"/home/.b", "b", 0o644)
	must(t, New(home, false, nil, nil, clock).Apply(a))
	dst := filepath.Join(home, ".config/mcp/servers.json")
	if got, _ := os.Readlink(dst); got != a+"/home@work/.config/mcp/servers.json" {
		t.Fatalf("single-profile link = %q", got)
	}
	// A second profile: module sources are merged by dot mcp, so the variant link must go.
	l := New(home, false, nil, nil, clock)
	l.Registered = []string{a, b}
	must(t, l.Apply(a))
	if _, err := os.Lstat(dst); err == nil {
		t.Errorf("module link from a variant kept in multi-profile mode")
	}
}

func TestShadowedBy(t *testing.T) {
	_, dir := variants(t)
	machine(t, "linux", "work")
	if l, ok := ShadowedBy(dir, "home", ".rc"); !ok || l != "home@linux" {
		t.Errorf("home/.rc: %q %v", l, ok)
	}
	if l, ok := ShadowedBy(dir, "home@linux", ".rc"); !ok || l != "home@work" {
		t.Errorf("home@linux/.rc: %q %v", l, ok)
	}
	if _, ok := ShadowedBy(dir, "home@work", ".rc"); ok {
		t.Error("the top layer is never shadowed")
	}
	if _, ok := ShadowedBy(dir, "home", ".new"); ok {
		t.Error("a path no layer gives is not shadowed")
	}
}

func TestOrphanUnderRootOnlyAnInactiveLayerGave(t *testing.T) {
	root := t.TempDir()
	home, dir := filepath.Join(root, "home"), filepath.Join(root, "p")
	must(t, os.MkdirAll(home, 0o755))
	write(t, dir+"/home/.base", "b", 0o644)
	write(t, dir+"/home@work/.config/app/config", "w", 0o644)
	machine(t, "linux", "work")
	must(t, New(home, false, nil, nil, clock).Apply(dir))
	dst := filepath.Join(home, ".config/app/config")
	if _, err := os.Readlink(dst); err != nil {
		t.Fatalf("host link missing: %v", err)
	}
	// Another host: nothing planned under ~/.config any more, the old link must still go.
	machine(t, "linux", "desk")
	must(t, New(home, false, nil, nil, clock).Apply(dir))
	if _, err := os.Lstat(dst); err == nil {
		t.Error("link into an inactive host layer kept")
	}
}

func TestFileAndDescendantAcrossLayers(t *testing.T) {
	root := t.TempDir()
	home, dir := filepath.Join(root, "home"), filepath.Join(root, "p")
	must(t, os.MkdirAll(home, 0o755))
	write(t, dir+"/home/.config/app/rc", "base", 0o644)
	write(t, dir+"/home@work/.config", "file", 0o644) // a file above a lower layer's folder
	write(t, dir+"/home/.tool", "file", 0o644)
	write(t, dir+"/home@linux/.tool/conf", "dir", 0o644) // a folder above a lower layer's file
	machine(t, "linux", "work")
	must(t, New(home, false, nil, nil, clock).Apply(dir))
	if got, _ := os.Readlink(filepath.Join(home, ".config")); got != dir+"/home@work/.config" {
		t.Errorf("~/.config = %q", got)
	}
	if got, _ := os.Readlink(filepath.Join(home, ".tool/conf")); got != dir+"/home@linux/.tool/conf" {
		t.Errorf("~/.tool/conf = %q", got)
	}
}
