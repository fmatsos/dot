package install

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// git runs git (in dir when not empty) and folds its stderr into the error.
func git(dir string, args ...string) error {
	_, err := gitOut(dir, args...)
	return err
}

func gitOut(dir string, args ...string) ([]byte, error) {
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	var stderr bytes.Buffer
	c := exec.Command("git", args...)
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return out, fmt.Errorf("git %s : %w : %s", strings.Join(args, " "), err, msg)
		}
		return out, fmt.Errorf("git %s : %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// gitText is the trimmed stdout of a global (no -C) git command.
func gitText(args ...string) (string, error) {
	out, err := gitOut("", args...)
	return strings.TrimSpace(string(out)), err
}

// pull is `git pull --rebase` on a clone, its output passed through.
func pull(dir string, out, errOut io.Writer) error {
	c := exec.Command("git", "-C", dir, "pull", "--rebase")
	c.Stdout, c.Stderr = out, errOut
	return c.Run()
}
