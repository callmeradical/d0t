package plan

import (
	"context"

	"d0t/internal/repo"
	"d0t/internal/ui"
)

// Action is anything the plan can do: link a file, render a template, run a
// hook, install a package. Actions are pure data until Apply is invoked.
type Action interface {
	// Kind is the primitive name: "link", "copy", "template", "fragment",
	// "exec", "pkg".
	Kind() string
	// Describe returns a one-line human-readable summary.
	Describe() string
	// Plan inspects the live filesystem and reports what Apply would change.
	// It must not mutate state.
	Plan(ctx *Context) (Change, error)
	// Apply executes the change. It must be idempotent: re-running on a
	// converged target is a no-op.
	Apply(ctx *Context) error
}

// FileAction is an Action that produces or owns a file at a known absolute
// path. The Target is the dedup key during profile-layer merging: when two
// profiles produce a FileAction for the same target, the later profile wins.
type FileAction interface {
	Action
	// Target is the absolute path of the file the action manages.
	Target() string
	// Source is the repo-relative path of the source file (for display
	// and for diffs). It may be empty for synthesized actions.
	Source() string
}

// ChangeOp classifies a planned change.
type ChangeOp int

const (
	// OpNoOp means the live filesystem already matches the desired state.
	OpNoOp ChangeOp = iota
	// OpCreate means a new file/link/etc. will be created.
	OpCreate
	// OpUpdate means an existing managed target will be updated.
	OpUpdate
	// OpConflict means the target exists but is not what d0t expects;
	// Apply will fail without --force or --adopt.
	OpConflict
	// OpSkip means the action is skipped (e.g. pkg manager not present
	// without strict_pkg).
	OpSkip
)

func (o ChangeOp) String() string {
	switch o {
	case OpNoOp:
		return "ok"
	case OpCreate:
		return "create"
	case OpUpdate:
		return "update"
	case OpConflict:
		return "conflict"
	case OpSkip:
		return "skip"
	default:
		return "?"
	}
}

// Change is the outcome of Action.Plan.
type Change struct {
	// Op is the kind of change.
	Op ChangeOp
	// Detail is an optional human-readable explanation.
	Detail string
	// Diff is an optional unified diff (for templates/copies).
	Diff string
}

// Context is shared state passed to every Action during Plan and Apply.
type Context struct {
	// Ctx is the cancellation context for long-running operations.
	Ctx context.Context
	// Repo is the opened d0t repository.
	Repo *repo.Repo
	// DryRun, when true, makes Apply behave like Plan.
	DryRun bool
	// Force, when true, allows overwriting unrelated files at target paths.
	Force bool
	// Adopt, when true, backs up unrelated files before overwriting.
	Adopt bool
	// Verbose toggles extra output.
	Verbose bool
	// Out is the user-facing printer.
	Out ui.Printer
}
