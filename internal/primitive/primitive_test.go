package primitive_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"d0t/internal/plan"
	"d0t/internal/primitive"
	"d0t/internal/repo"
	"d0t/internal/ui"
)

// ---------------------------------------------------------------------------
// test helpers
// ---------------------------------------------------------------------------

func newCtx(t *testing.T, dryRun, force, adopt bool) *plan.Context {
	t.Helper()
	r := &repo.Repo{Root: t.TempDir()}
	return &plan.Context{
		Ctx:    context.Background(),
		Repo:   r,
		DryRun: dryRun,
		Force:  force,
		Adopt:  adopt,
		Out:    ui.NewPrinter(io.Discard, false),
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// Link.Plan
// ---------------------------------------------------------------------------

func TestLink_Plan_TargetAbsent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", ".zshrc")
	mustWrite(t, src, "# zsh")
	target := filepath.Join(dir, "dst", ".zshrc")

	l := primitive.NewLink(src, target, "base/home/.zshrc")
	ch, err := l.Plan(newCtx(t, false, false, false))
	if err != nil {
		t.Fatal(err)
	}
	if ch.Op != plan.OpCreate {
		t.Errorf("Op = %v, want %v", ch.Op, plan.OpCreate)
	}
}

func TestLink_Plan_AlreadyCorrect(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", ".zshrc")
	mustWrite(t, src, "# zsh")
	target := filepath.Join(dir, "dst", ".zshrc")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(src, target); err != nil {
		t.Fatal(err)
	}

	l := primitive.NewLink(src, target, "base/home/.zshrc")
	ch, err := l.Plan(newCtx(t, false, false, false))
	if err != nil {
		t.Fatal(err)
	}
	if ch.Op != plan.OpNoOp {
		t.Errorf("Op = %v, want %v", ch.Op, plan.OpNoOp)
	}
}

func TestLink_Plan_StaleSymlink(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", ".zshrc")
	mustWrite(t, src, "# zsh")
	target := filepath.Join(dir, "dst", ".zshrc")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	// Target points to something else.
	if err := os.Symlink("/some/other/file", target); err != nil {
		t.Fatal(err)
	}

	l := primitive.NewLink(src, target, "base/home/.zshrc")
	ch, err := l.Plan(newCtx(t, false, false, false))
	if err != nil {
		t.Fatal(err)
	}
	if ch.Op != plan.OpUpdate {
		t.Errorf("Op = %v, want %v", ch.Op, plan.OpUpdate)
	}
}

func TestLink_Plan_ConflictRegularFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", ".zshrc")
	mustWrite(t, src, "# zsh")
	target := filepath.Join(dir, "dst", ".zshrc")
	mustWrite(t, target, "# existing")

	l := primitive.NewLink(src, target, "base/home/.zshrc")
	ch, err := l.Plan(newCtx(t, false, false, false))
	if err != nil {
		t.Fatal(err)
	}
	if ch.Op != plan.OpConflict {
		t.Errorf("Op = %v, want %v", ch.Op, plan.OpConflict)
	}
}

// ---------------------------------------------------------------------------
// Link.Apply
// ---------------------------------------------------------------------------

func TestLink_Apply_CreatesSymlink(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", ".zshrc")
	mustWrite(t, src, "# zsh")
	target := filepath.Join(dir, "dst", ".zshrc")

	l := primitive.NewLink(src, target, "base/home/.zshrc")
	if err := l.Apply(newCtx(t, false, false, false)); err != nil {
		t.Fatal(err)
	}
	got, err := os.Readlink(target)
	if err != nil {
		t.Fatalf("target is not a symlink: %v", err)
	}
	if got != src {
		t.Errorf("symlink target = %q, want %q", got, src)
	}
}

func TestLink_Apply_DryRunDoesNotCreateSymlink(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", ".zshrc")
	mustWrite(t, src, "# zsh")
	target := filepath.Join(dir, "dst", ".zshrc")

	l := primitive.NewLink(src, target, "base/home/.zshrc")
	if err := l.Apply(newCtx(t, true, false, false)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Error("dry-run should not create the symlink")
	}
}

func TestLink_Apply_ConflictWithoutForceFails(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", ".zshrc")
	mustWrite(t, src, "# zsh")
	target := filepath.Join(dir, "dst", ".zshrc")
	mustWrite(t, target, "# pre-existing")

	l := primitive.NewLink(src, target, "base/home/.zshrc")
	if err := l.Apply(newCtx(t, false, false, false)); err == nil {
		t.Error("expected error for conflict without --force, got nil")
	}
}

func TestLink_Apply_ForceOverwritesConflict(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", ".zshrc")
	mustWrite(t, src, "# zsh")
	target := filepath.Join(dir, "dst", ".zshrc")
	mustWrite(t, target, "# pre-existing")

	l := primitive.NewLink(src, target, "base/home/.zshrc")
	if err := l.Apply(newCtx(t, false, true, false)); err != nil {
		t.Fatal(err)
	}
	got, err := os.Readlink(target)
	if err != nil {
		t.Fatalf("target is not a symlink after --force: %v", err)
	}
	if got != src {
		t.Errorf("symlink target = %q, want %q", got, src)
	}
}

func TestLink_Apply_AdoptBacksUpConflict(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", ".zshrc")
	mustWrite(t, src, "# zsh")
	target := filepath.Join(dir, "dst", ".zshrc")
	mustWrite(t, target, "# pre-existing")

	l := primitive.NewLink(src, target, "base/home/.zshrc")
	if err := l.Apply(newCtx(t, false, false, true)); err != nil {
		t.Fatal(err)
	}
	// Target should now be a symlink.
	if _, err := os.Readlink(target); err != nil {
		t.Errorf("target is not a symlink after --adopt: %v", err)
	}
	// A backup file should exist alongside the target.
	entries, _ := os.ReadDir(filepath.Dir(target))
	var backupFound bool
	for _, e := range entries {
		if e.Name() != ".zshrc" && len(e.Name()) > len(".zshrc") {
			backupFound = true
		}
	}
	if !backupFound {
		t.Error("expected a backup file after --adopt")
	}
}

// ---------------------------------------------------------------------------
// Copy.Plan
// ---------------------------------------------------------------------------

func TestCopy_Plan_TargetAbsent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "config")
	mustWrite(t, src, "contents")
	target := filepath.Join(dir, "dst", "config")

	c := primitive.NewCopy(src, target, "base/home/.ssh/config.copy", 0o600)
	ch, err := c.Plan(newCtx(t, false, false, false))
	if err != nil {
		t.Fatal(err)
	}
	if ch.Op != plan.OpCreate {
		t.Errorf("Op = %v, want %v", ch.Op, plan.OpCreate)
	}
}

func TestCopy_Plan_MatchingContent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "config")
	mustWrite(t, src, "contents")
	target := filepath.Join(dir, "dst", "config")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("contents"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := primitive.NewCopy(src, target, "base/home/.ssh/config.copy", 0o600)
	ch, err := c.Plan(newCtx(t, false, false, false))
	if err != nil {
		t.Fatal(err)
	}
	if ch.Op != plan.OpNoOp {
		t.Errorf("Op = %v, want %v", ch.Op, plan.OpNoOp)
	}
}

func TestCopy_Plan_DifferentContent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "config")
	mustWrite(t, src, "new contents")
	target := filepath.Join(dir, "dst", "config")
	mustWrite(t, target, "old contents")

	c := primitive.NewCopy(src, target, "base/home/.ssh/config.copy", 0o644)
	ch, err := c.Plan(newCtx(t, false, false, false))
	if err != nil {
		t.Fatal(err)
	}
	if ch.Op != plan.OpUpdate {
		t.Errorf("Op = %v, want %v", ch.Op, plan.OpUpdate)
	}
}

// ---------------------------------------------------------------------------
// Copy.Apply
// ---------------------------------------------------------------------------

func TestCopy_Apply_WritesFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "config")
	mustWrite(t, src, "ssh config contents")
	target := filepath.Join(dir, "dst", "config")

	c := primitive.NewCopy(src, target, "base/home/.ssh/config.copy", 0o600)
	if err := c.Apply(newCtx(t, false, false, false)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ssh config contents" {
		t.Errorf("target content = %q, want %q", got, "ssh config contents")
	}
}

func TestCopy_Apply_Idempotent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "config")
	mustWrite(t, src, "contents")
	target := filepath.Join(dir, "dst", "config")

	c := primitive.NewCopy(src, target, "base/home/.ssh/config.copy", 0o644)
	for i := 0; i < 3; i++ {
		if err := c.Apply(newCtx(t, false, false, false)); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
}
