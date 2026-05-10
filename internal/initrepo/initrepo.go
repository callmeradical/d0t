// Package initrepo implements the `d0t init` and `d0t adopt` operations.
package initrepo

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// scaffoldDirs is the set of directories created by Init. It mirrors the
// canonical repo layout described in DESIGN.md.
var scaffoldDirs = []string{
	"base/home",
	"base/xdg",
	"base/hooks",
	"base/fragments",
	"os/darwin",
	"os/linux",
	"hosts",
}

// Note: packages are declared directly in d0tfile (brew/cask/tap/apt/mas
// keywords), so there is no separate packages.toml scaffold.

// scaffoldFiles are files written by Init only when they do not already
// exist. Keys are repo-relative paths; values are the file contents.
var scaffoldFiles = map[string]string{
	"d0t.toml": `# d0t configuration — see README.md for the full reference.

# profiles = ["base", "os/darwin", "hosts/mymachine"]

# [roots]
# caches = "${XDG_CACHE_HOME:-$HOME/.cache}"

# [secrets]
# Default backend for bare keys in {{ secret "key" }} template calls.
# Keys with explicit prefixes (op://, keychain://, env://, pass://) always
# route to the correct backend regardless of this setting.
# backend = "op"        # 1Password CLI (op read op://Vault/Item/Field)
# backend = "keychain"  # macOS Keychain (security find-generic-password)
# backend = "env"       # environment variables
# backend = "pass"      # pass password manager
`,
	".gitignore": `# machine-local — never commit
*.d0t-backup-*
`,
	"base/d0tfile": `# base profile — declare every resource explicitly
# See: d0t help or README.md for syntax

# --- files -------------------------------------------------------
# link   home/.zshrc
# link   home/.zshenv
# link   xdg/nvim
# copy   home/.ssh/config   mode=0600
# tmpl   home/.gitconfig

# --- fragments ---------------------------------------------------
# fragment fragments/path-export.fragment

# --- hooks -------------------------------------------------------
# hook post-apply hooks/post-apply.sh

# --- packages ----------------------------------------------------
# brew  ripgrep neovim bat fzf lazygit starship tmux
# cask  ghostty raycast
# tap   homebrew/cask-fonts
# apt   ripgrep neovim bat
# mas   497799835  Xcode
`,
	"base/vars.toml": `# Template variables — access as .Vars.<key> inside .tmpl files.
# Values must be strings.
# email = "you@example.com"
`,
}

// Init scaffolds a new d0t repository at dir. It creates the standard profile
// directories and writes template files for d0t.toml, .gitignore, and starter
// vars/packages manifests. Existing files are never overwritten.
func Init(dir string) error {
	for _, d := range scaffoldDirs {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	for relPath, content := range scaffoldFiles {
		abs := filepath.Join(dir, relPath)
		if err := writeIfAbsent(abs, content); err != nil {
			return fmt.Errorf("write %s: %w", relPath, err)
		}
	}
	return nil
}

// Adopt moves an existing file at src into the repo at dest, then replaces src
// with a symlink pointing to dest. It is the inverse of a manual copy: the
// user places their existing config under d0t management without losing it.
//
// Preconditions:
//   - src must exist and be a regular file (not a symlink)
//   - dest must not already exist
func Adopt(src, dest string) error {
	// Validate src.
	srcInfo, err := os.Lstat(src)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("adopt: source %s does not exist", src)
		}
		return fmt.Errorf("adopt: stat %s: %w", src, err)
	}
	if srcInfo.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("adopt: source %s is already a symlink", src)
	}
	if !srcInfo.Mode().IsRegular() {
		return fmt.Errorf("adopt: source %s is not a regular file", src)
	}

	// Validate dest.
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("adopt: destination %s already exists", dest)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("adopt: stat dest %s: %w", dest, err)
	}

	// Create parent directory of dest.
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("adopt: create parent of %s: %w", dest, err)
	}

	// Move src → dest.
	if err := os.Rename(src, dest); err != nil {
		// Rename fails across filesystems; fall back to copy + remove.
		if err2 := copyFile(src, dest, srcInfo.Mode()); err2 != nil {
			return fmt.Errorf("adopt: copy %s -> %s: %w", src, dest, err2)
		}
		if err2 := os.Remove(src); err2 != nil {
			return fmt.Errorf("adopt: remove original %s: %w", src, err2)
		}
	}

	// Replace original path with a symlink → dest.
	if err := os.Symlink(dest, src); err != nil {
		return fmt.Errorf("adopt: create symlink %s -> %s: %w", src, dest, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func writeIfAbsent(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		return nil // already exists
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func copyFile(src, dest string, mode fs.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, data, mode)
}
