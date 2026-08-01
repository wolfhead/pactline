package artifact

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAICompatibleVisionSendsImageAndReturnsDescription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		require.Equal(t, "Bearer vision-secret", r.Header.Get("Authorization"))
		encoded, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var request map[string]any
		require.NoError(t, json.Unmarshal(encoded, &request))
		require.NotContains(t, string(encoded), "vision-secret")
		require.Contains(t, string(encoded), "identify the affected account scope")
		require.Contains(t, string(encoded), "sticker, emoji, reaction, meme, avatar, or decorative image")
		require.Contains(t, string(encoded), "do not analyze it further")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"choices":[{"message":{"content":"The image contains a timeout table; one visible row shows 49.4%."}}]
		}`)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "image.png")
	require.NoError(t, os.WriteFile(path, []byte("synthetic-image"), 0o600))
	vision, err := NewOpenAICompatibleVision(VisionConfig{
		APIKey: "vision-secret", BaseURL: server.URL + "/v1", Model: "vision-model",
		HTTPClient: server.Client(),
	})
	require.NoError(t, err)

	description, err := vision.Describe(
		context.Background(), path, "image/png", "identify the affected account scope",
	)

	require.NoError(t, err)
	require.Equal(t, "The image contains a timeout table; one visible row shows 49.4%.", description)
}

func TestOpenAICompatibleVisionRejectsInvalidBaseURL(t *testing.T) {
	_, err := NewOpenAICompatibleVision(VisionConfig{
		APIKey: "test-key", BaseURL: "not-a-url", Model: "vision-model",
	})

	require.ErrorContains(t, err, "absolute HTTP(S) URL")
}
