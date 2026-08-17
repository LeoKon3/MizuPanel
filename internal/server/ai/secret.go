package ai

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	ErrMasterKeyUnavailable = errors.New("AI 主密钥不可用，请恢复数据库配套的 ai.key")
	ErrSecretDecrypt        = errors.New("AI Provider 密钥无法解密，请恢复正确的 ai.key")
)

const masterKeySize = 32

type SecretManager struct {
	path string
	mu   sync.RWMutex
	key  []byte
}

func NewSecretManager(path string) *SecretManager {
	return &SecretManager{path: path}
}

// Initialize creates a key only for a database with no Provider rows. This
// prevents a lost key file from being silently replaced beside ciphertext.
func (m *SecretManager) Initialize(encryptedSecretCount int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.key) == masterKeySize {
		return nil
	}
	info, statErr := os.Lstat(m.path)
	if statErr == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0) {
		return ErrMasterKeyUnavailable
	}
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return ErrMasterKeyUnavailable
	}
	content, err := os.ReadFile(m.path)
	if err == nil {
		if len(content) != masterKeySize {
			return ErrMasterKeyUnavailable
		}
		m.key = append([]byte(nil), content...)
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) || encryptedSecretCount > 0 {
		return ErrMasterKeyUnavailable
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return fmt.Errorf("create AI key directory: %w", err)
	}
	key := make([]byte, masterKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return fmt.Errorf("generate AI master key: %w", err)
	}
	file, err := os.OpenFile(m.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create AI master key: %w", err)
	}
	if _, err := file.Write(key); err != nil {
		_ = file.Close()
		return fmt.Errorf("write AI master key: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync AI master key: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close AI master key: %w", err)
	}
	m.key = key
	return nil
}

func (m *SecretManager) Encrypt(providerID, protocol, plaintext string) (string, error) {
	return m.encrypt(plaintext, secretAAD(providerID, protocol))
}

func (m *SecretManager) Decrypt(providerID, protocol, encrypted string) (string, error) {
	return m.decrypt(encrypted, secretAAD(providerID, protocol))
}

func (m *SecretManager) EncryptToolArguments(toolCallID, plaintext string) (string, error) {
	return m.encrypt(plaintext, toolArgumentsAAD(toolCallID))
}

func (m *SecretManager) DecryptToolArguments(toolCallID, encrypted string) (string, error) {
	return m.decrypt(encrypted, toolArgumentsAAD(toolCallID))
}

func (m *SecretManager) encrypt(plaintext string, aad []byte) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	aead, err := m.aead()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate AI secret nonce: %w", err)
	}
	sealed := aead.Seal(nil, nonce, []byte(plaintext), aad)
	payload := append(nonce, sealed...)
	return "v1:" + base64.RawStdEncoding.EncodeToString(payload), nil
}

func (m *SecretManager) decrypt(encrypted string, aad []byte) (string, error) {
	if encrypted == "" {
		return "", nil
	}
	prefix, payload, ok := strings.Cut(encrypted, ":")
	if !ok || prefix != "v1" {
		return "", ErrSecretDecrypt
	}
	decoded, err := base64.RawStdEncoding.DecodeString(payload)
	if err != nil {
		return "", ErrSecretDecrypt
	}
	aead, err := m.aead()
	if err != nil {
		return "", err
	}
	if len(decoded) <= aead.NonceSize() {
		return "", ErrSecretDecrypt
	}
	plaintext, err := aead.Open(nil, decoded[:aead.NonceSize()], decoded[aead.NonceSize():], aad)
	if err != nil {
		return "", ErrSecretDecrypt
	}
	return string(plaintext), nil
}

func (m *SecretManager) aead() (cipher.AEAD, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.key) != masterKeySize {
		return nil, ErrMasterKeyUnavailable
	}
	block, err := aes.NewCipher(m.key)
	if err != nil {
		return nil, ErrMasterKeyUnavailable
	}
	return cipher.NewGCM(block)
}

func secretAAD(providerID, protocol string) []byte {
	return []byte("mizupanel-ai-provider\x00" + providerID + "\x00" + protocol)
}

func toolArgumentsAAD(toolCallID string) []byte {
	return []byte("mizupanel-ai-tool-call\x00" + toolCallID + "\x00arguments-v1")
}
