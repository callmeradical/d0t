// Package upgrade implements d0t upgrade: run package manager upgrades.
package upgrade

import (
	"fmt"
	"os"
	"os/exec"
)

// Manager is one package manager that can be upgraded.
type Manager struct {
	// Name is the human-readable manager name (brew, apt, mas).
	Name string
	// Available indicates whether the manager is installed on this host.
	// Defaults to true when zero.
	Available bool
	// Run performs the upgrade for this manager.
	Run func() error
}

// Config holds the set of managers to run.
type Config struct {
	Managers []Manager
}

// Upgrader runs package manager upgrades.
type Upgrader struct{ cfg Config }

// New constructs an Upgrader.
func New(cfg Config) *Upgrader { return &Upgrader{cfg: cfg} }

// Run upgrades all available managers. Skips managers where Available is
// false. A manager failure is printed as a warning and does not abort
// the remaining managers.
func (u *Upgrader) Run() error {
	for _, m := range u.cfg.Managers {
		if !m.Available {
			fmt.Printf("upgrade: %s not available — skipping\n", m.Name)
			continue
		}
		fmt.Printf("upgrade: %s\n", m.Name)
		if err := m.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "upgrade: %s failed: %v\n", m.Name, err)
		}
	}
	return nil
}

// DefaultManagers returns the set of managers d0t knows about, each
// pre-checked for availability on the current host.
func DefaultManagers() []Manager {
	return []Manager{
		{
			Name:      "brew",
			Available: commandExists("brew"),
			Run:       brewUpgrade,
		},
		{
			Name:      "mas",
			Available: commandExists("mas"),
			Run:       masUpgrade,
		},
		{
			Name:      "apt-get",
			Available: commandExists("apt-get"),
			Run:       aptUpgrade,
		},
	}
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func brewUpgrade() error {
	// brew update fetches latest formulae; brew upgrade installs newer versions.
	for _, args := range [][]string{
		{"brew", "update"},
		{"brew", "upgrade"},
		{"brew", "upgrade", "--cask"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%v: %w", args, err)
		}
	}
	return nil
}

func masUpgrade() error {
	cmd := exec.Command("mas", "upgrade")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func aptUpgrade() error {
	for _, args := range [][]string{
		{"apt-get", "update"},
		{"apt-get", "upgrade", "-y"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%v: %w", args, err)
		}
	}
	return nil
}
