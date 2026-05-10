// Package repo discovers and represents a d0t dotfiles repository on disk.
//
// A repo is a directory whose immediate children are profile directories
// (base, os/<os>, hosts/<host>, plus user-defined profiles). An optional
// d0t.toml at the repo root tweaks profile resolution and declares custom
// filesystem roots.
package repo

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
)

// Config is the optional repo-root d0t.toml. All fields are optional.
type Config struct {
	// Profiles, if non-empty, overrides the default profile order
	// (base, os/<os>, hosts/<host>). Each entry is a path relative to
	// the repo root.
	Profiles []string `toml:"profiles"`

	// Roots maps profile-internal top-level dir names to absolute
	// filesystem destinations. Values undergo $VAR / ~ expansion.
	// The built-in roots (home, xdg, etc, root) can be overridden here.
	Roots map[string]string `toml:"roots"`

	// StrictPkg, if true, fails apply when a declared package manager
	// is not installed on the host. Default: warn-and-skip.
	StrictPkg bool `toml:"strict_pkg"`

	// Secrets configures the secret resolution backend for template
	// rendering.
	Secrets SecretsConfig `toml:"secrets"`

	// Self configures d0t self-update behaviour.
	Self SelfConfig `toml:"self"`
}

// SelfConfig controls `d0t update` (self-update).
type SelfConfig struct {
	// Source is the path to the d0t source repo used to rebuild the binary.
	// Supports ~ expansion. Example: "~/Dev/d0t"
	Source string `toml:"source"`
}

// SecretsConfig controls how {{ secret "..." }} calls in templates are
// resolved.
type SecretsConfig struct {
	// Backend is the default backend for bare keys (no scheme prefix).
	// Recognised values: "op", "keychain", "env", "pass", "none" (default).
	// Keys with an explicit scheme prefix (op://, keychain://, env://, pass://)
	// always route to the appropriate backend regardless of this setting.
	Backend string `toml:"backend"`
}

// Repo is an opened dotfiles repository.
type Repo struct {
	// Root is the absolute path to the repo directory.
	Root string

	// Config is the parsed d0t.toml (or zero value if absent).
	Config Config

	// Hostname of the current machine, lower-cased.
	Hostname string

	// OS is runtime.GOOS.
	OS string

	// Arch is runtime.GOARCH.
	Arch string

	// User is the current OS user.
	User string

	// Home is the current user's home directory.
	Home string
}

// Open discovers the repo. Resolution order:
//  1. explicit path argument (if non-empty)
//  2. $D0T_REPO
//  3. $HOME/.d0t
//
// The chosen path must exist and be a directory.
func Open(explicit string) (*Repo, error) {
	path, err := resolveRoot(explicit)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("repo path %s: %w", path, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("repo path %s is not a directory", path)
	}

	r := &Repo{
		Root: path,
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}

	if err := r.loadConfig(); err != nil {
		return nil, err
	}
	if err := r.loadHostInfo(); err != nil {
		return nil, err
	}

	return r, nil
}

func resolveRoot(explicit string) (string, error) {
	candidate := explicit
	if candidate == "" {
		candidate = os.Getenv("D0T_REPO")
	}
	if candidate == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate home directory: %w", err)
		}
		candidate = filepath.Join(home, ".d0t")
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve repo path: %w", err)
	}
	return abs, nil
}

func (r *Repo) loadConfig() error {
	path := filepath.Join(r.Root, "d0t.toml")
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := toml.Unmarshal(b, &r.Config); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func (r *Repo) loadHostInfo() error {
	host, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("hostname: %w", err)
	}
	// Strip trailing .local on macOS so hosts/<short> works.
	if i := indexByte(host, '.'); i >= 0 {
		host = host[:i]
	}
	r.Hostname = host

	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("current user: %w", err)
	}
	r.User = u.Username
	r.Home = u.HomeDir
	return nil
}

// ProfilePath returns the absolute filesystem path of a profile, or
// empty string if the profile directory does not exist.
func (r *Repo) ProfilePath(name string) string {
	p := filepath.Join(r.Root, name)
	info, err := os.Stat(p)
	if err != nil || !info.IsDir() {
		return ""
	}
	return p
}

// indexByte is a tiny helper to avoid importing strings here.
func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
