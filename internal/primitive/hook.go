package primitive

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/callmeradical/d0t/internal/plan"
)

// knownPhases lists the recognized hook phase prefixes in execution order.
var knownPhases = []string{"pre-apply", "post-apply", "pre-remove", "post-remove"}

// ClassifyHook determines the lifecycle phase for a hook filename. It returns
// the phase string and true if the file is a recognized hook, or ("", false)
// otherwise. Only executable file names are meaningful to the caller; the
// function does not stat the file.
func ClassifyHook(name string) (phase string, ok bool) {
	for _, p := range knownPhases {
		if name == p+".sh" || strings.HasPrefix(name, p+"-") || strings.HasPrefix(name, p+".") {
			return p, true
		}
	}
	return "", false
}

// Hook is a single lifecycle hook script.
type Hook struct {
	// Name is the basename of the script file.
	Name string
	// AbsPath is the absolute path to the script file.
	AbsPath string
	// ProfileName is the profile this hook belongs to (for env vars + display).
	ProfileName string
	// Phase is one of "pre-apply", "post-apply", "pre-remove", "post-remove".
	Phase string
	// Optional, when true, means a non-zero exit is logged but not fatal.
	Optional bool
}

// NewHook constructs a Hook. The Optional flag is detected from the filename.
func NewHook(name, absPath, profileName, phase string) Hook {
	return Hook{
		Name:        name,
		AbsPath:     absPath,
		ProfileName: profileName,
		Phase:       phase,
		Optional:    strings.Contains(name, ".optional."),
	}
}

// Kind implements plan.Action.
func (h Hook) Kind() string { return "exec" }

// Describe implements plan.Action.
func (h Hook) Describe() string {
	opt := ""
	if h.Optional {
		opt = " (optional)"
	}
	return fmt.Sprintf("exec [%s] %s%s", h.Phase, h.Name, opt)
}

// Plan implements plan.Action. Hooks always report OpCreate because they
// are always executed on apply — their idempotency is the script's
// responsibility.
func (h Hook) Plan(_ *plan.Context) (plan.Change, error) {
	return plan.Change{Op: plan.OpCreate, Detail: "will execute"}, nil
}

// Apply executes the hook script.
func (h Hook) Apply(ctx *plan.Context) error {
	if ctx.DryRun {
		ctx.Out.Info("[dry-run] exec [%s] %s", h.Phase, h.Name)
		return nil
	}

	cmd := exec.CommandContext(ctx.Ctx, h.AbsPath)
	cmd.Dir = filepath.Dir(h.AbsPath) // cwd = profile dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), h.envVars(ctx)...)

	ctx.Out.Info("exec    [%s] %s", h.Phase, h.Name)
	if err := cmd.Run(); err != nil {
		if h.Optional {
			ctx.Out.Info("warn    %s: %v (optional, continuing)", h.Name, err)
			return nil
		}
		return fmt.Errorf("hook %s: %w", h.Name, err)
	}
	return nil
}

func (h Hook) envVars(ctx *plan.Context) []string {
	dryRun := "false"
	if ctx.DryRun {
		dryRun = "true"
	}
	env := []string{
		"D0T_PROFILE=" + h.ProfileName,
		"D0T_DRY_RUN=" + dryRun,
	}
	if ctx.Repo != nil {
		env = append(env,
			"D0T_REPO="+ctx.Repo.Root,
			"D0T_HOST="+ctx.Repo.Hostname,
			"D0T_OS="+ctx.Repo.OS,
		)
	}
	return env
}

// WalkHooks enumerates all hook scripts inside a profile's hooks/ directory,
// calling visit for each recognized hook in lexical order within each phase.
// Missing hooks/ directories are silently skipped.
func WalkHooks(profilePath, profileName string, visit func(Hook) error) error {
	hooksDir := filepath.Join(profilePath, "hooks")
	entries, err := os.ReadDir(hooksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	// Collect and sort hooks to ensure deterministic execution order.
	var hooks []Hook
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		phase, ok := ClassifyHook(e.Name())
		if !ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		// Only consider executable files.
		if info.Mode()&fs.ModePerm&0o111 == 0 {
			continue
		}
		hooks = append(hooks, NewHook(
			e.Name(),
			filepath.Join(hooksDir, e.Name()),
			profileName,
			phase,
		))
	}

	// Sort by phase order first, then lexically within phase.
	phaseIdx := func(p string) int {
		for i, ph := range knownPhases {
			if ph == p {
				return i
			}
		}
		return 99
	}
	sort.Slice(hooks, func(i, j int) bool {
		pi, pj := phaseIdx(hooks[i].Phase), phaseIdx(hooks[j].Phase)
		if pi != pj {
			return pi < pj
		}
		return hooks[i].Name < hooks[j].Name
	})

	for _, h := range hooks {
		if err := visit(h); err != nil {
			return err
		}
	}
	return nil
}
