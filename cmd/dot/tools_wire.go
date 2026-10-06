package main

import (
	"os"
	"path/filepath"

	"github.com/fmatsos/dot/internal/tools"
)

func init() { runTools = installTools }

// installTools maps an install pass onto internal/tools: mise only if a profile links a config.toml.
func installTools(_ *Env, req installToolsRequest) error {
	o := tools.Options{Home: req.Home, Dry: req.Dry, Skip: req.Skip, Out: req.Out, Err: req.Err}
	for _, p := range req.Profiles {
		if _, err := os.Stat(filepath.Join(p.Dir, "home", ".config", "mise", "config.toml")); err == nil {
			o.MiseConfig = true
		}
		if p.Manifest == nil {
			continue
		}
		if p.Manifest.NVM != nil {
			o.NVM = append(o.NVM, *p.Manifest.NVM)
		}
		if p.Manifest.Marketplace != nil {
			o.Marketplaces = append(o.Marketplaces, *p.Manifest.Marketplace)
		}
	}
	return tools.Run(o)
}
