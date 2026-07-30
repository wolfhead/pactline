package agent

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestEncryptedCheckpointStoreRoundTripAndDelete(t *testing.T) {
	runID := uuid.New()
	repository := &memoryCheckpointRepository{values: map[uuid.UUID]Checkpoint{}}
	store, err := NewEncryptedCheckpointStore(
		repository,
		"checkpoint-key-1",
		[]byte("0123456789abcdef0123456789abcdef"),
		"deepseek-v4-pro",
		func() time.Time { return time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC) },
	)
	require.NoError(t, err)

	plaintext := []byte("sensitive checkpoint")
	require.NoError(t, store.Set(context.Background(), runID.String(), plaintext))
	persisted := repository.values[runID]
	require.NotContains(t, string(persisted.Ciphertext), string(plaintext))
	require.Equal(t, EinoCheckpointVersion, persisted.EinoVersion)

	got, found, err := store.Get(context.Background(), runID.String())
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, plaintext, got)

	require.NoError(t, store.Delete(context.Background(), runID.String()))
	_, found, err = store.Get(context.Background(), runID.String())
	require.NoError(t, err)
	require.False(t, found)
}

func TestEncryptedCheckpointStoreRejectsTamperingAndWrongMetadata(t *testing.T) {
	runID := uuid.New()
	repository := &memoryCheckpointRepository{values: map[uuid.UUID]Checkpoint{}}
	store, err := NewEncryptedCheckpointStore(
		repository,
		"checkpoint-key-1",
		[]byte("0123456789abcdef0123456789abcdef"),
		"deepseek-v4-pro",
		time.Now,
	)
	require.NoError(t, err)
	require.NoError(t, store.Set(context.Background(), runID.String(), []byte("checkpoint")))

	checkpoint := repository.values[runID]
	checkpoint.Ciphertext[len(checkpoint.Ciphertext)-1] ^= 0xff
	repository.values[runID] = checkpoint
	_, _, err = store.Get(context.Background(), runID.String())
	require.ErrorIs(t, err, ErrCheckpointDecrypt)

	checkpoint = repository.values[runID]
	checkpoint.Model = "other-model"
	repository.values[runID] = checkpoint
	_, _, err = store.Get(context.Background(), runID.String())
	require.ErrorIs(t, err, ErrCheckpointDecrypt)
}

type memoryCheckpointRepository struct {
	values map[uuid.UUID]Checkpoint
}

func (r *memoryCheckpointRepository) SaveCheckpoint(_ context.Context, value Checkpoint) error {
	value.Ciphertext = append([]byte(nil), value.Ciphertext...)
	r.values[value.RunID] = value
	return nil
}

func (r *memoryCheckpointRepository) GetCheckpoint(_ context.Context, id uuid.UUID) (Checkpoint, error) {
	value, ok := r.values[id]
	if !ok {
		return Checkpoint{}, ErrAgentCheckpointNotFound
	}
	value.Ciphertext = append([]byte(nil), value.Ciphertext...)
	return value, nil
}

func (r *memoryCheckpointRepository) DeleteCheckpoint(_ context.Context, id uuid.UUID) error {
	delete(r.values, id)
	return nil
}
