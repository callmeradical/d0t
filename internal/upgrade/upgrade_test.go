// Package upgrade implements d0t upgrade: run package manager upgrades.
package upgrade_test

import (
	"testing"

	"github.com/callmeradical/d0t/internal/upgrade"
)

// ---------------------------------------------------------------------------
// Upgrade: runs each available manager
// ---------------------------------------------------------------------------

func TestUpgrade_RunsAvailableManagers(t *testing.T) {
	var called []string
	u := upgrade.New(upgrade.Config{
		Managers: []upgrade.Manager{
			{Name: "brew", Available: true, Run: func() error { called = append(called, "brew"); return nil }},
			{Name: "mas", Available: true, Run: func() error { called = append(called, "mas"); return nil }},
		},
	})
	if err := u.Run(); err != nil {
		t.Fatal(err)
	}
	if len(called) != 2 {
		t.Errorf("expected 2 managers called, got %v", called)
	}
}

func TestUpgrade_SkipsUnavailableManagers(t *testing.T) {
	var called []string
	u := upgrade.New(upgrade.Config{
		Managers: []upgrade.Manager{
			{Name: "brew", Available: false, Run: func() error { called = append(called, "brew"); return nil }},
			{Name: "apt", Available: true, Run: func() error { called = append(called, "apt"); return nil }},
		},
	})
	if err := u.Run(); err != nil {
		t.Fatal(err)
	}
	if len(called) != 1 || called[0] != "apt" {
		t.Errorf("expected only apt, got %v", called)
	}
}

func TestUpgrade_ContinuesAfterManagerFailure(t *testing.T) {
	var called []string
	u := upgrade.New(upgrade.Config{
		Managers: []upgrade.Manager{
			{Name: "brew", Available: true, Run: func() error {
				called = append(called, "brew")
				return nil
			}},
		},
	})
	if err := u.Run(); err != nil {
		t.Fatal(err)
	}
	if len(called) == 0 {
		t.Error("expected brew to be called")
	}
}

func TestUpgrade_NoManagersIsNoop(t *testing.T) {
	u := upgrade.New(upgrade.Config{})
	if err := u.Run(); err != nil {
		t.Fatalf("empty upgrade should not error: %v", err)
	}
}
