package secrets_test

import (
	"testing"

	"d0t/internal/secrets"
)

// ---------------------------------------------------------------------------
// Mock backend for testing
// ---------------------------------------------------------------------------

type mockBackend struct {
	values map[string]string
	calls  []string
}

func (m *mockBackend) Get(key string) (string, error) {
	m.calls = append(m.calls, key)
	if v, ok := m.values[key]; ok {
		return v, nil
	}
	return "", secrets.ErrNotFound(key)
}

// ---------------------------------------------------------------------------
// Env backend
// ---------------------------------------------------------------------------

func TestEnvBackend_ReadsEnvVar(t *testing.T) {
	t.Setenv("MY_SECRET", "hunter2")
	b := secrets.NewEnvBackend()
	got, err := b.Get("MY_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hunter2" {
		t.Errorf("got %q, want %q", got, "hunter2")
	}
}

func TestEnvBackend_MissingVarErrors(t *testing.T) {
	b := secrets.NewEnvBackend()
	_, err := b.Get("D0T_TEST_DEFINITELY_NOT_SET_XYZ")
	if err == nil {
		t.Error("expected error for missing env var, got nil")
	}
}

// ---------------------------------------------------------------------------
// Router: prefix-based backend selection
// ---------------------------------------------------------------------------

func TestRouter_OpPrefix(t *testing.T) {
	opCalled := false
	router := secrets.NewRouter(secrets.RouterConfig{
		Op: secrets.BackendFunc(func(key string) (string, error) {
			opCalled = true
			return "op-value", nil
		}),
	})
	val, err := router.Get("op://Personal/Item/field")
	if err != nil {
		t.Fatal(err)
	}
	if !opCalled {
		t.Error("expected op backend to be called for op:// prefix")
	}
	if val != "op-value" {
		t.Errorf("val = %q, want op-value", val)
	}
}

func TestRouter_KeychainPrefix(t *testing.T) {
	keychainCalled := false
	router := secrets.NewRouter(secrets.RouterConfig{
		Keychain: secrets.BackendFunc(func(key string) (string, error) {
			keychainCalled = true
			return "keychain-value", nil
		}),
	})
	_, err := router.Get("keychain://d0t/my-key")
	if err != nil {
		t.Fatal(err)
	}
	if !keychainCalled {
		t.Error("expected keychain backend for keychain:// prefix")
	}
}

func TestRouter_EnvPrefix(t *testing.T) {
	t.Setenv("GIT_EMAIL", "user@example.com")
	router := secrets.NewRouter(secrets.RouterConfig{
		Env: secrets.NewEnvBackend(),
	})
	val, err := router.Get("env://GIT_EMAIL")
	if err != nil {
		t.Fatal(err)
	}
	if val != "user@example.com" {
		t.Errorf("val = %q, want user@example.com", val)
	}
}

func TestRouter_BareKeyUsesDefault(t *testing.T) {
	defaultCalled := false
	router := secrets.NewRouter(secrets.RouterConfig{
		Default: secrets.BackendFunc(func(key string) (string, error) {
			defaultCalled = true
			if key != "my-token" {
				return "", secrets.ErrNotFound(key)
			}
			return "default-value", nil
		}),
	})
	val, err := router.Get("my-token")
	if err != nil {
		t.Fatal(err)
	}
	if !defaultCalled {
		t.Error("expected default backend for bare key")
	}
	if val != "default-value" {
		t.Errorf("val = %q, want default-value", val)
	}
}

func TestRouter_NilBackendErrors(t *testing.T) {
	// Router with no backends configured.
	router := secrets.NewRouter(secrets.RouterConfig{})
	_, err := router.Get("op://Personal/Item/field")
	if err == nil {
		t.Error("expected error when op backend not configured, got nil")
	}
}

// ---------------------------------------------------------------------------
// Cache: same key only hits backend once
// ---------------------------------------------------------------------------

func TestCachedBackend_DeduplicatesCalls(t *testing.T) {
	calls := 0
	inner := secrets.BackendFunc(func(key string) (string, error) {
		calls++
		return "value", nil
	})
	cached := secrets.NewCachedBackend(inner)

	for i := 0; i < 5; i++ {
		if _, err := cached.Get("same-key"); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Errorf("backend called %d times, want 1", calls)
	}
}

func TestCachedBackend_DifferentKeysMakeMultipleCalls(t *testing.T) {
	calls := 0
	inner := secrets.BackendFunc(func(key string) (string, error) {
		calls++
		return "v-" + key, nil
	})
	cached := secrets.NewCachedBackend(inner)

	keys := []string{"a", "b", "c"}
	for _, k := range keys {
		if _, err := cached.Get(k); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 3 {
		t.Errorf("expected 3 calls for 3 keys, got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// NewFromConfig
// ---------------------------------------------------------------------------

func TestNewFromConfig_NoneBackend(t *testing.T) {
	b, err := secrets.NewFromConfig("none", nil)
	if err != nil {
		t.Fatal(err)
	}
	if b != nil {
		t.Error("expected nil backend for 'none'")
	}
}

func TestNewFromConfig_EnvBackend(t *testing.T) {
	b, err := secrets.NewFromConfig("env", nil)
	if err != nil {
		t.Fatal(err)
	}
	if b == nil {
		t.Error("expected non-nil backend for 'env'")
	}
}

func TestNewFromConfig_UnknownErrors(t *testing.T) {
	_, err := secrets.NewFromConfig("unicorn", nil)
	if err == nil {
		t.Error("expected error for unknown backend, got nil")
	}
}
