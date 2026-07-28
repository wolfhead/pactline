package identity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpaqueSecretAndHash(t *testing.T) {
	secret, err := NewOpaqueSecret()
	require.NoError(t, err)
	assert.Len(t, secret, 43)
	hash := HashSecret([]byte(secret))
	assert.True(t, VerifySecret([]byte(secret), hash[:]))
	assert.False(t, VerifySecret([]byte(secret+"x"), hash[:]))
}

func TestCredentialCipher(t *testing.T) {
	key := make([]byte, 32)
	cipher, err := NewCredentialCipher(map[string][]byte{"key-1": key})
	require.NoError(t, err)

	first, err := cipher.Encrypt("key-1", []byte("provider-token"))
	require.NoError(t, err)
	second, err := cipher.Encrypt("key-1", []byte("provider-token"))
	require.NoError(t, err)
	assert.NotEqual(t, first, second)

	plaintext, err := cipher.Decrypt("key-1", first)
	require.NoError(t, err)
	assert.Equal(t, []byte("provider-token"), plaintext)

	first[len(first)-1] ^= 1
	_, err = cipher.Decrypt("key-1", first)
	assert.ErrorContains(t, err, "authentication failed")
	_, err = cipher.Decrypt("unknown", second)
	assert.ErrorContains(t, err, "unknown credential encryption key id")
}

func TestCredentialCipherRejectsInvalidKey(t *testing.T) {
	_, err := NewCredentialCipher(map[string][]byte{"key": make([]byte, 31)})
	assert.ErrorContains(t, err, "must be 32 bytes")
}
