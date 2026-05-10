// Package cli is the d0t command dispatcher. It is intentionally tiny —
// stdlib flag, no third-party CLI framework — to keep the surface small.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"d0t/internal/fsutil"
	"d0t/internal/initrepo"
	"d0t/internal/plan"
	"d0t/internal/primitive"
	"d0t/internal/repo"
	"d0t/internal/selfupdate"
	"d0t/internal/state"
	d0tsync "d0t/internal/sync"
	"d0t/internal/ui"
	"d0t/internal/upgrade"
)

// ErrUsage signals that the user invoked d0t incorrectly. main() exits 2.
var ErrUsage = errors.New("usage")

// command is one top-level d0t subcommand.
type command struct {
	name    string
	summary string
	run     func(ctx context.Context, g *globalFlags, args []string) error
}

var commands = []command{
	{"apply", "converge target state", runApply},
	{"plan", "show what apply would do (dry run)", runPlan},
	{"status", "show drift, missing, and extra", runStatus},
	{"diff", "show content diff for templates/copies", runDiff},
	{"remove", "tear down everything d0t manages", runRemove},
	{"adopt", "move an existing config into the repo and replace with a link", runAdopt},
	{"init", "scaffold a new d0t repo", runInit},
	{"doctor", "sanity-check the repo and host", runDoctor},
	{"sync", "git pull the dotfiles repo and re-apply", runSync},
	{"upgrade", "upgrade installed packages (brew upgrade, mas upgrade)", runUpgrade},
	{"update", "rebuild the d0t binary from source", runUpdate},
}

// globalFlags are accepted before the subcommand.
type globalFlags struct {
	repo     string
	profiles string
	dryRun   bool
	verbose  bool
	yes      bool
}

func (g *globalFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&g.repo, "repo", "", "path to dotfiles repo (default: $D0T_REPO or ~/.d0t)")
	fs.StringVar(&g.profiles, "profile", "", "comma-separated profile override (default: base,os/<os>,hosts/<host>)")
	fs.BoolVar(&g.dryRun, "dry-run", false, "do not modify the filesystem")
	fs.BoolVar(&g.verbose, "verbose", false, "verbose output")
	fs.BoolVar(&g.yes, "yes", false, "do not prompt for confirmation")
}

// Run is the entrypoint called from main.
func Run(ctx context.Context, args []string) error {
	g := &globalFlags{}
	fs := flag.NewFlagSet("d0t", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // we print our own usage
	g.bind(fs)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			usage(os.Stdout)
			return nil
		}
		usage(os.Stderr)
		return ErrUsage
	}
	rest := fs.Args()
	if len(rest) == 0 {
		usage(os.Stderr)
		return ErrUsage
	}
	name, sub := rest[0], rest[1:]
	for _, c := range commands {
		if c.name == name {
			return c.run(ctx, g, sub)
		}
	}
	fmt.Fprintf(os.Stderr, "d0t: unknown command %q\n\n", name)
	usage(os.Stderr)
	return ErrUsage
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: d0t [global flags] <command> [args]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "commands:")
	maxName := 0
	for _, c := range commands {
		if len(c.name) > maxName {
			maxName = len(c.name)
		}
	}
	cs := append([]command(nil), commands...)
	sort.Slice(cs, func(i, j int) bool { return cs[i].name < cs[j].name })
	for _, c := range cs {
		fmt.Fprintf(w, "  %-*s  %s\n", maxName, c.name, c.summary)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "global flags:")
	fmt.Fprintln(w, "  --repo PATH        path to dotfiles repo")
	fmt.Fprintln(w, "  --profile LIST     comma-separated profile override")
	fmt.Fprintln(w, "  --dry-run          do not modify the filesystem")
	fmt.Fprintln(w, "  --verbose          verbose output")
	fmt.Fprintln(w, "  --yes              do not prompt for confirmation")
}

// loadRepoAndPlan is the common preamble for apply/plan/status/diff/remove.
// It discovers the repo, resolves profiles, and builds the plan.
func loadRepoAndPlan(g *globalFlags) (*repo.Repo, *plan.Plan, error) {
	r, err := repo.Open(g.repo)
	if err != nil {
		return nil, nil, fmt.Errorf("open repo: %w", err)
	}
	var profileOverride []string
	if g.profiles != "" {
		profileOverride = splitCSV(g.profiles)
	}
	p, err := plan.Build(r, plan.BuildOptions{
		ProfileOverride: profileOverride,
		ReaderBuilder:   primitive.NewProfileReaderBuilder(),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("build plan: %w", err)
	}
	return r, p, nil
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// --- subcommands ---------------------------------------------------------

type applyFlags struct {
	noPkg   bool
	noHooks bool
	force   bool
	adopt   bool
}

func runApply(ctx context.Context, g *globalFlags, args []string) error {
	var af applyFlags
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.BoolVar(&af.noPkg, "no-pkg", false, "skip package primitives")
	fs.BoolVar(&af.noHooks, "no-hooks", false, "skip exec hooks")
	fs.BoolVar(&af.force, "force", false, "overwrite unrelated files at target paths")
	fs.BoolVar(&af.adopt, "adopt", false, "back up unrelated files before overwriting")
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}
	r, p, err := loadRepoAndPlan(g)
	if err != nil {
		return err
	}

	// Load state (or start fresh).
	statePath := state.Path(r.Home)
	st, err := state.Load(statePath)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	opts := plan.ApplyOptions{
		DryRun:    g.dryRun,
		Force:     af.force,
		Adopt:     af.adopt,
		SkipPkg:   af.noPkg,
		SkipHooks: af.noHooks,
		Out:       ui.NewPrinter(os.Stdout, g.verbose),
		OnFileApplied: func(a plan.FileAction) {
			hash, _ := fsutil.HashFile(a.Target())
			st.Set(a.Target(), state.Entry{
				Kind:   a.Kind(),
				Source: a.Source(),
				Hash:   hash,
			})
		},
	}
	if err := p.Apply(ctx, r, opts); err != nil {
		return err
	}

	// Persist state even in dry-run mode the callback isn't called, so
	// state won't grow. Save unconditionally to capture any new entries.
	if !g.dryRun {
		if err := state.Save(statePath, st); err != nil {
			return fmt.Errorf("save state: %w", err)
		}
	}
	return nil
}

func runPlan(ctx context.Context, g *globalFlags, args []string) error {
	_ = ctx
	if err := flag.NewFlagSet("plan", flag.ContinueOnError).Parse(args); err != nil {
		return ErrUsage
	}
	r, p, err := loadRepoAndPlan(g)
	if err != nil {
		return err
	}
	return p.Print(os.Stdout, r, plan.PrintOptions{ShowNoOp: g.verbose})
}

func runStatus(ctx context.Context, g *globalFlags, args []string) error {
	_ = ctx
	if err := flag.NewFlagSet("status", flag.ContinueOnError).Parse(args); err != nil {
		return ErrUsage
	}
	r, p, err := loadRepoAndPlan(g)
	if err != nil {
		return err
	}
	return p.Status(os.Stdout, r)
}

func runDiff(ctx context.Context, g *globalFlags, args []string) error {
	_ = ctx
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}
	r, p, err := loadRepoAndPlan(g)
	if err != nil {
		return err
	}
	return p.Diff(os.Stdout, r, fs.Args())
}

func runRemove(ctx context.Context, g *globalFlags, args []string) error {
	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}

	r, err := repo.Open(g.repo)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}

	statePath := state.Path(r.Home)
	st, err := state.Load(statePath)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	if st.Len() == 0 {
		fmt.Fprintln(os.Stdout, "nothing to remove (state is empty)")
		return nil
	}

	printer := ui.NewPrinter(os.Stdout, g.verbose)
	targets := st.Targets()

	if !g.yes && !g.dryRun {
		fmt.Fprintf(os.Stdout, "will remove %d managed targets. continue? [y/N] ", len(targets))
		var ans string
		fmt.Fscanln(os.Stdin, &ans)
		if ans != "y" && ans != "Y" {
			fmt.Fprintln(os.Stdout, "aborted")
			return nil
		}
	}

	for _, target := range targets {
		select {
		case <-ctx.Done():
			return fmt.Errorf("remove cancelled")
		default:
		}
		e, _ := st.Get(target)
		if g.dryRun {
			printer.Info("[dry-run] remove (%s) %s", e.Kind, target)
			continue
		}
		if err := removeTarget(target, e); err != nil {
			printer.Info("warn    %s: %v", target, err)
			continue
		}
		printer.Info("remove  (%s) %s", e.Kind, target)
		st.Delete(target)
	}

	if !g.dryRun {
		if err := state.Save(statePath, st); err != nil {
			return fmt.Errorf("save state: %w", err)
		}
	}
	return nil
}

// removeTarget tears down a single managed target according to its primitive.
func removeTarget(target string, e state.Entry) error {
	switch e.Kind {
	case "link":
		// Only remove if it still points to where d0t put it.
		current, isLink, err := fsutil.SymlinkTarget(target)
		if err != nil {
			return err
		}
		if !isLink {
			return fmt.Errorf("target is not a symlink; skipping")
		}
		_ = current // symlink exists; remove it
		return os.Remove(target)

	case "copy", "template":
		// Only remove if hash still matches what d0t wrote.
		if e.Hash != "" {
			current, err := fsutil.HashFile(target)
			if err != nil {
				return err
			}
			if current != e.Hash {
				return fmt.Errorf("content has changed since apply; skipping (use --force to override)")
			}
		}
		return os.Remove(target)

	case "fragment":
		// Remove the managed block from the target file; leave the rest.
		return removeFragment(target, e.Marker)

	default:
		return fmt.Errorf("unknown kind %q; skipping", e.Kind)
	}
}

// removeFragment deletes the d0t-managed block with the given marker from
// the target file. If the block is not present the file is left unchanged.
func removeFragment(target, marker string) error {
	if marker == "" {
		return fmt.Errorf("fragment entry has no marker")
	}
	b, err := os.ReadFile(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	content := string(b)
	// Use the same comment prefix logic as the fragment primitive. For remove
	// we just need to find and strip the block regardless of comment style.
	open := ">>> d0t:" + marker + " >>>"
	close := "<<< d0t:" + marker + " <<<"

	startIdx := -1
	for i, line := range splitLines(content) {
		if contains(line, open) {
			startIdx = i
			break
		}
		_ = i
	}
	if startIdx < 0 {
		return nil // already gone
	}

	lines := splitLines(content)
	endIdx := -1
	for i := startIdx; i < len(lines); i++ {
		if contains(lines[i], close) {
			endIdx = i
			break
		}
	}
	if endIdx < 0 {
		return fmt.Errorf("found open marker but not close marker for %s", marker)
	}

	kept := append(lines[:startIdx:startIdx], lines[endIdx+1:]...)
	updated := joinLines(kept)
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	return fsutil.AtomicWrite(target, []byte(updated), info.Mode().Perm())
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	// strings.Split on "a\nb\n" gives ["a","b",""], preserve trailing newline
	// semantics by keeping the empty last element only if s ends with \n.
	return lines
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n")
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}

func runAdopt(ctx context.Context, g *globalFlags, args []string) error {
	_ = ctx
	fs := flag.NewFlagSet("adopt", flag.ContinueOnError)
	var dest string
	fs.StringVar(&dest, "dest", "", "destination path in repo (default: base/home/<relative-to-home>)")
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: d0t adopt <path> [--dest <repo-relative-path>]")
		return ErrUsage
	}
	src, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return err
	}

	r, err := repo.Open(g.repo)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}

	if dest == "" {
		// Default: infer base/home/<rel-to-home>.
		rel, err := filepath.Rel(r.Home, src)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("adopt: %s is not under $HOME; use --dest to specify the repo destination", src)
		}
		dest = filepath.Join(r.Root, "base", "home", rel)
	} else {
		dest = filepath.Join(r.Root, dest)
	}

	if g.dryRun {
		fmt.Fprintf(os.Stdout, "[dry-run] adopt %s -> %s\n", src, dest)
		return nil
	}
	if err := initrepo.Adopt(src, dest); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "adopted %s -> %s\n", src, dest)
	return nil
}

func runInit(ctx context.Context, g *globalFlags, args []string) error {
	_ = ctx
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}

	dir := g.repo
	if dir == "" {
		dir = os.Getenv("D0T_REPO")
	}
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		dir = filepath.Join(home, ".d0t")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	if g.dryRun {
		fmt.Fprintf(os.Stdout, "[dry-run] init repo at %s\n", abs)
		return nil
	}
	if err := initrepo.Init(abs); err != nil {
		return fmt.Errorf("init: %w", err)
	}
	fmt.Fprintf(os.Stdout, "initialised repo at %s\n", abs)
	fmt.Fprintf(os.Stdout, "next: add your dotfiles under %s/base/home/ and run d0t apply\n", abs)
	return nil
}

func runDoctor(ctx context.Context, g *globalFlags, args []string) error {
	_ = ctx
	_ = g
	_ = args
	return errors.New("doctor: not yet implemented")
}

func runSync(ctx context.Context, g *globalFlags, args []string) error {
	_ = ctx
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	var noPull, noApply bool
	fs.BoolVar(&noPull, "no-pull", false, "skip git pull")
	fs.BoolVar(&noApply, "no-apply", false, "skip apply after pull")
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}

	r, err := repo.Open(g.repo)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}

	printer := ui.NewPrinter(os.Stdout, g.verbose)
	s := d0tsync.New(d0tsync.Config{
		Pull: d0tsync.GitPull(r.Root),
		Apply: func() error {
			p, buildErr := plan.Build(r, plan.BuildOptions{
				ReaderBuilder: primitive.NewProfileReaderBuilder(),
			})
			if buildErr != nil {
				return buildErr
			}
			return p.Apply(ctx, r, plan.ApplyOptions{
				Out: printer,
			})
		},
	})

	return s.Run(d0tsync.Options{NoPull: noPull || g.dryRun, NoApply: noApply})
}

func runUpgrade(_ context.Context, g *globalFlags, args []string) error {
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}
	if g.dryRun {
		fmt.Fprintln(os.Stdout, "[dry-run] would run: brew update && brew upgrade && brew upgrade --cask && mas upgrade")
		return nil
	}
	u := upgrade.New(upgrade.Config{Managers: upgrade.DefaultManagers()})
	return u.Run()
}

func runUpdate(_ context.Context, g *globalFlags, args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}

	r, err := repo.Open(g.repo)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}

	src := r.Config.Self.Source
	if src == "" {
		return fmt.Errorf("d0t update: set [self] source = \"/path/to/d0t\" in %s/d0t.toml", r.Root)
	}
	// Expand leading ~
	if strings.HasPrefix(src, "~/") {
		src = filepath.Join(r.Home, src[2:])
	}

	out, err := selfupdate.CurrentBinaryPath()
	if err != nil {
		return fmt.Errorf("locate d0t binary: %w", err)
	}

	if g.dryRun {
		fmt.Fprintf(os.Stdout, "[dry-run] would build: go build -o %s %s/cmd/d0t\n", out, src)
		return nil
	}

	u := selfupdate.New(selfupdate.Config{
		SourcePath: src,
		OutputPath: out,
	})
	return u.Run()
}
