package state_test

import (
	"os"
	"path/filepath"
	"testing"

	"d0t/internal/state"
)

// ---------------------------------------------------------------------------
// StatePath
// ---------------------------------------------------------------------------

func TestStatePath_UsesXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/custom/state")
	got := state.Path("/home/user")
	want := "/custom/state/d0t/state.json"
	if got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

func TestStatePath_FallsBackToLocalState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	got := state.Path("/home/user")
	want := "/home/user/.local/state/d0t/state.json"
	if got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Load / Save round-trip
// ---------------------------------------------------------------------------

func TestLoadSave_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := state.New()
	s.Set("/home/user/.zshrc", state.Entry{
		Kind:   "link",
		Source: "base/home/.zshrc",
	})
	s.Set("/home/user/.gitconfig", state.Entry{
		Kind:   "template",
		Source: "base/home/.gitconfig.tmpl",
		Hash:   "abc123",
	})

	if err := state.Save(path, s); err != nil {
		t.Fatal(err)
	}

	loaded, err := state.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	e, ok := loaded.Get("/home/user/.zshrc")
	if !ok {
		t.Fatal("expected .zshrc entry")
	}
	if e.Kind != "link" || e.Source != "base/home/.zshrc" {
		t.Errorf("unexpected entry: %+v", e)
	}

	e2, ok := loaded.Get("/home/user/.gitconfig")
	if !ok {
		t.Fatal("expected .gitconfig entry")
	}
	if e2.Hash != "abc123" {
		t.Errorf("hash = %q, want abc123", e2.Hash)
	}
}

func TestLoad_MissingFileReturnsEmpty(t *testing.T) {
	s, err := state.Load("/no/such/file/state.json")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil State")
	}
	_, ok := s.Get("/anything")
	if ok {
		t.Error("expected empty state, got an entry")
	}
}

func TestLoad_InvalidJSONErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := state.Load(path)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

// ---------------------------------------------------------------------------
// Set / Get / Delete
// ---------------------------------------------------------------------------

func TestState_Delete(t *testing.T) {
	s := state.New()
	s.Set("/foo", state.Entry{Kind: "link"})
	s.Delete("/foo")
	_, ok := s.Get("/foo")
	if ok {
		t.Error("expected entry to be deleted")
	}
}

func TestState_SetOverwrites(t *testing.T) {
	s := state.New()
	s.Set("/foo", state.Entry{Kind: "link", Hash: "old"})
	s.Set("/foo", state.Entry{Kind: "copy", Hash: "new"})
	e, ok := s.Get("/foo")
	if !ok {
		t.Fatal("expected entry")
	}
	if e.Kind != "copy" || e.Hash != "new" {
		t.Errorf("expected overwritten entry, got %+v", e)
	}
}

func TestState_Targets(t *testing.T) {
	s := state.New()
	s.Set("/b", state.Entry{Kind: "link"})
	s.Set("/a", state.Entry{Kind: "copy"})
	s.Set("/c", state.Entry{Kind: "template"})

	targets := s.Targets()
	if len(targets) != 3 {
		t.Fatalf("got %d targets, want 3", len(targets))
	}
	// Should be sorted.
	if targets[0] != "/a" || targets[1] != "/b" || targets[2] != "/c" {
		t.Errorf("targets not sorted: %v", targets)
	}
}

// ---------------------------------------------------------------------------
// Save creates parent directories
// ---------------------------------------------------------------------------

func TestSave_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", "nested", "state.json")

	s := state.New()
	s.Set("/foo", state.Entry{Kind: "link"})
	if err := state.Save(path, s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("state file not created: %v", err)
	}
}
