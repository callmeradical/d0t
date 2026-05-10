package primitive

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"d0t/internal/fsutil"
	"d0t/internal/plan"
)

// Copy renders bytes from Source into Target. Use when the file cannot be a
// symlink (apps that rewrite their own config; restricted-permission files
// like ~/.ssh/config).
type Copy struct {
	source string
	target string
	relSrc string
	mode   fs.FileMode
}

// NewCopy constructs a Copy action. mode is the source file's mode and is
// preserved on the target.
func NewCopy(source, target, relSource string, mode fs.FileMode) *Copy {
	return &Copy{source: source, target: target, relSrc: relSource, mode: mode}
}

// Kind implements plan.Action.
func (c *Copy) Kind() string { return "copy" }

// Target implements plan.FileAction.
func (c *Copy) Target() string { return c.target }

// Source implements plan.FileAction.
func (c *Copy) Source() string { return c.relSrc }

// Describe implements plan.Action.
func (c *Copy) Describe() string {
	return fmt.Sprintf("copy %s <- %s", c.target, c.relSrc)
}

// Plan implements plan.Action.
func (c *Copy) Plan(_ *plan.Context) (plan.Change, error) {
	srcHash, err := fsutil.HashFile(c.source)
	if err != nil {
		return plan.Change{}, fmt.Errorf("hash source %s: %w", c.source, err)
	}
	info, err := os.Lstat(c.target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return plan.Change{Op: plan.OpCreate}, nil
		}
		return plan.Change{}, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return plan.Change{
			Op:     plan.OpConflict,
			Detail: "target is a symlink; copy refuses to replace symlinks without --force",
		}, nil
	}
	if info.IsDir() {
		return plan.Change{Op: plan.OpConflict, Detail: "target is a directory"}, nil
	}
	dstHash, err := fsutil.HashFile(c.target)
	if err != nil {
		return plan.Change{}, fmt.Errorf("hash target %s: %w", c.target, err)
	}
	if dstHash == srcHash && info.Mode().Perm() == c.mode.Perm() {
		return plan.Change{Op: plan.OpNoOp}, nil
	}
	return plan.Change{Op: plan.OpUpdate}, nil
}

// Apply implements plan.Action.
func (c *Copy) Apply(ctx *plan.Context) error {
	change, err := c.Plan(ctx)
	if err != nil {
		return err
	}
	switch change.Op {
	case plan.OpNoOp:
		ctx.Out.Verbose("ok      copy %s", c.target)
		return nil
	case plan.OpConflict:
		if !ctx.Force && !ctx.Adopt {
			return fmt.Errorf("copy %s: %s (use --adopt to back up, --force to replace)", c.target, change.Detail)
		}
		if !ctx.DryRun {
			if ctx.Adopt {
				backup := fsutil.BackupPath(c.target)
				if err := os.Rename(c.target, backup); err != nil {
					return fmt.Errorf("backup %s: %w", c.target, err)
				}
				ctx.Out.Info("backup  %s -> %s", c.target, backup)
			} else {
				if err := os.Remove(c.target); err != nil {
					return err
				}
			}
		}
	}
	if ctx.DryRun {
		ctx.Out.Info("[dry-run] %s %s", change.Op, c.Describe())
		return nil
	}
	data, err := os.ReadFile(c.source)
	if err != nil {
		return fmt.Errorf("read source %s: %w", c.source, err)
	}
	if err := fsutil.AtomicWrite(c.target, data, c.mode.Perm()); err != nil {
		return fmt.Errorf("write %s: %w", c.target, err)
	}
	// When a conflict was resolved (adopt/force), report the actual outcome
	// rather than the original conflict status.
	displayOp := change.Op
	if change.Op == plan.OpConflict {
		if ctx.Adopt {
			displayOp = plan.OpUpdate // backed up + replaced
		} else {
			displayOp = plan.OpUpdate // force-replaced
		}
	}
	ctx.Out.Info("%-7s %s", displayOp, c.Describe())
	return nil
}
