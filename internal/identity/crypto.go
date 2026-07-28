package identity

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

func NewOpaqueSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", fmt.Errorf("generate opaque secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func HashSecret(secret []byte) [sha256.Size]byte {
	return sha256.Sum256(secret)
}

func VerifySecret(secret, expectedHash []byte) bool {
	actual := HashSecret(secret)
	return len(expectedHash) == sha256.Size && hmac.Equal(actual[:], expectedHash)
}

type CredentialCipher struct {
	keys map[string][]byte
}

func NewCredentialCipher(keys map[string][]byte) (*CredentialCipher, error) {
	if len(keys) == 0 {
		return nil, errors.New("credential cipher requires at least one key")
	}
	owned := make(map[string][]byte, len(keys))
	for id, key := range keys {
		if id == "" {
			return nil, errors.New("credential cipher key id is required")
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("credential cipher key %q must be 32 bytes", id)
		}
		owned[id] = append([]byte(nil), key...)
	}
	return &CredentialCipher{keys: owned}, nil
}

func (c *CredentialCipher) Encrypt(keyID string, plaintext []byte) ([]byte, error) {
	gcm, err := c.gcm(keyID)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate credential nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, []byte(keyID)), nil
}

func (c *CredentialCipher) Decrypt(keyID string, ciphertext []byte) ([]byte, error) {
	gcm, err := c.gcm(keyID)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("credential ciphertext is truncated")
	}
	nonce := ciphertext[:gcm.NonceSize()]
	plaintext, err := gcm.Open(nil, nonce, ciphertext[gcm.NonceSize():], []byte(keyID))
	if err != nil {
		return nil, errors.New("credential ciphertext authentication failed")
	}
	return plaintext, nil
}

func (c *CredentialCipher) gcm(keyID string) (cipher.AEAD, error) {
	key, ok := c.keys[keyID]
	if !ok {
		return nil, fmt.Errorf("unknown credential encryption key id %q", keyID)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create credential cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create credential gcm: %w", err)
	}
	return gcm, nil
}
