package ai

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretManagerLifecycleAndAAD(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "ai.key")
	manager := NewSecretManager(path)
	if err := manager.Initialize(0); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("key mode = %o, want 600", got)
	}

	first, err := manager.Encrypt("provider-1", ProtocolOpenAIChatCompletions, "secret-marker")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	second, err := manager.Encrypt("provider-1", ProtocolOpenAIChatCompletions, "secret-marker")
	if err != nil {
		t.Fatalf("Encrypt second: %v", err)
	}
	if first == second || strings.Contains(first, "secret-marker") || !strings.HasPrefix(first, "v1:") {
		t.Fatalf("ciphertexts are not versioned/randomized: %q %q", first, second)
	}
	plaintext, err := manager.Decrypt("provider-1", ProtocolOpenAIChatCompletions, first)
	if err != nil || plaintext != "secret-marker" {
		t.Fatalf("Decrypt = %q, %v", plaintext, err)
	}
	if _, err := manager.Decrypt("provider-2", ProtocolOpenAIChatCompletions, first); !errors.Is(err, ErrSecretDecrypt) {
		t.Fatalf("wrong provider error = %v, want ErrSecretDecrypt", err)
	}
}

func TestSecretManagerRefusesMissingOrWeakExistingKey(t *testing.T) {
	missing := NewSecretManager(filepath.Join(t.TempDir(), "ai.key"))
	if err := missing.Initialize(1); !errors.Is(err, ErrMasterKeyUnavailable) {
		t.Fatalf("missing key error = %v", err)
	}

	weakPath := filepath.Join(t.TempDir(), "ai.key")
	if err := os.WriteFile(weakPath, make([]byte, masterKeySize), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := NewSecretManager(weakPath).Initialize(0); !errors.Is(err, ErrMasterKeyUnavailable) {
		t.Fatalf("weak key error = %v", err)
	}
}
