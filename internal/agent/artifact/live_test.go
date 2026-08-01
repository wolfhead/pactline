package artifact

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

func TestJieKouVisionLive(t *testing.T) {
	apiKey := strings.TrimSpace(os.Getenv("JIEKOU_API_KEY"))
	if apiKey == "" {
		t.Skip("JIEKOU_API_KEY is not configured")
	}
	path := filepath.Join(t.TempDir(), "timeout-report.png")
	require.NoError(t, writeVisionSmokeImage(path))
	vision, err := NewOpenAICompatibleVision(VisionConfig{
		APIKey: apiKey, BaseURL: DefaultVisionBaseURL, Model: DefaultVisionModel,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	description, err := vision.Describe(
		ctx,
		path,
		"image/png",
		"Identify the visible timeout value, sample size, and release decision.",
	)

	require.NoError(t, err)
	require.NotEmpty(t, description)
	require.Contains(t, description, "49.4")
	t.Logf("vision description: %s", description)
}

func writeVisionSmokeImage(path string) error {
	canvas := image.NewRGBA(image.Rect(0, 0, 720, 240))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	drawer := font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(color.RGBA{R: 20, G: 30, B: 40, A: 255}),
		Face: basicfont.Face7x13,
	}
	for index, line := range []string{
		"TIMEOUT REPORT",
		"account A timeout_rate = 49.4%",
		"sample rows = 5",
		"release decision = BLOCKED",
	} {
		drawer.Dot = fixed.P(30, 45+index*42)
		drawer.DrawString(line)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	encodeErr := png.Encode(file, canvas)
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	return closeErr
}
