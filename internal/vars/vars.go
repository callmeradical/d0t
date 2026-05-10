// Package vars loads and merges template variables from vars.toml files
// across the active profile stack. Later profiles override earlier ones on
// a per-key basis.
package vars

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/callmeradical/d0t/internal/profile"
)

// Map is a flat string→string variable map used in template rendering.
// Values are always strings; non-string TOML scalars are rejected at load
// time to keep the template surface predictable.
type Map map[string]string

// Load reads vars.toml from each profile in order. Later profiles override
// keys from earlier ones. Missing vars.toml files are silently skipped.
func Load(profiles []profile.Profile) (Map, error) {
	out := make(Map)
	for _, p := range profiles {
		path := filepath.Join(p.Path, "vars.toml")
		layer, err := loadOne(path)
		if err != nil {
			return nil, fmt.Errorf("vars %s: %w", p.Name, err)
		}
		for k, v := range layer {
			out[k] = v
		}
	}
	return out, nil
}

func loadOne(path string) (Map, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	// Decode into a raw map first so we can enforce string-only values.
	var raw map[string]any
	if err := toml.Unmarshal(b, &raw); err != nil {
		return nil, err
	}

	out := make(Map, len(raw))
	for k, v := range raw {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("key %q: vars.toml values must be strings, got %T", k, v)
		}
		out[k] = s
	}
	return out, nil
}
