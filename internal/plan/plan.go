package plan

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/callmeradical/d0t/internal/profile"
	"github.com/callmeradical/d0t/internal/repo"
	"github.com/callmeradical/d0t/internal/ui"
)

// ProfileReader reads one profile's d0tfile and returns the actions it
// declares. It is called once per active profile in resolution order.
// The roots map (root name → absolute path) is provided for source-to-target
// resolution. Returning (nil, nil, nil, nil) means the profile has no
// d0tfile and should be silently skipped.
type ProfileReader func(profilePath, profileName string, roots map[string]string) (files []FileAction, hooks []Action, pkgs []Action, err error)

// ProfileReaderBuilder constructs a ProfileReader after the full profile list
// is known. Receiving all profiles lets the builder pre-load merged vars
// (which span the whole profile stack) before reading any individual manifest.
type ProfileReaderBuilder func(profs []profile.Profile, r *repo.Repo) (ProfileReader, error)

// BuildOptions modulates Build.
type BuildOptions struct {
	// ProfileOverride, if non-nil, replaces the default profile resolution.
	ProfileOverride []string
	// ReaderBuilder is required. It builds the per-profile manifest reader.
	ReaderBuilder ProfileReaderBuilder
}

// Plan is the resolved set of actions for a host, ready to be applied.
type Plan struct {
	// Profiles is the resolved profile order, for display.
	Profiles []string
	// Files is the deduped, target-sorted list of file actions.
	Files []FileAction
	// Hooks is the full list of lifecycle hooks in execution order,
	// accumulated across all profiles.
	Hooks []Action
	// PkgActions is the list of package manager actions.
	PkgActions []Action
}

// Build resolves profiles, reads each profile's d0tfile, and produces a Plan.
// Profiles without a d0tfile are silently skipped.
// For any given target path, the last profile that declares it wins.
// Hooks and package actions are accumulated across all profiles.
func Build(r *repo.Repo, opts BuildOptions) (*Plan, error) {
	if opts.ReaderBuilder == nil {
		return nil, fmt.Errorf("plan.Build: ReaderBuilder is required")
	}

	profs, err := profile.Resolve(r, opts.ProfileOverride)
	if err != nil {
		return nil, err
	}
	roots, err := profile.ResolveRoots(r)
	if err != nil {
		return nil, err
	}

	reader, err := opts.ReaderBuilder(profs, r)
	if err != nil {
		return nil, fmt.Errorf("build reader: %w", err)
	}

	p := &Plan{Profiles: profileNames(profs)}
	fileMap := map[string]FileAction{}
	// Accumulate pkg declarations across profiles before deduping into actions.
	var allPkgActions []Action

	for _, prof := range profs {
		files, hooks, pkgs, err := reader(prof.Path, prof.Name, roots)
		if err != nil {
			return nil, fmt.Errorf("read manifest in %s: %w", prof.Name, err)
		}
		// nil result means no d0tfile — skip this profile.
		if files == nil && hooks == nil && pkgs == nil {
			continue
		}
		for _, fa := range files {
			fileMap[fa.Target()] = fa
		}
		p.Hooks = append(p.Hooks, hooks...)
		allPkgActions = append(allPkgActions, pkgs...)
	}

	// Deduplicate pkg actions by manager+item.
	p.PkgActions = dedupPkgActions(allPkgActions)

	targets := make([]string, 0, len(fileMap))
	for t := range fileMap {
		targets = append(targets, t)
	}
	sort.Strings(targets)
	for _, t := range targets {
		p.Files = append(p.Files, fileMap[t])
	}
	return p, nil
}

func profileNames(ps []profile.Profile) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name
	}
	return out
}

// dedupPkgActions removes duplicate package actions keeping the first
// occurrence (profile order: base first, host last).
func dedupPkgActions(actions []Action) []Action {
	seen := map[string]bool{}
	var out []Action
	for _, a := range actions {
		key := a.Kind() + ":" + a.Describe()
		if !seen[key] {
			seen[key] = true
			out = append(out, a)
		}
	}
	return out
}

// ApplyOptions configures Apply.
type ApplyOptions struct {
	DryRun    bool
	Force     bool
	Adopt     bool
	SkipPkg   bool
	SkipHooks bool
	Out       ui.Printer
	// OnFileApplied, if non-nil, is called after each FileAction succeeds
	// (not called in dry-run mode).
	OnFileApplied func(a FileAction)
}

// Apply executes actions in lifecycle order:
//  1. pre-apply hooks
//  2. file actions (link / copy / template / fragment)
//  3. package installs
//  4. post-apply hooks
func (p *Plan) Apply(ctx context.Context, r *repo.Repo, opts ApplyOptions) error {
	actCtx := &Context{
		Ctx:   ctx,
		Repo:  r,
		DryRun: opts.DryRun,
		Force:  opts.Force,
		Adopt:  opts.Adopt,
		Out:    opts.Out,
	}
	if actCtx.Out == nil {
		actCtx.Out = ui.NewPrinter(io.Discard, false)
	}

	runActions := func(actions []Action) error {
		for _, a := range actions {
			select {
			case <-ctx.Done():
				return fmt.Errorf("apply cancelled")
			default:
			}
			if err := a.Apply(actCtx); err != nil {
				return err
			}
		}
		return nil
	}

	if !opts.SkipHooks {
		if err := runActions(hooksForPhase(p.Hooks, "pre-apply")); err != nil {
			return err
		}
	}

	for _, fa := range p.Files {
		select {
		case <-ctx.Done():
			return fmt.Errorf("apply cancelled")
		default:
		}
		if err := fa.Apply(actCtx); err != nil {
			return err
		}
		if opts.OnFileApplied != nil && !opts.DryRun {
			opts.OnFileApplied(fa)
		}
	}

	if !opts.SkipPkg {
		if err := runActions(p.PkgActions); err != nil {
			return err
		}
	}

	if !opts.SkipHooks {
		if err := runActions(hooksForPhase(p.Hooks, "post-apply")); err != nil {
			return err
		}
	}

	return nil
}

func hooksForPhase(hooks []Action, phase string) []Action {
	var out []Action
	for _, h := range hooks {
		if strings.Contains(h.Describe(), "["+phase+"]") {
			out = append(out, h)
		}
	}
	return out
}

// PrintOptions configures Print.
type PrintOptions struct {
	ShowNoOp bool
}

// Print writes a human-readable plan summary to w.
func (p *Plan) Print(w io.Writer, r *repo.Repo, opts PrintOptions) error {
	fmt.Fprintf(w, "repo:     %s\n", r.Root)
	fmt.Fprintf(w, "host:     %s (%s/%s)\n", r.Hostname, r.OS, r.Arch)
	fmt.Fprintf(w, "profiles: %s\n", strings.Join(p.Profiles, " -> "))
	fmt.Fprintln(w)

	pctx := &Context{Repo: r, Out: ui.NewPrinter(io.Discard, false)}
	counts := map[ChangeOp]int{}

	allActions := make([]Action, 0, len(p.Files)+len(p.Hooks)+len(p.PkgActions))
	for _, f := range p.Files {
		allActions = append(allActions, f)
	}
	allActions = append(allActions, p.Hooks...)
	allActions = append(allActions, p.PkgActions...)

	for _, a := range allActions {
		change, err := a.Plan(pctx)
		if err != nil {
			fmt.Fprintf(w, "  ERROR    %s: %v\n", a.Describe(), err)
			counts[OpConflict]++
			continue
		}
		counts[change.Op]++
		if change.Op == OpNoOp && !opts.ShowNoOp {
			continue
		}
		line := fmt.Sprintf("  %-8s %s", change.Op, a.Describe())
		if change.Detail != "" {
			line += "  (" + change.Detail + ")"
		}
		fmt.Fprintln(w, line)
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "summary: %d create, %d update, %d ok, %d conflict\n",
		counts[OpCreate], counts[OpUpdate], counts[OpNoOp], counts[OpConflict])
	return nil
}

// Status is Print with noop suppression.
func (p *Plan) Status(w io.Writer, r *repo.Repo) error {
	return p.Print(w, r, PrintOptions{ShowNoOp: false})
}

// Diff prints content diffs for file actions matching the given target paths.
func (p *Plan) Diff(w io.Writer, r *repo.Repo, filter []string) error {
	pctx := &Context{Repo: r, Out: ui.NewPrinter(io.Discard, false)}
	want := map[string]bool{}
	for _, f := range filter {
		want[f] = true
	}
	for _, a := range p.Files {
		if len(want) > 0 && !want[a.Target()] {
			continue
		}
		change, err := a.Plan(pctx)
		if err != nil {
			fmt.Fprintf(w, "ERROR  %s: %v\n", a.Describe(), err)
			continue
		}
		if change.Diff == "" {
			continue
		}
		fmt.Fprintf(w, "--- %s (current)\n+++ %s (desired)\n", a.Target(), a.Target())
		fmt.Fprintln(w, change.Diff)
	}
	return nil
}
