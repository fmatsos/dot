package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/fmatsos/dot/internal/guard"
	"github.com/fmatsos/dot/internal/link"
	"github.com/fmatsos/dot/internal/manifest"
	"github.com/spf13/cobra"
)

func init() { register(newAdoptCmd) }

// adoptGuard builds the guard of the target profile: its forbidden.local ($DOTFILES_FORBIDDEN
// overrides it, like for dot guard), or secrets only when $DOTFILES_GUARD or the clone's
// dotfiles.guard says so. A missing or invalid list refuses.
func adoptGuard(env *Env, dir string) (*guard.Guard, error) {
	g := &guard.Guard{Stderr: env.Stderr, Scan: scanner(env)}
	mode, src := env.Getenv("DOTFILES_GUARD"), "$DOTFILES_GUARD"
	if mode == "" {
		var err error
		if mode, err = gitText(dir, "config", "--get", "dotfiles.guard"); err != nil {
			mode = ""
		}
		src = "dotfiles.guard"
	}
	if mode != "" && mode != "secrets" {
		return nil, fmt.Errorf("%s: unknown value, refused", src)
	}
	if mode == "secrets" {
		return g, nil
	}
	path := filepath.Join(dir, "forbidden.local")
	if f := env.Getenv("DOTFILES_FORBIDDEN"); f != "" {
		path = env.Expand(f)
	}
	var err error
	if g.Terms, err = guard.LoadTerms(path); err != nil {
		if errors.Is(err, guard.ErrAbsent) {
			return nil, fmt.Errorf("forbidden terms list missing or empty (%s), refused", path)
		}
		return nil, fmt.Errorf("forbidden terms list invalid or unreadable (%s), refused", path)
	}
	return g, nil
}

// sparseHasLayer refuses an adoption into a clone whose sparse set leaves the layer (home or a
// home@ variant) out.
func sparseHasLayer(dir, layer string) error {
	m, err := manifest.Load(filepath.Join(dir, "dot.json"))
	if err != nil {
		if _, serr := os.Stat(filepath.Join(dir, "dot.json")); serr != nil {
			return nil // no manifest: nothing says the clone is sparse
		}
		return err
	}
	if len(m.Sparse) > 0 && !slices.Contains(m.Sparse, layer) {
		return fmt.Errorf("%s/ is outside the profile's sparse checkout (deploy.sparse: %s)", layer, strings.Join(m.Sparse, ", "))
	}
	return nil
}

func newAdoptCmd(env *Env) *cobra.Command {
	var dry, onOS, onHost bool
	cmd := &cobra.Command{
		Use:   "adopt [-p <key>] [-n] [--os | --host] <file>...",
		Short: "Move existing files from ~ into a profile and replace them with links",
		Long: "For each regular file in ~: moves it to <profile>/home/<same path> and creates in its place the link\n" +
			"that dot install would have created. The target profile is -p, $DOT_PROFILE, then the registry's default.\n" +
			"--os moves into home@<system>/ (darwin, linux), --host into home@<machine>/: variants of home/ that\n" +
			"apply only to that system or that machine (the sparse checkout must list them).\n\n" +
			"Refusals (exit 1, the other files continue): outside ~ or inside ~/.dot, symbolic link (an already\n" +
			"linked file too), directory, existing destination, file already linked by another profile, home/ outside the sparse\n" +
			"checkout, forbidden term from the target profile's list, secret (betterleaks). The check precedes any\n" +
			"move and reports only the file name. Nothing is committed: dot push does that.\n\n" +
			"-n shows the plan and runs the checks without writing anything.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usagef("usage: dot adopt [-p <key>] [-n] <file>...")
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			layer := "home"
			switch {
			case onOS && onHost:
				return usagef("adopt: --os and --host are mutually exclusive")
			case onOS:
				layer = link.OSLayer()
			case onHost:
				var ok bool
				if layer, ok = link.HostLayer(); !ok {
					return errors.New("adopt: --host: unknown or invalid machine name")
				}
			}
			dir, err := env.ProfileDir()
			if err != nil {
				return err
			}
			dir, _ = filepath.Abs(dir)
			key := filepath.Base(dir)
			if _, err := os.Stat(filepath.Join(dir, layer)); err != nil {
				if fi, serr := os.Stat(dir); serr != nil || !fi.IsDir() {
					return fmt.Errorf("adopt: deployed clone missing (%s)", dir)
				}
			}
			if err := sparseHasLayer(dir, layer); err != nil {
				return fmt.Errorf("adopt: %v", err)
			}
			g, err := adoptGuard(env, dir)
			if err != nil {
				return fmt.Errorf("adopt: %v", err)
			}
			var others []link.Profile
			if keys, err := env.ProfileKeys(); err == nil {
				for _, k := range keys {
					if d, _ := filepath.Abs(env.ProfileDirFor(k)); d != dir {
						others = append(others, link.Profile{Key: k, Dir: d})
					}
				}
			}
			cwd, _ := os.Getwd()
			failed, done := false, 0
			for _, arg := range args {
				shown := func(s string) string { // a name holding a term is never printed
					if g.Terms.Match(s) {
						return "<masked name>"
					}
					return s
				}
				refuse := func(shownPath, why string) {
					if g.Terms.Match(why) { // a reason may quote the path relative to ~
						why = "detail masked (forbidden reference)"
					}
					fmt.Fprintf(env.Stderr, "adopt: %s refused: %s\n", shown(shownPath), why)
					failed = true
				}
				src, rel, err := link.AdoptSource(env.Home, cwd, env.Expand(arg))
				if err != nil {
					refuse(arg, err.Error())
					continue
				}
				tilde := link.Tilde(env.Home, src)
				dst := filepath.Join(dir, layer, rel)
				if err := link.SafeDest(dir, dst); err != nil {
					refuse(tilde, err.Error())
					continue
				}
				if _, err := os.Lstat(dst); err == nil {
					refuse(tilde, fmt.Sprintf("destination exists (%s:%s/%s)", key, layer, filepath.ToSlash(rel)))
					continue
				}
				if above, ok := link.ShadowedBy(dir, layer, rel); ok {
					refuse(tilde, fmt.Sprintf("shadowed by %s:%s/%s, which wins over %s", key, above, filepath.ToSlash(rel), layer))
					continue
				}
				// With several profiles, a module source is merged by dot ai settings/mcp, never linked:
				// other profiles may hold their own copy, and a link in ~ would shadow the merge.
				merged := len(others) > 0 && link.IsModuleSource(rel)
				if merged && layer != "home" {
					refuse(tilde, "module source: dot ai settings and dot ai mcp only read home/, not the variants")
					continue
				}
				if owners := link.ClaimedBy(rel, others); len(owners) > 0 && !merged {
					refuse(tilde, fmt.Sprintf("file linked by several profiles: %s (home/%s) and %s", strings.Join(owners, ", "), filepath.ToSlash(rel), key))
					continue
				}
				if err := g.File(src, tilde); err != nil {
					msg := err.Error()
					var fl *guard.Failure
					if errors.As(err, &fl) {
						msg = fl.Msg
					}
					refuse(tilde, msg)
					continue
				}
				fmt.Fprintf(env.Stdout, "adopt %s -> %s:%s/%s\n", tilde, key, layer, filepath.ToSlash(rel))
				if dry {
					continue
				}
				if err := link.AdoptMove(src, dst, !merged); err != nil {
					refuse(tilde, err.Error())
					continue
				}
				if merged {
					fmt.Fprintf(env.Stdout, "  %s is not linked: dot ai settings and dot ai mcp merge the profiles\n", tilde)
				}
				done++
			}
			if failed {
				return exitError{code: 1}
			}
			if done > 0 {
				fmt.Fprintln(env.Stdout, "adopted: nothing is committed, run dot push to commit.")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&onOS, "os", false, "move into home@<system>/ (variant for this system)")
	cmd.Flags().BoolVar(&onHost, "host", false, "move into home@<machine>/ (variant for this machine)")
	cmd.Flags().BoolVarP(&dry, "dry-run", "n", false, "show the plan and check without writing anything")
	return cmd
}
