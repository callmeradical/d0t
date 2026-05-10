// Package selfupdate implements d0t update: rebuild the d0t binary from source.
package selfupdate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Config controls how the binary is rebuilt.
type Config struct {
	// SourcePath is the absolute path to the d0t source repo (the directory
	// containing go.mod). Comes from d0t.toml [self] source.
	SourcePath string
	// OutputPath is where the rebuilt binary is written. Defaults to the
	// path of the currently running d0t binary.
	OutputPath string
	// Build is the function that compiles the binary. Injectable for tests.
	Build func(sourcePath, outputPath string) error
}

// SelfUpdater rebuilds the d0t binary.
type SelfUpdater struct{ cfg Config }

// New constructs a SelfUpdater.
func New(cfg Config) *SelfUpdater {
	if cfg.Build == nil {
		cfg.Build = goBuild
	}
	return &SelfUpdater{cfg: cfg}
}

// Run rebuilds the binary. Returns an error if SourcePath or OutputPath are
// not configured.
func (s *SelfUpdater) Run() error {
	src := strings.TrimSpace(s.cfg.SourcePath)
	if src == "" {
		return fmt.Errorf("d0t update: no source path configured\n" +
			"Add [self] source = \"/path/to/d0t\" to d0t.toml")
	}
	out := strings.TrimSpace(s.cfg.OutputPath)
	if out == "" {
		return fmt.Errorf("d0t update: no output path (could not determine binary location)")
	}
	fmt.Printf("update: building d0t from %s → %s\n", src, out)
	return s.cfg.Build(src, out)
}

// CurrentBinaryPath returns the absolute path of the running d0t binary.
func CurrentBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	// Resolve any symlinks so we write to the real file.
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe, nil // use unresolved path as fallback
	}
	return resolved, nil
}

func goBuild(sourcePath, outputPath string) error {
	cmdPath := filepath.Join(sourcePath, "cmd", "d0t")
	cmd := exec.Command("go", "build", "-o", outputPath, cmdPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = sourcePath
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build: %w", err)
	}
	fmt.Printf("update: d0t rebuilt successfully\n")
	return nil
}
