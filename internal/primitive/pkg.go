package primitive

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"d0t/internal/plan"
)

// ---------------------------------------------------------------------------
// Manifest types
// ---------------------------------------------------------------------------

// PkgManifest is the parsed content of a packages.toml file.
type PkgManifest struct {
	Brew BrewManifest `toml:"brew"`
	Apt  AptManifest  `toml:"apt"`
	Mas  MasManifest  `toml:"mas"`
}

// BrewManifest lists Homebrew packages.
type BrewManifest struct {
	Formulae []string `toml:"formulae"`
	Casks    []string `toml:"casks"`
	Taps     []string `toml:"taps"`
}

// AptManifest lists apt packages.
type AptManifest struct {
	Packages []string `toml:"packages"`
}

// MasManifest lists Mac App Store apps.
type MasManifest struct {
	Apps []MasApp `toml:"apps"`
}

// MasApp is a single Mac App Store app.
type MasApp struct {
	ID   int    `toml:"id"`
	Name string `toml:"name"`
}



// MergeManifests merges multiple PkgManifests into one, deduplicating string
// slices by value and accumulating MAS apps by ID. Later entries do not
// override earlier ones for string slices — all unique values are kept.
func MergeManifests(manifests []*PkgManifest) *PkgManifest {
	out := &PkgManifest{}
	seenFormulae := map[string]bool{}
	seenCasks := map[string]bool{}
	seenTaps := map[string]bool{}
	seenApt := map[string]bool{}
	seenMas := map[int]bool{}

	for _, m := range manifests {
		if m == nil {
			continue
		}
		for _, f := range m.Brew.Formulae {
			if !seenFormulae[f] {
				seenFormulae[f] = true
				out.Brew.Formulae = append(out.Brew.Formulae, f)
			}
		}
		for _, c := range m.Brew.Casks {
			if !seenCasks[c] {
				seenCasks[c] = true
				out.Brew.Casks = append(out.Brew.Casks, c)
			}
		}
		for _, t := range m.Brew.Taps {
			if !seenTaps[t] {
				seenTaps[t] = true
				out.Brew.Taps = append(out.Brew.Taps, t)
			}
		}
		for _, p := range m.Apt.Packages {
			if !seenApt[p] {
				seenApt[p] = true
				out.Apt.Packages = append(out.Apt.Packages, p)
			}
		}
		for _, a := range m.Mas.Apps {
			if !seenMas[a.ID] {
				seenMas[a.ID] = true
				out.Mas.Apps = append(out.Mas.Apps, a)
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Package actions
// ---------------------------------------------------------------------------

// PkgAction handles installing packages for a single manager (brew, apt, mas).
// Each manager is its own action so they can be planned/displayed independently.
type PkgAction struct {
	manager  string
	packages []string // formulae, cask names, apt packages, etc.
	detail   string   // human-readable summary for display
	apply    func(ctx *plan.Context, packages []string) error
}

// Kind implements plan.Action.
func (p *PkgAction) Kind() string { return "pkg" }

// Describe implements plan.Action.
func (p *PkgAction) Describe() string {
	return fmt.Sprintf("pkg [%s] %s", p.manager, p.detail)
}

// Plan implements plan.Action. Package actions are always OpCreate because
// the check for already-installed happens inside Apply (manager is the source
// of truth, not d0t state).
func (p *PkgAction) Plan(_ *plan.Context) (plan.Change, error) {
	return plan.Change{Op: plan.OpCreate, Detail: p.detail}, nil
}

// Apply implements plan.Action.
func (p *PkgAction) Apply(ctx *plan.Context) error {
	if ctx.DryRun {
		ctx.Out.Info("[dry-run] pkg [%s] %s", p.manager, p.detail)
		return nil
	}
	return p.apply(ctx, p.packages)
}

// ActionsFromManifest converts a merged PkgManifest into a slice of PkgActions.
// Managers not present on the host (no executable in PATH) are returned as
// skip actions with detail "manager not found".
func ActionsFromManifest(m *PkgManifest) []*PkgAction {
	if m == nil {
		return nil
	}
	var out []*PkgAction

	// ---- Homebrew taps -------------------------------------------------
	if len(m.Brew.Taps) > 0 {
		taps := m.Brew.Taps
		out = append(out, &PkgAction{
			manager:  "brew-tap",
			packages: taps,
			detail:   strings.Join(taps, " "),
			apply:    brewTapApply,
		})
	}
	// ---- Homebrew formulae ---------------------------------------------
	if len(m.Brew.Formulae) > 0 {
		fs := m.Brew.Formulae
		out = append(out, &PkgAction{
			manager:  "brew",
			packages: fs,
			detail:   strings.Join(fs, " "),
			apply:    brewInstallApply,
		})
	}
	// ---- Homebrew casks ------------------------------------------------
	if len(m.Brew.Casks) > 0 {
		cs := m.Brew.Casks
		out = append(out, &PkgAction{
			manager:  "brew-cask",
			packages: cs,
			detail:   strings.Join(cs, " "),
			apply:    brewCaskApply,
		})
	}
	// ---- apt -----------------------------------------------------------
	if len(m.Apt.Packages) > 0 {
		ps := m.Apt.Packages
		out = append(out, &PkgAction{
			manager:  "apt",
			packages: ps,
			detail:   strings.Join(ps, " "),
			apply:    aptInstallApply,
		})
	}
	// ---- mas -----------------------------------------------------------
	if len(m.Mas.Apps) > 0 {
		ids := make([]string, len(m.Mas.Apps))
		names := make([]string, len(m.Mas.Apps))
		for i, a := range m.Mas.Apps {
			ids[i] = fmt.Sprintf("%d", a.ID)
			names[i] = a.Name
		}
		out = append(out, &PkgAction{
			manager:  "mas",
			packages: ids,
			detail:   strings.Join(names, " "),
			apply:    masInstallApply,
		})
	}
	return out
}



// ---------------------------------------------------------------------------
// Manager apply implementations
// ---------------------------------------------------------------------------

func managerAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func brewTapApply(ctx *plan.Context, taps []string) error {
	if !managerAvailable("brew") {
		ctx.Out.Info("warn    brew not found; skipping taps")
		return nil
	}
	for _, tap := range taps {
		ctx.Out.Info("brew tap %s", tap)
		if err := run(ctx, "brew", "tap", tap); err != nil {
			return fmt.Errorf("brew tap %s: %w", tap, err)
		}
	}
	return nil
}

func brewInstallApply(ctx *plan.Context, formulae []string) error {
	if !managerAvailable("brew") {
		ctx.Out.Info("warn    brew not found; skipping formulae")
		return nil
	}
	ctx.Out.Info("brew install %s", strings.Join(formulae, " "))
	return run(ctx, append([]string{"brew", "install"}, formulae...)...)
}

func brewCaskApply(ctx *plan.Context, casks []string) error {
	if !managerAvailable("brew") {
		ctx.Out.Info("warn    brew not found; skipping casks")
		return nil
	}
	ctx.Out.Info("brew install --cask %s", strings.Join(casks, " "))
	return run(ctx, append([]string{"brew", "install", "--cask"}, casks...)...)
}

func aptInstallApply(ctx *plan.Context, packages []string) error {
	if !managerAvailable("apt-get") {
		ctx.Out.Info("warn    apt-get not found; skipping")
		return nil
	}
	ctx.Out.Info("apt-get install -y %s", strings.Join(packages, " "))
	return run(ctx, append([]string{"apt-get", "install", "-y"}, packages...)...)
}

func masInstallApply(ctx *plan.Context, ids []string) error {
	if !managerAvailable("mas") {
		ctx.Out.Info("warn    mas not found; skipping")
		return nil
	}
	for _, id := range ids {
		ctx.Out.Info("mas install %s", id)
		if err := run(ctx, "mas", "install", id); err != nil {
			return fmt.Errorf("mas install %s: %w", id, err)
		}
	}
	return nil
}

func run(ctx *plan.Context, args ...string) error {
	cmd := exec.CommandContext(ctx.Ctx, args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
