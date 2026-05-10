// Package secrets provides secret resolution for d0t template rendering.
//
// The central type is Backend, a simple Get(key) interface. Four backends are
// provided: Op (1Password CLI), Keychain (macOS Security framework), Env
// (environment variables), and Pass (pass password manager).
//
// A Router selects the backend by key prefix:
//
//	op://Vault/Item/Field  → Op backend
//	keychain://service/account → Keychain backend
//	env://VAR_NAME         → Env backend
//	pass://path/to/secret  → Pass backend
//	bare-key               → configured default backend
//
// A CachedBackend wraps any backend and deduplicates lookups within a single
// apply run so that `op read` is not called more than once per unique key.
package secrets

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Backend resolves a secret by key.
type Backend interface {
	Get(key string) (string, error)
}

// BackendFunc adapts a function to the Backend interface.
type BackendFunc func(key string) (string, error)

func (f BackendFunc) Get(key string) (string, error) { return f(key) }

// NotFoundError is returned when a key does not exist in the backend.
type NotFoundError struct{ Key string }

func (e NotFoundError) Error() string { return fmt.Sprintf("secret %q not found", e.Key) }

// ErrNotFound constructs a NotFoundError.
func ErrNotFound(key string) error { return NotFoundError{Key: key} }

// ---------------------------------------------------------------------------
// Env backend
// ---------------------------------------------------------------------------

type envBackend struct{}

// NewEnvBackend returns a Backend that reads from environment variables.
// The key is used as-is as the variable name.
func NewEnvBackend() Backend { return envBackend{} }

func (envBackend) Get(key string) (string, error) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return "", ErrNotFound(key)
	}
	return v, nil
}

// ---------------------------------------------------------------------------
// Op (1Password) backend
// ---------------------------------------------------------------------------

type opBackend struct{}

// NewOpBackend returns a Backend that resolves secrets via `op read`.
// The key must be a full 1Password secret reference: op://Vault/Item/Field.
func NewOpBackend() Backend { return opBackend{} }

func (opBackend) Get(key string) (string, error) {
	// Strip scheme if present so the key is always passed as-is to op.
	// op read expects the full op:// reference.
	out, err := exec.Command("op", "read", "--no-newline", key).Output()
	if err != nil {
		return "", fmt.Errorf("op read %q: %w", key, err)
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

// ---------------------------------------------------------------------------
// Keychain (macOS) backend
// ---------------------------------------------------------------------------

type keychainBackend struct{}

// NewKeychainBackend returns a Backend that reads from the macOS Keychain via
// the `security` command. Key format: "service/account" or just "service".
func NewKeychainBackend() Backend { return keychainBackend{} }

func (keychainBackend) Get(key string) (string, error) {
	// Strip scheme prefix if present.
	key = strings.TrimPrefix(key, "keychain://")
	service, account, _ := strings.Cut(key, "/")
	args := []string{"find-generic-password", "-s", service, "-w"}
	if account != "" {
		args = append(args, "-a", account)
	}
	out, err := exec.Command("security", args...).Output()
	if err != nil {
		return "", fmt.Errorf("keychain %q: %w", key, err)
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

// ---------------------------------------------------------------------------
// Pass backend
// ---------------------------------------------------------------------------

type passBackend struct{}

// NewPassBackend returns a Backend that resolves secrets via the `pass` CLI.
func NewPassBackend() Backend { return passBackend{} }

func (passBackend) Get(key string) (string, error) {
	key = strings.TrimPrefix(key, "pass://")
	out, err := exec.Command("pass", "show", key).Output()
	if err != nil {
		return "", fmt.Errorf("pass show %q: %w", key, err)
	}
	// pass show prints the secret on the first line.
	line, _, _ := strings.Cut(string(out), "\n")
	return strings.TrimRight(line, "\r"), nil
}

// ---------------------------------------------------------------------------
// Router
// ---------------------------------------------------------------------------

// RouterConfig holds one optional backend per prefix. Default is used for
// bare keys (no recognized scheme prefix).
type RouterConfig struct {
	Op       Backend
	Keychain Backend
	Env      Backend
	Pass     Backend
	Default  Backend
}

type router struct {
	cfg RouterConfig
}

// NewRouter returns a Backend that dispatches by key prefix.
func NewRouter(cfg RouterConfig) Backend {
	return &router{cfg: cfg}
}

func (r *router) Get(key string) (string, error) {
	switch {
	case strings.HasPrefix(key, "op://"):
		if r.cfg.Op == nil {
			return "", fmt.Errorf("secret %q requires op backend but none is configured (set [secrets] backend = \"op\" in d0t.toml)", key)
		}
		return r.cfg.Op.Get(key)

	case strings.HasPrefix(key, "keychain://"):
		if r.cfg.Keychain == nil {
			return "", fmt.Errorf("secret %q requires keychain backend but none is configured", key)
		}
		return r.cfg.Keychain.Get(key)

	case strings.HasPrefix(key, "env://"):
		b := r.cfg.Env
		if b == nil {
			b = NewEnvBackend()
		}
		return b.Get(strings.TrimPrefix(key, "env://"))

	case strings.HasPrefix(key, "pass://"):
		if r.cfg.Pass == nil {
			return "", fmt.Errorf("secret %q requires pass backend but none is configured", key)
		}
		return r.cfg.Pass.Get(key)

	default:
		if r.cfg.Default == nil {
			return "", fmt.Errorf("secret %q: no default backend configured (set [secrets] backend in d0t.toml)", key)
		}
		return r.cfg.Default.Get(key)
	}
}

// ---------------------------------------------------------------------------
// Cached backend
// ---------------------------------------------------------------------------

type cachedBackend struct {
	inner Backend
	mu    sync.Mutex
	cache map[string]cachedEntry
}

type cachedEntry struct {
	value string
	err   error
}

// NewCachedBackend wraps a Backend and deduplicates lookups. Each unique key
// is resolved at most once per instance. Use a fresh CachedBackend per apply
// run.
func NewCachedBackend(inner Backend) Backend {
	return &cachedBackend{
		inner: inner,
		cache: make(map[string]cachedEntry),
	}
}

func (c *cachedBackend) Get(key string) (string, error) {
	c.mu.Lock()
	if e, ok := c.cache[key]; ok {
		c.mu.Unlock()
		return e.value, e.err
	}
	c.mu.Unlock()

	v, err := c.inner.Get(key)

	c.mu.Lock()
	c.cache[key] = cachedEntry{value: v, err: err}
	c.mu.Unlock()

	return v, err
}

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

// NewFromConfig constructs the appropriate Backend from a backend name string
// and an optional default backend override. Returns nil for "none" or "".
// The returned backend is wrapped in a CachedBackend.
func NewFromConfig(backendName string, defaultOverride Backend) (Backend, error) {
	switch strings.ToLower(strings.TrimSpace(backendName)) {
	case "", "none":
		return nil, nil

	case "op":
		cfg := RouterConfig{
			Op:       NewOpBackend(),
			Keychain: NewKeychainBackend(),
			Env:      NewEnvBackend(),
			Pass:     NewPassBackend(),
			Default:  NewOpBackend(),
		}
		if defaultOverride != nil {
			cfg.Default = defaultOverride
		}
		return NewCachedBackend(NewRouter(cfg)), nil

	case "keychain":
		cfg := RouterConfig{
			Op:       NewOpBackend(),
			Keychain: NewKeychainBackend(),
			Env:      NewEnvBackend(),
			Pass:     NewPassBackend(),
			Default:  NewKeychainBackend(),
		}
		if defaultOverride != nil {
			cfg.Default = defaultOverride
		}
		return NewCachedBackend(NewRouter(cfg)), nil

	case "env":
		cfg := RouterConfig{
			Op:       NewOpBackend(),
			Keychain: NewKeychainBackend(),
			Env:      NewEnvBackend(),
			Pass:     NewPassBackend(),
			Default:  NewEnvBackend(),
		}
		if defaultOverride != nil {
			cfg.Default = defaultOverride
		}
		return NewCachedBackend(NewRouter(cfg)), nil

	case "pass":
		cfg := RouterConfig{
			Op:       NewOpBackend(),
			Keychain: NewKeychainBackend(),
			Env:      NewEnvBackend(),
			Pass:     NewPassBackend(),
			Default:  NewPassBackend(),
		}
		if defaultOverride != nil {
			cfg.Default = defaultOverride
		}
		return NewCachedBackend(NewRouter(cfg)), nil

	default:
		return nil, fmt.Errorf("unknown secrets backend %q (want: op, keychain, env, pass, none)", backendName)
	}
}
