// Package primitive implements the d0t primitives: link, copy, template,
// fragment, exec, pkg. Each primitive provides a constructor that returns
// a value satisfying plan.Action (and plan.FileAction where applicable).
package primitive

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"d0t/internal/fsutil"
	"d0t/internal/plan"
)

// Link is the default file primitive: it creates a symlink at Target
// pointing to Source.
type Link struct {
	source string // absolute path in repo
	target string // absolute path on filesystem
	relSrc string // repo-relative source for display
}

// NewLink constructs a Link action. source and target must be absolute.
func NewLink(source, target, relSource string) *Link {
	return &Link{source: source, target: target, relSrc: relSource}
}

// Kind implements plan.Action.
func (l *Link) Kind() string { return "link" }

// Target implements plan.FileAction.
func (l *Link) Target() string { return l.target }

// Source implements plan.FileAction.
func (l *Link) Source() string { return l.relSrc }

// Describe implements plan.Action.
func (l *Link) Describe() string {
	return fmt.Sprintf("link %s -> %s", l.target, l.relSrc)
}

// Plan implements plan.Action.
func (l *Link) Plan(_ *plan.Context) (plan.Change, error) {
	current, isLink, err := fsutil.SymlinkTarget(l.target)
	if err != nil {
		return plan.Change{}, err
	}
	if !isLink {
		// Path may be absent or a regular file/dir.
		if _, err := os.Lstat(l.target); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return plan.Change{Op: plan.OpCreate}, nil
			}
			return plan.Change{}, err
		}
		return plan.Change{
			Op:     plan.OpConflict,
			Detail: "target exists and is not a d0t-managed symlink",
		}, nil
	}
	if current == l.source {
		return plan.Change{Op: plan.OpNoOp}, nil
	}
	return plan.Change{
		Op:     plan.OpUpdate,
		Detail: fmt.Sprintf("symlink points to %s", current),
	}, nil
}

// Apply implements plan.Action.
func (l *Link) Apply(ctx *plan.Context) error {
	change, err := l.Plan(ctx)
	if err != nil {
		return err
	}
	switch change.Op {
	case plan.OpNoOp:
		ctx.Out.Verbose("ok      link %s", l.target)
		return nil
	case plan.OpConflict:
		if !ctx.Force && !ctx.Adopt {
			return fmt.Errorf("link %s: %s (use --adopt to back up, --force to replace)", l.target, change.Detail)
		}
		if err := l.handleConflict(ctx); err != nil {
			return err
		}
	case plan.OpUpdate:
		if !ctx.DryRun {
			if err := os.Remove(l.target); err != nil {
				return fmt.Errorf("remove stale symlink %s: %w", l.target, err)
			}
		}
	}
	if ctx.DryRun {
		ctx.Out.Info("[dry-run] %s %s", change.Op, l.Describe())
		return nil
	}
	if err := fsutil.EnsureParentDir(l.target); err != nil {
		return fmt.Errorf("create parent of %s: %w", l.target, err)
	}
	if err := os.Symlink(l.source, l.target); err != nil {
		return fmt.Errorf("symlink %s -> %s: %w", l.target, l.source, err)
	}
	displayOp := change.Op
	if change.Op == plan.OpConflict {
		displayOp = plan.OpUpdate
	}
	ctx.Out.Info("%-7s %s", displayOp, l.Describe())
	return nil
}

func (l *Link) handleConflict(ctx *plan.Context) error {
	if ctx.DryRun {
		return nil
	}
	if ctx.Adopt {
		backup := fsutil.BackupPath(l.target)
		if err := os.Rename(l.target, backup); err != nil {
			return fmt.Errorf("backup %s: %w", l.target, err)
		}
		ctx.Out.Info("backup  %s -> %s", l.target, backup)
		return nil
	}
	// Force: remove whatever is there. Refuse to recursively delete dirs.
	info, err := os.Lstat(l.target)
	if err != nil {
		return err
	}
	if info.IsDir() && info.Mode()&fs.ModeSymlink == 0 {
		return fmt.Errorf("refusing to overwrite directory %s with --force; use --adopt", l.target)
	}
	return os.Remove(l.target)
}
