package initrepo_test

import (
	"os"
	"path/filepath"
	"testing"

	"d0t/internal/initrepo"
)

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func TestInit_CreatesProfileLayout(t *testing.T) {
	dir := t.TempDir()

	if err := initrepo.Init(dir); err != nil {
		t.Fatal(err)
	}

	// Expected directories from the default scaffold.
	wantDirs := []string{
		"base/home",
		"base/xdg",
		"base/hooks",
		"base/fragments",
		"os/darwin",
		"os/linux",
		"hosts",
	}
	for _, d := range wantDirs {
		info, err := os.Stat(filepath.Join(dir, d))
		if err != nil {
			t.Errorf("expected dir %s: %v", d, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", d)
		}
	}
}

func TestInit_CreatesRootConfig(t *testing.T) {
	dir := t.TempDir()
	if err := initrepo.Init(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "d0t.toml")); err != nil {
		t.Errorf("expected d0t.toml: %v", err)
	}
}

func TestInit_CreatesBaseD0tfile(t *testing.T) {
	dir := t.TempDir()
	if err := initrepo.Init(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "base", "d0tfile")); err != nil {
		t.Errorf("expected base/d0tfile: %v", err)
	}
}

func TestInit_CreatesGitignore(t *testing.T) {
	dir := t.TempDir()
	if err := initrepo.Init(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err != nil {
		t.Errorf("expected .gitignore: %v", err)
	}
}

func TestInit_IdempotentOnExistingRepo(t *testing.T) {
	dir := t.TempDir()
	if err := initrepo.Init(dir); err != nil {
		t.Fatal(err)
	}
	// Second call should not fail.
	if err := initrepo.Init(dir); err != nil {
		t.Fatalf("second init failed: %v", err)
	}
}

func TestInit_DoesNotOverwriteExistingFiles(t *testing.T) {
	dir := t.TempDir()
	// Pre-create d0t.toml with custom content.
	custom := "# custom\n"
	if err := os.WriteFile(filepath.Join(dir, "d0t.toml"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := initrepo.Init(dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "d0t.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != custom {
		t.Errorf("init overwrote existing d0t.toml: got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Adopt
// ---------------------------------------------------------------------------

func TestAdopt_MovesFileAndCreatesSymlink(t *testing.T) {
	dir := t.TempDir()
	// Create source file to adopt.
	srcFile := filepath.Join(dir, "existing", ".zshrc")
	if err := os.MkdirAll(filepath.Dir(srcFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcFile, []byte("# zsh"), 0o644); err != nil {
		t.Fatal(err)
	}

	repoDir := filepath.Join(dir, "repo")
	if err := initrepo.Init(repoDir); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(repoDir, "base", "home", ".zshrc")
	if err := initrepo.Adopt(srcFile, dest); err != nil {
		t.Fatal(err)
	}

	// File should be in the repo.
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("file not moved to repo: %v", err)
	}
	// Original path should now be a symlink pointing to the repo file.
	link, err := os.Readlink(srcFile)
	if err != nil {
		t.Fatalf("original path is not a symlink: %v", err)
	}
	if link != dest {
		t.Errorf("symlink points to %q, want %q", link, dest)
	}
}

func TestAdopt_FailsIfDestAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, ".zshrc")
	dest := filepath.Join(dir, "repo", "base", "home", ".zshrc")
	if err := os.WriteFile(src, []byte("# src"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("# dest"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := initrepo.Adopt(src, dest); err == nil {
		t.Error("expected error when dest already exists, got nil")
	}
}

func TestAdopt_FailsIfSrcDoesNotExist(t *testing.T) {
	dir := t.TempDir()
	if err := initrepo.Adopt(filepath.Join(dir, "no-such-file"), filepath.Join(dir, "dest")); err == nil {
		t.Error("expected error for missing source, got nil")
	}
}

func TestAdopt_PreservesFileContent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, ".zshrc")
	dest := filepath.Join(dir, "repo", "base", "home", ".zshrc")
	content := "# my zsh config\nexport EDITOR=nvim\n"
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := initrepo.Adopt(src, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}
