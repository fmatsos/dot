package guard

import (
	"bytes"
	"os"
)

const maxFile = 32 << 20 // bound of a file read whole for the terms check

// File checks one file that is not in a repository yet: its name (display, relative to ~) and
// content against the terms, then the secret scanner on the file itself. It reports no matched text.
func (g *Guard) File(path, name string) error {
	if g.Terms.Match(name) {
		return fail("forbidden reference in the file name")
	}
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return fail("file unreadable, refusing")
	}
	if fi.Size() > maxFile {
		return fail("file too large to check, refusing")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fail("file unreadable, refusing")
	}
	if !isMedia(path) { // as for staged files and history: names and secrets only for media
		for _, l := range bytes.Split(data, []byte{'\n'}) {
			if g.Terms.MatchLine(l) {
				return fail("forbidden reference in the content")
			}
		}
	}
	return g.leaks("dir", path)
}
