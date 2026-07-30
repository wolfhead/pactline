package agent

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
)

var ErrRunInputDecrypt = errors.New("agent run input cannot be decrypted")

type RunInput struct {
	RunID                   uuid.UUID
	EncryptionKeyID         string
	CommandCiphertext       []byte
	PendingResumeCiphertext []byte
}

type InputCipher struct {
	keyID  string
	aead   cipher.AEAD
	random io.Reader
}

func NewInputCipher(keyID string, key []byte) (*InputCipher, error) {
	if keyID == "" || len(key) != 32 {
		return nil, ErrCheckpointConfiguration
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrCheckpointConfiguration
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrCheckpointConfiguration
	}
	return &InputCipher{keyID: keyID, aead: aead, random: rand.Reader}, nil
}

func (c *InputCipher) KeyID() string { return c.keyID }

func (c *InputCipher) Encrypt(runID uuid.UUID, purpose string, plaintext []byte) ([]byte, error) {
	if runID == uuid.Nil || purpose == "" || len(plaintext) == 0 {
		return nil, ErrCheckpointConfiguration
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(c.random, nonce); err != nil {
		return nil, fmt.Errorf("generate Agent input nonce: %w", err)
	}
	return append(
		nonce,
		c.aead.Seal(nil, nonce, plaintext, inputAAD(runID, purpose, c.keyID))...,
	), nil
}

func (c *InputCipher) Decrypt(
	runID uuid.UUID,
	purpose string,
	ciphertext []byte,
) ([]byte, error) {
	if runID == uuid.Nil || purpose == "" || len(ciphertext) <= c.aead.NonceSize() {
		return nil, ErrRunInputDecrypt
	}
	nonce := ciphertext[:c.aead.NonceSize()]
	plaintext, err := c.aead.Open(
		nil,
		nonce,
		ciphertext[c.aead.NonceSize():],
		inputAAD(runID, purpose, c.keyID),
	)
	if err != nil {
		return nil, ErrRunInputDecrypt
	}
	return plaintext, nil
}

func inputAAD(runID uuid.UUID, purpose, keyID string) []byte {
	return []byte(fmt.Sprintf("pactline-agent-input|%s|%s|%s", runID, purpose, keyID))
}
