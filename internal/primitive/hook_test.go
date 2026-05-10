package primitive_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/callmeradical/d0t/internal/plan"
	"github.com/callmeradical/d0t/internal/primitive"
	"github.com/callmeradical/d0t/internal/repo"
)

// mustExec writes a shell script and makes it executable.
func mustExec(t *testing.T, path, content string) {
	t.Helper()
	mustWrite(t, path, content)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// ClassifyHook
// ---------------------------------------------------------------------------

func TestClassifyHook_Phase(t *testing.T) {
	cases := []struct {
		name      string
		wantPhase string
		wantOk    bool
	}{
		{"pre-apply.sh", "pre-apply", true},
		{"post-apply.sh", "post-apply", true},
		{"post-apply-10-reload.sh", "post-apply", true},
		{"pre-remove.sh", "pre-remove", true},
		{"post-remove.sh", "post-remove", true},
		{"README.md", "", false},
		{"pre-apply-10.optional.sh", "pre-apply", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			phase, ok := primitive.ClassifyHook(tc.name)
			if ok != tc.wantOk {
				t.Errorf("ok = %v, want %v", ok, tc.wantOk)
			}
			if phase != tc.wantPhase {
				t.Errorf("phase = %q, want %q", phase, tc.wantPhase)
			}
		})
	}
}

func TestClassifyHook_Optional(t *testing.T) {
	cases := []struct {
		name         string
		wantOptional bool
	}{
		{"pre-apply.sh", false},
		{"pre-apply.optional.sh", true},
		{"post-apply-10.optional.sh", true},
		{"post-apply-10-desc.optional.sh", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := primitive.NewHook(tc.name, "/fake/path", "base", "pre-apply")
			if h.Optional != tc.wantOptional {
				t.Errorf("Optional = %v, want %v", h.Optional, tc.wantOptional)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// WalkHooks
// ---------------------------------------------------------------------------

func TestWalkHooks_EnumeratesPhases(t *testing.T) {
	dir := t.TempDir()
	hooksDir := filepath.Join(dir, "hooks")
	mustExec(t, filepath.Join(hooksDir, "pre-apply.sh"), "#!/bin/sh\necho pre")
	mustExec(t, filepath.Join(hooksDir, "post-apply-10-reload.sh"), "#!/bin/sh\necho post")
	mustWrite(t, filepath.Join(hooksDir, "README.md"), "docs")

	var hooks []primitive.Hook
	err := primitive.WalkHooks(dir, "base", func(h primitive.Hook) error {
		hooks = append(hooks, h)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks) != 2 {
		t.Fatalf("got %d hooks, want 2", len(hooks))
	}
	phases := map[string]bool{}
	for _, h := range hooks {
		phases[h.Phase] = true
	}
	if !phases["pre-apply"] || !phases["post-apply"] {
		t.Errorf("expected pre-apply and post-apply, got %v", phases)
	}
}

func TestWalkHooks_MissingHooksDirIsNoop(t *testing.T) {
	dir := t.TempDir()
	var count int
	err := primitive.WalkHooks(dir, "base", func(h primitive.Hook) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 hooks from absent dir, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// Hook.Apply (actually runs the script)
// ---------------------------------------------------------------------------

func TestHook_Apply_RunsScript(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "sentinel")
	script := filepath.Join(dir, "post-apply.sh")
	mustExec(t, script, "#!/bin/sh\ntouch "+sentinel+"\n")

	h := primitive.NewHook("post-apply.sh", script, "base", "post-apply")
	ctx := &plan.Context{
		Ctx:    context.Background(),
		DryRun: false,
		Out:    silentPrinter{},
	}
	if err := h.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("sentinel not created — script did not run: %v", err)
	}
}

func TestHook_Apply_DryRunDoesNotExecute(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "sentinel")
	script := filepath.Join(dir, "post-apply.sh")
	mustExec(t, script, "#!/bin/sh\ntouch "+sentinel+"\n")

	h := primitive.NewHook("post-apply.sh", script, "base", "post-apply")
	ctx := &plan.Context{Ctx: context.Background(), DryRun: true, Out: silentPrinter{}}
	if err := h.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Error("dry-run should not execute the script")
	}
}

func TestHook_Apply_NonZeroExitFails(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "post-apply.sh")
	mustExec(t, script, "#!/bin/sh\nexit 1\n")

	h := primitive.NewHook("post-apply.sh", script, "base", "post-apply")
	ctx := &plan.Context{Ctx: context.Background(), DryRun: false, Out: silentPrinter{}}
	if err := h.Apply(ctx); err == nil {
		t.Error("expected error for non-zero exit, got nil")
	}
}

func TestHook_Apply_OptionalNonZeroDoesNotFail(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "post-apply.optional.sh")
	mustExec(t, script, "#!/bin/sh\nexit 1\n")

	h := primitive.NewHook("post-apply.optional.sh", script, "base", "post-apply")
	ctx := &plan.Context{Ctx: context.Background(), DryRun: false, Out: silentPrinter{}}
	if err := h.Apply(ctx); err != nil {
		t.Errorf("optional hook failure should not propagate: %v", err)
	}
}

func TestHook_Apply_EnvVarsSet(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "env.txt")
	script := filepath.Join(dir, "post-apply.sh")
	mustExec(t, script, "#!/bin/sh\necho $D0T_HOST > "+out+"\n")

	h := primitive.NewHook("post-apply.sh", script, "base", "post-apply")
	ctx := &plan.Context{
		Ctx:    context.Background(),
		DryRun: false,
		Out:    silentPrinter{},
		Repo:   &repo.Repo{Root: dir, Hostname: "myhost"},
	}
	if err := h.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "myhost\n" {
		t.Errorf("D0T_HOST = %q, want %q", string(got), "myhost\n")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

type silentPrinter struct{}

func (silentPrinter) Info(string, ...any)    {}
func (silentPrinter) Verbose(string, ...any) {}
