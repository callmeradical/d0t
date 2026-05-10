// Package sync implements d0t sync: pull the dotfiles repo then re-apply.
package sync

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// ErrAlreadyUpToDate is returned by Pull when the repo has no new commits.
// Run treats this as a non-fatal condition and still runs Apply.
var ErrAlreadyUpToDate = errors.New("already up to date")

// Config holds injectable functions so the behaviour is testable without
// hitting the real filesystem or network.
type Config struct {
	// Pull runs `git pull` in the repo directory. Returning ErrAlreadyUpToDate
	// is non-fatal; any other error aborts before Apply runs.
	Pull func() error
	// Apply re-converges the managed targets after a successful pull.
	Apply func() error
}

// Options controls which steps of Run execute.
type Options struct {
	// NoPull skips the git pull step (apply still runs).
	NoPull bool
	// NoApply skips the apply step.
	NoApply bool
}

// Syncer runs the sync workflow.
type Syncer struct{ cfg Config }

// New constructs a Syncer from a Config.
func New(cfg Config) *Syncer { return &Syncer{cfg: cfg} }

// Run executes the sync workflow: pull → apply.
func (s *Syncer) Run(opts Options) error {
	if !opts.NoPull && s.cfg.Pull != nil {
		if err := s.cfg.Pull(); err != nil {
			if errors.Is(err, ErrAlreadyUpToDate) {
				fmt.Println("sync: already up to date")
			} else {
				return fmt.Errorf("sync pull: %w", err)
			}
		}
	}
	if !opts.NoApply && s.cfg.Apply != nil {
		if err := s.cfg.Apply(); err != nil {
			return fmt.Errorf("sync apply: %w", err)
		}
	}
	return nil
}

// GitPull returns a Pull function that runs `git pull` in repoPath.
func GitPull(repoPath string) func() error {
	return func() error {
		cmd := exec.Command("git", "-C", repoPath, "pull")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
		return nil
	}
}
