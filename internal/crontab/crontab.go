// Package crontab installs profile cron jobs in a managed block, preserving foreign lines.
package crontab

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const (
	startMarker = "# >>> dot >>>"
	endMarker   = "# <<< dot <<<"
)

// Options drives Run. Sources are crontab file paths in registry order.
type Options struct {
	Sources []string
	DryRun  bool
	Out     io.Writer
}

// Apply replaces the complete managed block, or appends block if none exists.
// block includes its markers and final newline; an empty block removes the old one.
func Apply(current, block string) string {
	start, offset := -1, 0
	for _, line := range strings.SplitAfter(current, "\n") {
		text := strings.TrimSuffix(line, "\n")
		if start < 0 && text == startMarker {
			start = offset
		} else if start >= 0 && text == endMarker {
			return current[:start] + block + current[offset+len(line):]
		}
		offset += len(line)
	}
	if block == "" {
		return current
	}
	if current != "" && !strings.HasSuffix(current, "\n") {
		current += "\n"
	}
	return current + block
}

// Run reads profile sources and updates only the managed part of the user's crontab.
func Run(o Options) error {
	var content strings.Builder
	for _, source := range o.Sources {
		data, err := os.ReadFile(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("%s illisible (%w)", source, err)
		}
		if text := strings.TrimRight(string(data), "\n"); text != "" {
			content.WriteString(text)
			content.WriteByte('\n')
		}
	}
	command, err := exec.LookPath("crontab")
	if err != nil {
		if content.Len() == 0 && errors.Is(err, exec.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("commande crontab introuvable (%w)", err)
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(command, "-l")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	current := ""
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || !strings.Contains(stderr.String(), "no crontab for") {
			return fmt.Errorf("lecture de la crontab impossible : %s (%w)", strings.TrimSpace(stderr.String()), err)
		}
	} else {
		current = stdout.String()
	}
	block := ""
	if content.Len() > 0 {
		block = startMarker + "\n" + content.String() + endMarker + "\n"
	}
	updated := Apply(current, block)
	if updated == current {
		fmt.Fprintln(o.Out, "crontab: à jour")
		return nil
	}
	if o.DryRun {
		if block == "" {
			fmt.Fprintln(o.Out, "[dry] crontab: bloc retiré")
		} else {
			fmt.Fprintf(o.Out, "[dry] crontab:\n%s", block)
		}
		return nil
	}
	// ponytail: cron validates job syntax on installation; dot only manages block boundaries.
	stderr.Reset()
	cmd = exec.Command(command, "-")
	cmd.Stdin, cmd.Stderr = strings.NewReader(updated), &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("installation de la crontab impossible : %s (%w)", strings.TrimSpace(stderr.String()), err)
	}
	fmt.Fprintln(o.Out, "crontab: mis à jour")
	return nil
}
