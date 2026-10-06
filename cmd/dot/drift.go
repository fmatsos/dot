package main

import (
	"os"

	"github.com/fmatsos/dot/internal/link"
)

// ponytail: driftReport has no Env in its signature, so the home is read from the process like main does.
func init() {
	driftReport = func(dir string) []string {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		return link.Drift(dir, home)
	}
}
