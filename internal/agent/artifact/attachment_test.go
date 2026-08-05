package artifact

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrepareAttachmentSniffsImageAndCreatesUsableFilename(t *testing.T) {
	path := t.TempDir() + "/source"
	require.NoError(t, os.WriteFile(path, []byte("\x89PNG\r\n\x1a\nchart"), 0o600))

	source, err := PrepareAttachment(LocalFile{
		Reference: Reference{Name: "image-1", MediaType: "image/*"},
		Path:      path,
	})

	require.NoError(t, err)
	require.Equal(t, "image/png", source.MediaType)
	require.Equal(t, "image-1.png", source.Filename)
	require.Equal(t, int64(13), source.SizeBytes)
	body, err := source.Open()
	require.NoError(t, err)
	require.NoError(t, body.Close())
}

func TestPrepareAttachmentRejectsEmptyFile(t *testing.T) {
	path := t.TempDir() + "/empty"
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	_, err := PrepareAttachment(LocalFile{Reference: Reference{Name: "empty.txt"}, Path: path})

	require.Error(t, err)
}
