package plan_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/callmeradical/d0t/internal/plan"
	"github.com/callmeradical/d0t/internal/primitive"
	"github.com/callmeradical/d0t/internal/repo"
	"github.com/callmeradical/d0t/internal/ui"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func makeRepo(t *testing.T) (repoDir string, r *repo.Repo) {
	t.Helper()
	dir := t.TempDir()
	r = &repo.Repo{
		Root:     dir,
		OS:       "darwin",
		Hostname: "test-host",
		Home:     t.TempDir(),
	}
	return dir, r
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func buildOpts() plan.BuildOptions {
	return plan.BuildOptions{ReaderBuilder: primitive.NewProfileReaderBuilder()}
}

func applyCtx(t *testing.T, r *repo.Repo) *plan.Context {
	t.Helper()
	return &plan.Context{
		Ctx:  context.Background(),
		Repo: r,
		Out:  ui.NewPrinter(io.Discard, false),
	}
}

// ---------------------------------------------------------------------------
// Build: no d0tfile → empty plan (no convention fallback)
// ---------------------------------------------------------------------------

func TestBuild_NoManifest_EmptyPlan(t *testing.T) {
	repoDir, r := makeRepo(t)
	// Put a file in home/ with no d0tfile — should NOT be auto-discovered.
	mustWrite(t, filepath.Join(repoDir, "base", "home", ".zshrc"), "# zsh")

	p, err := plan.Build(r, buildOpts())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) != 0 {
		t.Errorf("expected 0 files (no d0tfile), got %d", len(p.Files))
	}
}

// ---------------------------------------------------------------------------
// Build: single profile with d0tfile
// ---------------------------------------------------------------------------

func TestBuild_ManifestLink(t *testing.T) {
	repoDir, r := makeRepo(t)
	mustWrite(t, filepath.Join(repoDir, "base", "home", ".zshrc"), "# zsh")
	mustWrite(t, filepath.Join(repoDir, "base", "d0tfile"), "link home/.zshrc\n")

	p, err := plan.Build(r, buildOpts())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(p.Files))
	}
	want := filepath.Join(r.Home, ".zshrc")
	if p.Files[0].Target() != want {
		t.Errorf("target = %q, want %q", p.Files[0].Target(), want)
	}
	if p.Files[0].Kind() != "link" {
		t.Errorf("kind = %q, want link", p.Files[0].Kind())
	}
}

func TestBuild_ManifestCopyWithMode(t *testing.T) {
	repoDir, r := makeRepo(t)
	mustWrite(t, filepath.Join(repoDir, "base", "home", ".ssh", "config"), "Host *")
	mustWrite(t, filepath.Join(repoDir, "base", "d0tfile"), "copy home/.ssh/config  mode=0600\n")

	p, err := plan.Build(r, buildOpts())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) != 1 {
		t.Fatalf("got %d files, want 1", len(p.Files))
	}
	if p.Files[0].Kind() != "copy" {
		t.Errorf("kind = %q, want copy", p.Files[0].Kind())
	}
}

// ---------------------------------------------------------------------------
// Build: only declared files are managed
// ---------------------------------------------------------------------------

func TestBuild_OnlyDeclaredFilesManaged(t *testing.T) {
	repoDir, r := makeRepo(t)
	mustWrite(t, filepath.Join(repoDir, "base", "home", ".zshrc"), "# zsh")
	mustWrite(t, filepath.Join(repoDir, "base", "home", ".bashrc"), "# bash — not declared")
	mustWrite(t, filepath.Join(repoDir, "base", "d0tfile"), "link home/.zshrc\n")

	p, err := plan.Build(r, buildOpts())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) != 1 {
		t.Fatalf("expected 1 file (only .zshrc declared), got %d", len(p.Files))
	}
}

// ---------------------------------------------------------------------------
// Build: later profile wins on same target
// ---------------------------------------------------------------------------

func TestBuild_LaterProfileWins(t *testing.T) {
	repoDir, r := makeRepo(t)

	mustWrite(t, filepath.Join(repoDir, "base", "home", ".zshrc"), "# base")
	mustWrite(t, filepath.Join(repoDir, "base", "d0tfile"), "link home/.zshrc\n")

	mustWrite(t, filepath.Join(repoDir, "hosts", "test-host", "home", ".zshrc"), "# host")
	mustWrite(t, filepath.Join(repoDir, "hosts", "test-host", "d0tfile"), "link home/.zshrc\n")

	p, err := plan.Build(r, buildOpts())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) != 1 {
		t.Fatalf("expected 1 (deduped) file, got %d", len(p.Files))
	}
	if !strings.Contains(p.Files[0].Source(), "hosts/test-host") {
		t.Errorf("host profile source should win, got %q", p.Files[0].Source())
	}
}

// ---------------------------------------------------------------------------
// Build: non-conflicting files from different profiles merge
// ---------------------------------------------------------------------------

func TestBuild_MergesNonConflicting(t *testing.T) {
	repoDir, r := makeRepo(t)

	mustWrite(t, filepath.Join(repoDir, "base", "home", ".zshrc"), "# zsh")
	mustWrite(t, filepath.Join(repoDir, "base", "d0tfile"), "link home/.zshrc\n")

	mustWrite(t, filepath.Join(repoDir, "os", "darwin", "home", ".zprofile"), "# zprofile")
	mustWrite(t, filepath.Join(repoDir, "os", "darwin", "d0tfile"), "link home/.zprofile\n")

	p, err := plan.Build(r, buildOpts())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(p.Files))
	}
}

// ---------------------------------------------------------------------------
// Build: ReaderBuilder required
// ---------------------------------------------------------------------------

func TestBuild_ReaderBuilderRequired(t *testing.T) {
	_, r := makeRepo(t)
	_, err := plan.Build(r, plan.BuildOptions{})
	if err == nil {
		t.Error("expected error when ReaderBuilder is nil")
	}
}

// ---------------------------------------------------------------------------
// Build: profile order recorded
// ---------------------------------------------------------------------------

func TestBuild_ProfileOrderRecorded(t *testing.T) {
	repoDir, r := makeRepo(t)
	mustMkdir(t, filepath.Join(repoDir, "base"))
	mustMkdir(t, filepath.Join(repoDir, "os", "darwin"))
	mustWrite(t, filepath.Join(repoDir, "base", "d0tfile"), "# empty\n")
	mustWrite(t, filepath.Join(repoDir, "os", "darwin", "d0tfile"), "# empty\n")

	p, err := plan.Build(r, buildOpts())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Profiles) < 2 {
		t.Fatalf("expected >= 2 profiles, got %v", p.Profiles)
	}
	if p.Profiles[0] != "base" {
		t.Errorf("profiles[0] = %q, want base", p.Profiles[0])
	}
	if p.Profiles[1] != "os/darwin" {
		t.Errorf("profiles[1] = %q, want os/darwin", p.Profiles[1])
	}
}

// ---------------------------------------------------------------------------
// Print: summary counts all actions correctly
// ---------------------------------------------------------------------------

func TestPrint_CountsAllNoOps(t *testing.T) {
	repoDir, r := makeRepo(t)

	mustWrite(t, filepath.Join(repoDir, "base", "home", ".zshrc"), "# zsh")
	mustWrite(t, filepath.Join(repoDir, "base", "home", ".bashrc"), "# bash")
	mustWrite(t, filepath.Join(repoDir, "base", "d0tfile"), "link home/.zshrc\nlink home/.bashrc\n")

	p, err := plan.Build(r, buildOpts())
	if err != nil {
		t.Fatal(err)
	}

	// Apply both so they become NoOps.
	for _, fa := range p.Files {
		if err := fa.Apply(applyCtx(t, r)); err != nil {
			t.Fatal(err)
		}
	}

	p2, err := plan.Build(r, buildOpts())
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := p2.Print(&buf, r, plan.PrintOptions{ShowNoOp: false}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "2 ok") {
		t.Errorf("expected '2 ok' in summary, got:\n%s", buf.String())
	}
}
