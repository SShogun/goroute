package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type Store struct {
	path string
	key  []byte
	mu   sync.RWMutex
	data map[string]string // encrypted base64 ciphertext
}

func deriveKey(passphrase string) []byte {
	hash := sha256.Sum256([]byte(passphrase))
	return hash[:]
}

func NewStore(path string, secretKey string) (*Store, error) {
	key := deriveKey(secretKey)
	st := &Store{
		path: path,
		key:  key,
		data: make(map[string]string),
	}

	if err := st.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return st, nil
}

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}

	return json.Unmarshal(raw, &s.data)
}

func (s *Store) save() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	bytes, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, bytes, 0600)
}

func (s *Store) SetSecret(key, plaintext string) error {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	encoded := base64.StdEncoding.EncodeToString(ciphertext)

	s.mu.Lock()
	s.data[key] = encoded
	s.mu.Unlock()

	return s.save()
}

func (s *Store) GetSecret(key string) (string, error) {
	s.mu.RLock()
	encoded, ok := s.data[key]
	s.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("secret not found for key: %s", key)
	}

	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("malformed ciphertext")
	}

	nonce, actualCiphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, actualCiphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt failed: %w", err)
	}

	return string(plaintext), nil
}
