// Package profile resolves the active profile list for a host and maps
// profile-internal directory names to absolute filesystem destinations.
//
// A profile is a directory under the repo root. By convention the active
// profiles for a host are:
//
//	base
//	os/<runtime.GOOS>
//	hosts/<hostname>
//
// Any of these may be absent. The user can override the entire list via
// d0t.toml's `profiles = [...]` field.
package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"d0t/internal/repo"
)

// Profile is a single layer in the resolved stack.
type Profile struct {
	// Name is the repo-relative path of the profile (e.g. "base",
	// "os/darwin", "hosts/work-mbp").
	Name string
	// Path is the absolute filesystem path of the profile directory.
	Path string
}

// Resolve returns the ordered list of active profiles for the host. If
// override is non-empty it replaces the default order. Profiles whose
// directories do not exist are silently dropped.
func Resolve(r *repo.Repo, override []string) ([]Profile, error) {
	names := override
	if len(names) == 0 {
		if cfg := r.Config.Profiles; len(cfg) > 0 {
			names = cfg
		} else {
			names = defaultProfiles(r)
		}
	}

	out := make([]Profile, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.Trim(name, "/")
		if name == "" {
			continue
		}
		if seen[name] {
			return nil, fmt.Errorf("profile %q listed twice", name)
		}
		seen[name] = true
		path := r.ProfilePath(name)
		if path == "" {
			continue // absent profiles are silently skipped
		}
		out = append(out, Profile{Name: name, Path: path})
	}
	return out, nil
}

func defaultProfiles(r *repo.Repo) []string {
	out := []string{"base"}
	if r.OS != "" {
		out = append(out, "os/"+r.OS)
	}
	if r.Hostname != "" {
		out = append(out, "hosts/"+r.Hostname)
	}
	return out
}

// ResolveRoots returns the active set of filesystem roots for a repo,
// merging the built-in defaults with d0t.toml [roots] overrides.
// The path values are expanded ($VAR and leading ~).
func ResolveRoots(r *repo.Repo) (map[string]string, error) {
	roots := map[string]string{
		"home":      r.Home,
		"xdg":       xdgConfig(r),
		"etc":       "/etc",
		"root":      "/",
		"fragments": "__fragments__", // target comes from frontmatter, not a real root path
	}
	for k, v := range r.Config.Roots {
		expanded, err := expand(v, r)
		if err != nil {
			return nil, fmt.Errorf("root %q: %w", k, err)
		}
		roots[k] = expanded
	}
	return roots, nil
}

func xdgConfig(r *repo.Repo) string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	return filepath.Join(r.Home, ".config")
}

func expand(s string, r *repo.Repo) (string, error) {
	if strings.HasPrefix(s, "~/") {
		s = filepath.Join(r.Home, s[2:])
	} else if s == "~" {
		s = r.Home
	}
	return os.ExpandEnv(s), nil
}
