// Package fsutil contains small filesystem helpers used by d0t primitives.
// Operations here aim to be safe (atomic where possible) and idempotent.
package fsutil

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// EnsureParentDir creates the parent directory of path with mode 0755.
func EnsureParentDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}

// AtomicWrite writes data to path atomically: temp file in the target
// directory, then rename. Mode is applied before rename.
func AtomicWrite(path string, data []byte, mode fs.FileMode) error {
	if err := EnsureParentDir(path); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".d0t-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	cleanup := func() { _ = os.Remove(tmp) }

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		cleanup()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		cleanup()
		return err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// SymlinkTarget returns the destination of the symlink at path, or
// ("", false, nil) if path is not a symlink (including when it does not
// exist).
func SymlinkTarget(path string) (target string, isLink bool, err error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		return "", false, nil
	}
	t, err := os.Readlink(path)
	if err != nil {
		return "", true, err
	}
	return t, true, nil
}

// HashFile returns the hex-encoded SHA-256 of a regular file's contents.
// Returns ("", nil) if the file does not exist.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// HashBytes returns the hex-encoded SHA-256 of a byte slice.
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// BackupPath returns a sibling path with a .d0t-backup-<timestamp> suffix
// suitable for moving an unrelated file out of the way.
func BackupPath(path string) string {
	return fmt.Sprintf("%s.d0t-backup-%s", path, time.Now().UTC().Format("20060102-150405"))
}

// ReplaceWith removes whatever exists at path (file, link, empty dir) and
// returns nil if the path was already absent. Non-empty directories are
// not removed; use Adopt or --force semantics in the caller.
func ReplaceWith(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.IsDir() && info.Mode()&fs.ModeSymlink == 0 {
		return os.Remove(path) // will fail if non-empty, intentionally
	}
	return os.Remove(path)
}
