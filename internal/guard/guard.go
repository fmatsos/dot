package guard

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fmatsos/dot/internal/manifest"
)

// Failure is a refusal; its message names files, refs or commits, never matched text.
type Failure struct{ Msg string }

func (f *Failure) Error() string { return f.Msg }

func fail(format string, a ...any) error { return &Failure{fmt.Sprintf(format, a...)} }

// Guard checks one repository. Terms is nil in secrets-only mode.
type Guard struct {
	Terms  *Terms
	Dir    string                     // repository directory; "" = current directory
	Scan   func(args ...string) error // runs the secret scanner with these arguments (after the global flags)
	Stderr io.Writer
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{40,64}$`)

func (g *Guard) git(args ...string) *exec.Cmd {
	c := exec.Command("git", args...)
	c.Dir = g.Dir
	c.Stderr = g.Stderr
	return c
}

// cmdFail names the failed command; an argument holding a term (a remote name) is masked.
func (g *Guard) cmdFail(args []string) error {
	return fail("commande en échec, contrôle impossible : git %s", g.shown(args...))
}

// shown joins arguments for a message, masking those that match a term.
func (g *Guard) shown(args ...string) string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = a
		if g.Terms.Match(a) {
			out[i] = "<masqué>"
		}
	}
	return strings.Join(out, " ")
}

// output runs git and returns its stdout.
func (g *Guard) output(args ...string) ([]byte, error) {
	c := g.git(args...)
	var out bytes.Buffer
	c.Stdout = &out
	if err := c.Run(); err != nil {
		return nil, g.cmdFail(args)
	}
	return out.Bytes(), nil
}

// hits runs git and reports whether a line of its output matches a term. With prefix != 0 only
// the lines starting with that byte count (the lines a range adds). A failing command is an error.
func (g *Guard) hits(prefix byte, args ...string) (bool, error) {
	if g.Terms == nil {
		return false, nil
	}
	c := g.git(args...)
	pipe, err := c.StdoutPipe()
	if err != nil || c.Start() != nil {
		return false, g.cmdFail(args)
	}
	r := bufio.NewReader(pipe)
	for {
		line, rerr := r.ReadBytes('\n')
		line = bytes.TrimSuffix(line, []byte{'\n'})
		if (prefix == 0 || (len(line) > 0 && line[0] == prefix)) && g.Terms.MatchLine(line) {
			_ = c.Process.Kill()
			_ = c.Wait()
			return true, nil
		}
		if rerr != nil {
			if rerr != io.EOF {
				_ = c.Process.Kill()
				_ = c.Wait()
				return false, g.cmdFail(args)
			}
			break
		}
	}
	if c.Wait() != nil {
		return false, g.cmdFail(args)
	}
	return false, nil
}

func (g *Guard) leaks(args ...string) error {
	if g.Scan == nil {
		return fail("betterleaks indisponible, refus.")
	}
	err := g.Scan(args...)
	var x *exec.ExitError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &x):
		return fail("betterleaks a signalé un secret (ou n'a pas pu tourner).")
	default:
		return fail("%v", err)
	}
}

// history checks identity, messages, file names and added contents of a revision range, then secrets.
func (g *Guard) history(revs ...string) error {
	raw := strings.Join(revs, " ")
	r := g.shown(revs...)
	meta := append([]string{"-c", "core.quotePath=false", "log", "--format=%an%n%ae%n%cn%n%ce%n%B", "--name-only", "--diff-merges=first-parent"}, revs...)
	if hit, err := g.hits(0, meta...); err != nil {
		return err
	} else if hit {
		return fail("référence interdite dans les métadonnées ou noms de fichiers de : %s", r)
	}
	// --text: binary and -diff files are scanned too; first-parent: a merge shows what it adds.
	added := append([]string{"log", "-p", "-U0", "--text", "--diff-merges=first-parent", "--no-color", "--format="}, revs...)
	if hit, err := g.hits('+', added...); err != nil {
		return err
	} else if hit {
		return fail("référence interdite dans le contenu ajouté par : %s", r)
	}
	return g.leaks("git", "--log-opts="+raw, ".")
}

// Staged checks what is about to be committed: identity, names and contents of the indexed
// files, a changed dot.json, then secrets.
func (g *Guard) Staged() error {
	for _, v := range []struct{ name, what string }{
		{"GIT_AUTHOR_IDENT", "auteur"}, {"GIT_COMMITTER_IDENT", "committer"},
	} {
		if hit, err := g.hits(0, "var", v.name); err != nil {
			return err
		} else if hit {
			return fail("identité %s liée au travail.", v.what)
		}
	}
	changed, err := g.output("diff", "--cached", "--name-only", "--", "dot.json")
	if err != nil {
		return fail("état de dot.json indexé illisible.")
	}
	if len(bytes.TrimSpace(changed)) > 0 {
		if err := g.checkManifest(); err != nil {
			return err
		}
	}
	names, err := g.output("-c", "core.quotePath=false", "diff", "--cached", "--name-only", "--diff-filter=d", "-z")
	if err != nil {
		return fail("liste des fichiers indexés illisible.")
	}
	var bad strings.Builder
	n := 0
	for _, f := range strings.Split(string(names), "\x00") {
		if f == "" {
			continue
		}
		n++
		if g.Terms.Match(f) { // the name itself is forbidden text: never printed
			fmt.Fprintf(&bad, "  <nom masqué> (fichier %d)\n", n)
			continue
		}
		hit, err := g.hits(0, "show", ":0:"+f)
		if err != nil {
			return err
		}
		if hit {
			bad.WriteString("  " + printable(f) + "\n")
		}
	}
	if bad.Len() > 0 {
		return fail("référence interdite (nom ou contenu) dans :\n%s", strings.TrimSuffix(bad.String(), "\n"))
	}
	return g.leaks("git", "--staged", ".")
}

// printable keeps a file name from driving the terminal.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, s)
}

// checkManifest validates the indexed dot.json, whatever the working tree holds.
func (g *Guard) checkManifest() error {
	data, err := g.output("show", ":dot.json")
	if err != nil {
		return fail("dot.json indexé illisible, refus.")
	}
	dir, err := os.MkdirTemp("", "dot-guard-")
	if err != nil {
		return fail("dot.json indexé : dossier temporaire indisponible, refus.")
	}
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "dot.json")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return fail("dot.json indexé illisible, refus.")
	}
	if _, err := manifest.Load(p); err != nil {
		key := "objet"
		var ke *manifest.KeyError
		if errors.As(err, &ke) {
			key = ke.Key
		}
		if g.Terms.Match(key) { // a key may itself name a forbidden term
			key = "masquée"
		}
		return fail("dot.json invalide (clé %s) : dot doctor pour le détail, refus.", key)
	}
	return nil
}

// Msg checks a commit message file; lines starting with # are git comments.
func (g *Guard) Msg(path string) error {
	if g.Terms == nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fail("message de commit illisible, refus.")
	}
	for _, l := range bytes.Split(data, []byte{'\n'}) {
		if !bytes.HasPrefix(l, []byte{'#'}) && g.Terms.MatchLine(l) {
			return fail("référence interdite dans le message de commit.")
		}
	}
	return nil
}

// known reports whether a commit exists locally.
func (g *Guard) known(sha string) bool {
	c := g.git("cat-file", "-e", sha+"^{commit}")
	c.Stderr = nil
	return c.Run() == nil
}

// Push checks what a push sends, from the pre-push stdin: "<lref> <lsha> <rref> <rsha>" per line.
// remote is the name git hands the hook ("" when unknown): a new ref is checked against what
// that remote already holds, not against every remote.
func (g *Guard) Push(in io.Reader, remote string) error {
	notOn := "--remotes"
	if remote != "" && !strings.ContainsAny(remote, "*?[\\ \t\n") { // a glob or a space would widen or split the option
		notOn = "--remotes=" + remote
	}
	sc := bufio.NewScanner(in)
	n := 0
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		if len(f) != 4 {
			return fail("entrée du hook pre-push invalide, refus.")
		}
		n++
		lref, lsha, rref, rsha := f[0], f[1], f[2], f[3]
		if strings.Trim(lsha, "0") == "" {
			continue // deleting a remote ref
		}
		if g.Terms.Match(lref + "\n" + rref + "\n") {
			if g.Terms.Match(lref) { // never print a forbidden name
				lref = fmt.Sprintf("<nom masqué> (ligne %d)", n)
			}
			return fail("nom de branche ou de tag interdit : %s", printable(lref))
		}
		if !shaRe.MatchString(lsha) || !shaRe.MatchString(rsha) {
			return fail("entrée du hook pre-push invalide, refus.")
		}
		var err error
		// New ref, or remote tip unknown locally (force push from a stale clone): check everything
		// not already on a remote instead of trusting an unresolvable range.
		if strings.Trim(rsha, "0") == "" || !g.known(rsha) {
			err = g.history(lsha, "--not", notOn)
		} else {
			err = g.history(rsha + ".." + lsha)
		}
		if err != nil {
			return err
		}
		if err = g.tags(lsha); err != nil {
			return err
		}
	}
	if sc.Err() != nil {
		return fail("entrée du hook pre-push illisible, refus.")
	}
	return nil
}

// All checks every ref and the whole history.
func (g *Guard) All() error {
	if hit, err := g.hits(0, "for-each-ref", "--format=%(refname)"); err != nil {
		return err
	} else if hit {
		return fail("nom de branche ou de tag interdit.")
	}
	if err := g.allTags(); err != nil {
		return err
	}
	if hit, err := g.hits(0, "-c", "core.quotePath=false", "log", "--all", "--format=%an%n%ae%n%cn%n%ce%n%B", "--name-only", "--diff-merges=first-parent"); err != nil {
		return err
	} else if hit {
		return fail("référence interdite dans les métadonnées ou noms de fichiers de l'historique.")
	}
	if hit, err := g.hits('+', "log", "-p", "-U0", "--text", "--diff-merges=first-parent", "--no-color", "--format=", "--all"); err != nil {
		return err
	} else if hit {
		return fail("référence interdite dans le contenu de l'historique.")
	}
	return g.leaks("git", ".")
}

// maxTagChain bounds the tags followed from one ref (git cannot make a cycle); a variable for tests.
var maxTagChain = 1024

// tags checks the annotated tag objects (tagger identity, name, message) on the chain from sha:
// a tag of a tag is followed. Anything else, a commit included, is not a tag and passes.
// A chain longer than maxTagChain is refused: what is not examined is not trusted.
func (g *Guard) tags(sha string) error {
	start := sha
	for range maxTagChain {
		kind, err := g.output("cat-file", "-t", sha)
		if err != nil {
			return err
		}
		if strings.TrimSpace(string(kind)) != "tag" {
			return nil
		}
		body, err := g.output("cat-file", "tag", sha)
		if err != nil {
			return err
		}
		if g.Terms.Match(string(body)) {
			return fail("référence interdite dans le tag annoté : %s", sha)
		}
		first, _, _ := strings.Cut(string(body), "\n")
		next, ok := strings.CutPrefix(first, "object ")
		if !ok || !shaRe.MatchString(next) {
			return fail("tag annoté illisible : %s", sha)
		}
		sha = next
	}
	return fail("chaîne de tags trop profonde pour %s, refus.", start)
}

// allTags checks every annotated tag of the repository.
func (g *Guard) allTags() error {
	out, err := g.output("for-each-ref", "--format=%(objecttype) %(objectname)", "refs/tags")
	if err != nil {
		return err
	}
	for _, l := range strings.Split(string(out), "\n") {
		if kind, sha, ok := strings.Cut(l, " "); ok && kind == "tag" {
			if err := g.tags(sha); err != nil {
				return err
			}
		}
	}
	return nil
}
