package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseIfMatchAcceptsOneQuotedPositiveVersion(t *testing.T) {
	version, err := parseIfMatch(`"42"`)
	require.NoError(t, err)
	require.Equal(t, int64(42), version)
	require.Equal(t, `"42"`, formatETag(version))
}

func TestParseIfMatchRejectsUnsupportedValidators(t *testing.T) {
	for _, value := range []string{
		"", "42", `"0"`, `"01"`, "*", `W/"1"`, `"1","2"`, `"1", "2"`, `"-1"`,
	} {
		t.Run(value, func(t *testing.T) {
			_, err := parseIfMatch(value)
			require.ErrorIs(t, err, ErrInvalidRequest)
		})
	}
}
