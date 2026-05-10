package profile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/callmeradical/d0t/internal/profile"
	"github.com/callmeradical/d0t/internal/repo"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// Resolve
// ---------------------------------------------------------------------------

func TestResolve_DefaultOrder(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "base"), 0o755))
	must(t, os.MkdirAll(filepath.Join(dir, "os", "darwin"), 0o755))

	r := &repo.Repo{Root: dir, OS: "darwin", Hostname: "no-such-host"}
	profs, err := profile.Resolve(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(profs) != 2 {
		t.Fatalf("got %d profiles, want 2: %v", len(profs), profs)
	}
	if profs[0].Name != "base" {
		t.Errorf("profs[0].Name = %q, want base", profs[0].Name)
	}
	if profs[1].Name != "os/darwin" {
		t.Errorf("profs[1].Name = %q, want os/darwin", profs[1].Name)
	}
}

func TestResolve_Override(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "work"), 0o755))

	r := &repo.Repo{Root: dir, OS: "darwin", Hostname: "mbp"}
	profs, err := profile.Resolve(r, []string{"work"})
	if err != nil {
		t.Fatal(err)
	}
	if len(profs) != 1 || profs[0].Name != "work" {
		t.Fatalf("unexpected profiles: %v", profs)
	}
}

func TestResolve_DuplicateProfileErrors(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "base"), 0o755))

	r := &repo.Repo{Root: dir}
	_, err := profile.Resolve(r, []string{"base", "base"})
	if err == nil {
		t.Fatal("expected error for duplicate profile, got nil")
	}
}

func TestResolve_MissingProfileSilentlyDropped(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "base"), 0o755))
	// hosts/no-such-host does not exist — should be silently dropped.

	r := &repo.Repo{Root: dir, OS: "linux", Hostname: "no-such-host"}
	profs, err := profile.Resolve(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(profs) != 1 || profs[0].Name != "base" {
		t.Fatalf("expected only base, got %v", profs)
	}
}

// ---------------------------------------------------------------------------
// ResolveRoots
// ---------------------------------------------------------------------------

func TestResolveRoots_Builtins(t *testing.T) {
	r := &repo.Repo{Root: t.TempDir(), Home: "/home/test"}
	roots, err := profile.ResolveRoots(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"home", "xdg", "etc", "root", "fragments"} {
		if _, ok := roots[key]; !ok {
			t.Errorf("expected root %q to be present", key)
		}
	}
	if roots["home"] != "/home/test" {
		t.Errorf("home root = %q, want /home/test", roots["home"])
	}
	if roots["etc"] != "/etc" {
		t.Errorf("etc root = %q, want /etc", roots["etc"])
	}
}

func TestResolveRoots_CustomRoot(t *testing.T) {
	r := &repo.Repo{
		Root: t.TempDir(),
		Home: "/home/test",
		Config: repo.Config{
			Roots: map[string]string{"caches": "~/.cache"},
		},
	}
	roots, err := profile.ResolveRoots(r)
	if err != nil {
		t.Fatal(err)
	}
	if roots["caches"] != "/home/test/.cache" {
		t.Errorf("caches root = %q, want /home/test/.cache", roots["caches"])
	}
}
