package vars_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/callmeradical/d0t/internal/profile"
	"github.com/callmeradical/d0t/internal/vars"
)

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
// Load: single profile
// ---------------------------------------------------------------------------

func TestLoad_SingleProfile(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "base", "vars.toml"), `
email = "user@example.com"
editor = "nvim"
`)
	p := profile.Profile{Name: "base", Path: filepath.Join(dir, "base")}
	v, err := vars.Load([]profile.Profile{p})
	if err != nil {
		t.Fatal(err)
	}
	if v["email"] != "user@example.com" {
		t.Errorf("email = %q, want %q", v["email"], "user@example.com")
	}
	if v["editor"] != "nvim" {
		t.Errorf("editor = %q, want %q", v["editor"], "nvim")
	}
}

// ---------------------------------------------------------------------------
// Load: later profile overrides earlier
// ---------------------------------------------------------------------------

func TestLoad_LaterOverrides(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "base", "vars.toml"), `
email = "base@example.com"
editor = "vim"
`)
	mustWrite(t, filepath.Join(dir, "hosts", "work", "vars.toml"), `
email = "work@company.com"
`)
	profs := []profile.Profile{
		{Name: "base", Path: filepath.Join(dir, "base")},
		{Name: "hosts/work", Path: filepath.Join(dir, "hosts", "work")},
	}
	v, err := vars.Load(profs)
	if err != nil {
		t.Fatal(err)
	}
	// host overrides base email
	if v["email"] != "work@company.com" {
		t.Errorf("email = %q, want %q", v["email"], "work@company.com")
	}
	// base editor is inherited
	if v["editor"] != "vim" {
		t.Errorf("editor = %q, want %q", v["editor"], "vim")
	}
}

// ---------------------------------------------------------------------------
// Load: missing vars.toml is not an error
// ---------------------------------------------------------------------------

func TestLoad_MissingVarsTomlIsNoop(t *testing.T) {
	dir := t.TempDir()
	p := profile.Profile{Name: "base", Path: filepath.Join(dir, "base")}
	// base dir doesn't even exist
	v, err := vars.Load([]profile.Profile{p})
	if err != nil {
		t.Fatalf("unexpected error for missing vars.toml: %v", err)
	}
	if len(v) != 0 {
		t.Errorf("expected empty vars map, got %v", v)
	}
}

// ---------------------------------------------------------------------------
// Load: invalid TOML returns an error
// ---------------------------------------------------------------------------

func TestLoad_InvalidTomlErrors(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "base", "vars.toml"), `not = valid = toml`)
	p := profile.Profile{Name: "base", Path: filepath.Join(dir, "base")}
	_, err := vars.Load([]profile.Profile{p})
	if err == nil {
		t.Error("expected error for invalid TOML, got nil")
	}
}
