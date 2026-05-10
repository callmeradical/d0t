package primitive_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"d0t/internal/plan"
	"d0t/internal/primitive"
)

// ---------------------------------------------------------------------------
// ParseFragmentHeader
// ---------------------------------------------------------------------------

func TestParseFragmentHeader_Valid(t *testing.T) {
	src := "# d0t: target=~/.zshrc marker=path-export\nexport PATH=\"$HOME/.local/bin:$PATH\"\n"
	hdr, body, err := primitive.ParseFragmentHeader(src)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.RawTarget != "~/.zshrc" {
		t.Errorf("target = %q, want %q", hdr.RawTarget, "~/.zshrc")
	}
	if hdr.Marker != "path-export" {
		t.Errorf("marker = %q, want %q", hdr.Marker, "path-export")
	}
	if !strings.Contains(body, "export PATH") {
		t.Errorf("body should contain the content line, got %q", body)
	}
}

func TestParseFragmentHeader_MissingTarget(t *testing.T) {
	src := "# d0t: marker=path-export\ncontent\n"
	_, _, err := primitive.ParseFragmentHeader(src)
	if err == nil {
		t.Error("expected error for missing target, got nil")
	}
}

func TestParseFragmentHeader_MissingMarker(t *testing.T) {
	src := "# d0t: target=~/.zshrc\ncontent\n"
	_, _, err := primitive.ParseFragmentHeader(src)
	if err == nil {
		t.Error("expected error for missing marker, got nil")
	}
}

func TestParseFragmentHeader_MissingHeader(t *testing.T) {
	src := "just content, no header\n"
	_, _, err := primitive.ParseFragmentHeader(src)
	if err == nil {
		t.Error("expected error for missing d0t header line, got nil")
	}
}

// ---------------------------------------------------------------------------
// Fragment.Plan
// ---------------------------------------------------------------------------

func TestFragment_Plan_TargetAbsent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "path-export.sh.fragment")
	target := filepath.Join(dir, "dst", ".zshrc")
	mustWrite(t, src, "# d0t: target="+target+" marker=path-export\nexport PATH=\"$HOME/.local/bin:$PATH\"\n")

	frag, err := primitive.NewFragment(src, "base/fragments/path-export.sh.fragment", dir)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := frag.Plan(newCtx(t, false, false, false))
	if err != nil {
		t.Fatal(err)
	}
	if ch.Op != plan.OpCreate {
		t.Errorf("Op = %v, want %v", ch.Op, plan.OpCreate)
	}
}

func TestFragment_Plan_NoOp(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "path-export.sh.fragment")
	target := filepath.Join(dir, "dst", ".zshrc")
	content := "export PATH=\"$HOME/.local/bin:$PATH\"\n"
	mustWrite(t, src, "# d0t: target="+target+" marker=path-export\n"+content)

	// Write target with the already-managed block.
	managed := "# >>> d0t:path-export >>>\n" + content + "# <<< d0t:path-export <<<\n"
	mustWrite(t, target, "# existing content\n"+managed)

	frag, err := primitive.NewFragment(src, "base/fragments/path-export.sh.fragment", dir)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := frag.Plan(newCtx(t, false, false, false))
	if err != nil {
		t.Fatal(err)
	}
	if ch.Op != plan.OpNoOp {
		t.Errorf("Op = %v, want %v", ch.Op, plan.OpNoOp)
	}
}

func TestFragment_Plan_Update(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "path-export.sh.fragment")
	target := filepath.Join(dir, "dst", ".zshrc")
	mustWrite(t, src, "# d0t: target="+target+" marker=path-export\nexport PATH=\"$HOME/.local/bin:$PATH\"\n")

	// Target has a stale managed block with different content.
	stale := "# >>> d0t:path-export >>>\nold content\n# <<< d0t:path-export <<<\n"
	mustWrite(t, target, stale)

	frag, err := primitive.NewFragment(src, "base/fragments/path-export.sh.fragment", dir)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := frag.Plan(newCtx(t, false, false, false))
	if err != nil {
		t.Fatal(err)
	}
	if ch.Op != plan.OpUpdate {
		t.Errorf("Op = %v, want %v", ch.Op, plan.OpUpdate)
	}
}

// ---------------------------------------------------------------------------
// Fragment.Apply
// ---------------------------------------------------------------------------

func TestFragment_Apply_InsertsIntoExistingFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "path-export.sh.fragment")
	target := filepath.Join(dir, "dst", ".zshrc")
	content := "export PATH=\"$HOME/.local/bin:$PATH\"\n"
	mustWrite(t, src, "# d0t: target="+target+" marker=path-export\n"+content)
	mustWrite(t, target, "# existing line\n")

	frag, err := primitive.NewFragment(src, "base/fragments/path-export.sh.fragment", dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := frag.Apply(newCtx(t, false, false, false)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "# >>> d0t:path-export >>>") {
		t.Error("expected opening marker in target")
	}
	if !strings.Contains(s, content) {
		t.Error("expected fragment content in target")
	}
	if !strings.Contains(s, "# <<< d0t:path-export <<<") {
		t.Error("expected closing marker in target")
	}
	// Existing content must be preserved.
	if !strings.Contains(s, "# existing line") {
		t.Error("existing content should be preserved")
	}
}

func TestFragment_Apply_ReplacesExistingBlock(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "path-export.sh.fragment")
	target := filepath.Join(dir, "dst", ".zshrc")
	newContent := "export PATH=\"$HOME/.local/bin:$PATH\"\n"
	mustWrite(t, src, "# d0t: target="+target+" marker=path-export\n"+newContent)

	old := "# >>> d0t:path-export >>>\nold content\n# <<< d0t:path-export <<<\n"
	mustWrite(t, target, "before\n"+old+"after\n")

	frag, err := primitive.NewFragment(src, "base/fragments/path-export.sh.fragment", dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := frag.Apply(newCtx(t, false, false, false)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if strings.Contains(s, "old content") {
		t.Error("old block content should be replaced")
	}
	if !strings.Contains(s, newContent) {
		t.Error("new content should be present")
	}
	if !strings.Contains(s, "before") || !strings.Contains(s, "after") {
		t.Error("surrounding content should be preserved")
	}
}

func TestFragment_Apply_CreatesTargetIfAbsent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "path-export.sh.fragment")
	target := filepath.Join(dir, "dst", ".zshrc")
	mustWrite(t, src, "# d0t: target="+target+" marker=path-export\nexport PATH=\"$HOME/.local/bin:$PATH\"\n")
	// target does NOT exist

	frag, err := primitive.NewFragment(src, "base/fragments/path-export.sh.fragment", dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := frag.Apply(newCtx(t, false, false, false)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("target not created: %v", err)
	}
}

func TestFragment_Apply_DryRunDoesNotModify(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "path-export.sh.fragment")
	target := filepath.Join(dir, "dst", ".zshrc")
	original := "# original\n"
	mustWrite(t, src, "# d0t: target="+target+" marker=path-export\nnew line\n")
	mustWrite(t, target, original)

	frag, err := primitive.NewFragment(src, "base/fragments/path-export.sh.fragment", dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := frag.Apply(newCtx(t, true, false, false)); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != original {
		t.Errorf("dry-run should not modify target, got %q", got)
	}
}

func TestFragment_Apply_Idempotent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "path-export.sh.fragment")
	target := filepath.Join(dir, "dst", ".zshrc")
	mustWrite(t, src, "# d0t: target="+target+" marker=path-export\nexport X=1\n")
	mustWrite(t, target, "# base\n")

	frag, err := primitive.NewFragment(src, "base/fragments/path-export.sh.fragment", dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := frag.Apply(newCtx(t, false, false, false)); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	got, _ := os.ReadFile(target)
	// Should only have one copy of the block.
	count := strings.Count(string(got), "# >>> d0t:path-export >>>")
	if count != 1 {
		t.Errorf("expected exactly 1 opening marker, got %d\n%s", count, got)
	}
}
