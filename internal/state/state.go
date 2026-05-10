// Package state manages the d0t state file, which records every filesystem
// target that d0t has created or modified. The state is used by:
//
//   - `d0t remove` — know exactly what to tear down
//   - conflict detection — distinguish "d0t's stale copy" from a pre-existing
//     unrelated file
//   - drift detection — surface targets that were modified outside d0t
//
// The state is stored as a JSON file at:
//
//	${XDG_STATE_HOME:-~/.local/state}/d0t/state.json
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Entry records everything d0t knows about one managed target path.
type Entry struct {
	// Kind is the primitive that created this target: "link", "copy",
	// "template", or "fragment".
	Kind string `json:"kind"`
	// Source is the repo-relative path of the source file.
	Source string `json:"source"`
	// Hash is the SHA-256 hex digest of the content written to the target
	// (meaningful for copy, template, and fragment; empty for link).
	Hash string `json:"hash,omitempty"`
	// Marker is the fragment marker (meaningful for fragment only).
	Marker string `json:"marker,omitempty"`
}

// file is the on-disk JSON envelope.
type file struct {
	Version int               `json:"version"`
	Entries map[string]Entry  `json:"entries"`
}

// State is the in-memory state. Use New to create one, Load to read from
// disk, and Save to persist.
type State struct {
	entries map[string]Entry
}

// New returns an empty State.
func New() *State {
	return &State{entries: make(map[string]Entry)}
}

// Set records or updates the entry for target.
func (s *State) Set(target string, e Entry) {
	s.entries[target] = e
}

// Get returns the entry for target and whether it exists.
func (s *State) Get(target string) (Entry, bool) {
	e, ok := s.entries[target]
	return e, ok
}

// Delete removes the entry for target.
func (s *State) Delete(target string) {
	delete(s.entries, target)
}

// Targets returns all managed target paths in sorted order.
func (s *State) Targets() []string {
	targets := make([]string, 0, len(s.entries))
	for t := range s.entries {
		targets = append(targets, t)
	}
	sort.Strings(targets)
	return targets
}

// Len returns the number of tracked targets.
func (s *State) Len() int { return len(s.entries) }

// Path returns the canonical path of the state file. It honours
// $XDG_STATE_HOME and falls back to ~/.local/state.
func Path(home string) string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "d0t", "state.json")
}

// Load reads the state file at path. If the file does not exist an empty
// State is returned without error.
func Load(path string) (*State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return New(), nil
		}
		return nil, fmt.Errorf("read state %s: %w", path, err)
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parse state %s: %w", path, err)
	}
	s := &State{entries: f.Entries}
	if s.entries == nil {
		s.entries = make(map[string]Entry)
	}
	return s, nil
}

// Save atomically writes the state to path, creating parent directories as
// needed.
func Save(path string, s *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	f := file{
		Version: 1,
		Entries: s.entries,
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	b = append(b, '\n')

	// Atomic write via temp file + rename.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".d0t-state-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
