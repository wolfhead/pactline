package agent

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
)

const (
	CheckpointFormatVersion = 1
	EinoCheckpointVersion   = "v0.9.13"
)

var (
	ErrCheckpointConfiguration = errors.New("agent checkpoint encryption configuration is invalid")
	ErrCheckpointDecrypt       = errors.New("agent checkpoint cannot be decrypted")
)

type CheckpointRepository interface {
	SaveCheckpoint(context.Context, Checkpoint) error
	GetCheckpoint(context.Context, uuid.UUID) (Checkpoint, error)
	DeleteCheckpoint(context.Context, uuid.UUID) error
}

type EncryptedCheckpointStore struct {
	repository CheckpointRepository
	keyID      string
	model      string
	aead       cipher.AEAD
	now        func() time.Time
	random     io.Reader
}

func NewEncryptedCheckpointStore(
	repository CheckpointRepository,
	keyID string,
	key []byte,
	model string,
	now func() time.Time,
) (*EncryptedCheckpointStore, error) {
	if repository == nil || keyID == "" || model == "" || len(key) != 32 {
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
	if now == nil {
		now = time.Now
	}
	return &EncryptedCheckpointStore{
		repository: repository,
		keyID:      keyID,
		model:      model,
		aead:       aead,
		now:        now,
		random:     rand.Reader,
	}, nil
}

func (s *EncryptedCheckpointStore) Get(
	ctx context.Context,
	checkpointID string,
) ([]byte, bool, error) {
	runID, err := uuid.Parse(checkpointID)
	if err != nil {
		return nil, false, fmt.Errorf("parse agent checkpoint ID: %w", ErrAgentCheckpointNotFound)
	}
	checkpoint, err := s.repository.GetCheckpoint(ctx, runID)
	if errors.Is(err, ErrAgentCheckpointNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if checkpoint.FormatVersion != CheckpointFormatVersion ||
		checkpoint.EinoVersion != EinoCheckpointVersion ||
		checkpoint.Model != s.model ||
		checkpoint.EncryptionKeyID != s.keyID ||
		len(checkpoint.Ciphertext) <= s.aead.NonceSize() {
		return nil, false, ErrCheckpointDecrypt
	}
	nonce := checkpoint.Ciphertext[:s.aead.NonceSize()]
	sealed := checkpoint.Ciphertext[s.aead.NonceSize():]
	plaintext, err := s.aead.Open(nil, nonce, sealed, checkpointAAD(checkpoint))
	if err != nil {
		return nil, false, ErrCheckpointDecrypt
	}
	return plaintext, true, nil
}

func (s *EncryptedCheckpointStore) Set(
	ctx context.Context,
	checkpointID string,
	plaintext []byte,
) error {
	runID, err := uuid.Parse(checkpointID)
	if err != nil || runID == uuid.Nil || len(plaintext) == 0 {
		return ErrCheckpointConfiguration
	}
	checkpoint := Checkpoint{
		RunID:           runID,
		FormatVersion:   CheckpointFormatVersion,
		EinoVersion:     EinoCheckpointVersion,
		Model:           s.model,
		EncryptionKeyID: s.keyID,
		UpdatedAt:       s.now().UTC(),
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(s.random, nonce); err != nil {
		return fmt.Errorf("generate agent checkpoint nonce: %w", err)
	}
	checkpoint.Ciphertext = append(
		nonce,
		s.aead.Seal(nil, nonce, plaintext, checkpointAAD(checkpoint))...,
	)
	return s.repository.SaveCheckpoint(ctx, checkpoint)
}

func (s *EncryptedCheckpointStore) Delete(ctx context.Context, checkpointID string) error {
	runID, err := uuid.Parse(checkpointID)
	if err != nil || runID == uuid.Nil {
		return ErrCheckpointConfiguration
	}
	return s.repository.DeleteCheckpoint(ctx, runID)
}

func checkpointAAD(checkpoint Checkpoint) []byte {
	return []byte(fmt.Sprintf(
		"pactline-agent-checkpoint|%s|%d|%s|%s|%s",
		checkpoint.RunID,
		checkpoint.FormatVersion,
		checkpoint.EinoVersion,
		checkpoint.Model,
		checkpoint.EncryptionKeyID,
	))
}
