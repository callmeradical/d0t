// Package sync implements d0t sync: pull the dotfiles repo and re-apply.
package sync_test

import (
	"errors"
	"testing"

	"github.com/callmeradical/d0t/internal/sync"
)

// ---------------------------------------------------------------------------
// Sync: pull then apply
// ---------------------------------------------------------------------------

func TestSync_PullThenApply(t *testing.T) {
	var order []string
	s := sync.New(sync.Config{
		Pull:  func() error { order = append(order, "pull"); return nil },
		Apply: func() error { order = append(order, "apply"); return nil },
	})
	if err := s.Run(sync.Options{}); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "pull" || order[1] != "apply" {
		t.Errorf("expected [pull apply], got %v", order)
	}
}

func TestSync_PullFailureAbortsApply(t *testing.T) {
	applyCalled := false
	s := sync.New(sync.Config{
		Pull:  func() error { return errors.New("conflict") },
		Apply: func() error { applyCalled = true; return nil },
	})
	if err := s.Run(sync.Options{}); err == nil {
		t.Error("expected error when pull fails")
	}
	if applyCalled {
		t.Error("apply should not run when pull fails")
	}
}

func TestSync_NoApply(t *testing.T) {
	applyCalled := false
	s := sync.New(sync.Config{
		Pull:  func() error { return nil },
		Apply: func() error { applyCalled = true; return nil },
	})
	if err := s.Run(sync.Options{NoApply: true}); err != nil {
		t.Fatal(err)
	}
	if applyCalled {
		t.Error("apply should be skipped with NoApply")
	}
}

func TestSync_NoPull(t *testing.T) {
	pullCalled := false
	applyCalled := false
	s := sync.New(sync.Config{
		Pull:  func() error { pullCalled = true; return nil },
		Apply: func() error { applyCalled = true; return nil },
	})
	if err := s.Run(sync.Options{NoPull: true}); err != nil {
		t.Fatal(err)
	}
	if pullCalled {
		t.Error("pull should be skipped with NoPull")
	}
	if !applyCalled {
		t.Error("apply should still run with NoPull")
	}
}

func TestSync_AlreadyUpToDate(t *testing.T) {
	// A pull that returns ErrAlreadyUpToDate should still run apply.
	applyCalled := false
	s := sync.New(sync.Config{
		Pull:  func() error { return sync.ErrAlreadyUpToDate },
		Apply: func() error { applyCalled = true; return nil },
	})
	if err := s.Run(sync.Options{}); err != nil {
		t.Fatal(err)
	}
	if !applyCalled {
		t.Error("apply should run even when already up to date")
	}
}
