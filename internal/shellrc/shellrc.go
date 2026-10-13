// Package shellrc keeps dot's block in ~/.zshrc and ~/.bashrc: it loads the mise shims and the
// snippets a profile links into ~/.config/dot/rc.d. Everything outside the block belongs to the
// user. The block goes right after the shellkit block when there is one, else at the top.
package shellrc

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	begin    = "# >>> dot >>>"
	end      = "# <<< dot <<<"
	after    = "# <<< shellkit <<<"
	header   = "# Managed by dot (dot install): rewritten at each run. Your own lines go outside this block."
	miseLine = `[ -r "$HOME/.config/mise/shell.sh" ] && . "$HOME/.config/mise/shell.sh"`
)

// shells maps an rc file to its shell and the loop that sources ~/.config/dot/rc.d, in name order.
// ponytail: the loop is per shell (zsh globs, bash case); ceiling: no other shell.
var shells = []struct{ bin, file, loop string }{
	{"zsh", ".zshrc", `for _f in "$HOME"/.config/dot/rc.d/*.(sh|zsh)(N); do . "$_f"; done; unset _f`},
	{"bash", ".bashrc", `for _f in "$HOME"/.config/dot/rc.d/*; do case $_f in *.sh|*.bash) [ -r "$_f" ] && . "$_f" ;; esac; done; unset _f`},
}

// Options drives Ensure.
type Options struct {
	Home     string
	Dry      bool
	Out, Err io.Writer
	LookPath func(string) (string, error)
	// Nodes are the pinned nvm node versions ("22.11.0"): their bin directory goes at the end of
	// PATH, for the CLIs mise cannot install (the manifest's `nvm`).
	Nodes []string
}

func block(loop string, nodes []string) string {
	lines := []string{begin, header, miseLine}
	for _, n := range nodes {
		lines = append(lines, fmt.Sprintf(`export PATH="$PATH:$HOME/.nvm/versions/node/v%s/bin"`, n))
	}
	return strings.Join(append(lines, loop, end), "\n") + "\n"
}

// Splice returns text with blk in place of the dot block, or added after the shellkit block, or at
// the top. A begin marker without its end is an error: replacing to the end of the file would eat
// the user's lines.
func Splice(text, blk string) (string, error) {
	lines := strings.SplitAfter(text, "\n")
	find := func(marker string, from int) int {
		for i := from; i < len(lines); i++ {
			if strings.TrimRight(lines[i], "\r\n") == marker {
				return i
			}
		}
		return -1
	}
	if i := find(begin, 0); i >= 0 {
		j := find(end, i+1)
		if j < 0 {
			return "", errors.New("bloc dot sans marqueur de fin (" + end + ")")
		}
		return strings.Join(lines[:i], "") + blk + strings.Join(lines[j+1:], ""), nil
	}
	at := 0
	if j := find(after, 0); j >= 0 {
		at = j + 1
	}
	head, tail := strings.Join(lines[:at], ""), strings.TrimLeft(strings.Join(lines[at:], ""), "\n")
	if head != "" && !strings.HasSuffix(head, "\n") {
		head += "\n"
	}
	if tail != "" {
		blk += "\n"
	}
	if head != "" {
		head += "\n"
	}
	return head + blk + tail, nil
}

// Ensure writes the dot block into each shell's rc file: created when the shell is installed,
// rewritten when it differs, never written through a symlink (an old shellkit link would change
// shellkit's own file).
func Ensure(o Options) error {
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.Err == nil {
		o.Err = io.Discard
	}
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	for _, s := range shells {
		path := filepath.Join(o.Home, s.file)
		fi, err := os.Lstat(path)
		switch {
		case err == nil && fi.Mode()&os.ModeSymlink != 0:
			fmt.Fprintf(o.Err, "  shell : ~/%s est un lien, bloc dot non écrit (shellkit : relance son install.sh)\n", s.file)
			continue
		case err != nil && !errors.Is(err, os.ErrNotExist):
			return err
		case err != nil:
			if _, e := o.LookPath(s.bin); e != nil {
				continue
			}
		}
		var old []byte
		mode := os.FileMode(0o644)
		if err == nil {
			if old, err = os.ReadFile(path); err != nil {
				return err
			}
			mode = fi.Mode().Perm()
		}
		text, err := Splice(string(old), block(s.loop, o.Nodes))
		if err != nil {
			return fmt.Errorf("~/%s : %w", s.file, err)
		}
		if text == string(old) {
			continue
		}
		fmt.Fprintf(o.Out, "  shell : bloc dot écrit dans ~/%s\n", s.file)
		if o.Dry {
			continue
		}
		if err := os.WriteFile(path, []byte(text), mode); err != nil {
			return err
		}
	}
	return nil
}
