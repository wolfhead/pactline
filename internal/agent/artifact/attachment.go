package artifact

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// AttachmentSource is a bounded, reopenable stream prepared from a resolved
// conversation artifact. It keeps filesystem access outside model-visible
// tools while preserving the original resolver cleanup lifecycle.
type AttachmentSource struct {
	Reference Reference
	Filename  string
	MediaType string
	SizeBytes int64
	Open      func() (io.ReadCloser, error)
}

func PrepareAttachment(local LocalFile) (AttachmentSource, error) {
	info, err := os.Lstat(local.Path)
	if err != nil {
		return AttachmentSource{}, fmt.Errorf("inspect resolved artifact: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > MaxFileSize {
		return AttachmentSource{}, fmt.Errorf("resolved artifact is not a bounded regular file")
	}
	file, err := os.Open(local.Path)
	if err != nil {
		return AttachmentSource{}, fmt.Errorf("open resolved artifact metadata: %w", err)
	}
	header := make([]byte, 512)
	read, readErr := file.Read(header)
	closeErr := file.Close()
	if readErr != nil && readErr != io.EOF {
		return AttachmentSource{}, fmt.Errorf("read resolved artifact metadata: %w", readErr)
	}
	if closeErr != nil {
		return AttachmentSource{}, fmt.Errorf("close resolved artifact metadata: %w", closeErr)
	}
	mediaType := normalizedAttachmentMediaType(local.Reference, header[:read])
	filename := safeAttachmentFilename(local.Reference.Name, mediaType)
	if filename == "" || !utf8.ValidString(filename) {
		return AttachmentSource{}, fmt.Errorf("resolved artifact filename is invalid")
	}
	path := local.Path
	return AttachmentSource{
		Reference: local.Reference,
		Filename:  filename,
		MediaType: mediaType,
		SizeBytes: info.Size(),
		Open: func() (io.ReadCloser, error) {
			return os.Open(path)
		},
	}, nil
}

func normalizedAttachmentMediaType(reference Reference, header []byte) string {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(reference.MediaType))
	if err != nil || mediaType == "" || mediaType == "application/octet-stream" || strings.Contains(mediaType, "*") {
		mediaType = http.DetectContentType(header)
	}
	return strings.ToLower(strings.TrimSpace(mediaType))
}

func safeAttachmentFilename(name, mediaType string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "_"))
	name = filepath.Base(name)
	if name == "" || name == "." || name == ".." {
		name = "attachment"
	}
	if filepath.Ext(name) == "" {
		switch mediaType {
		case "image/png":
			name += ".png"
		case "image/jpeg":
			name += ".jpg"
		case "image/gif":
			name += ".gif"
		case "image/webp":
			name += ".webp"
		case "text/html":
			name += ".html"
		case "text/markdown":
			name += ".md"
		}
	}
	runes := []rune(name)
	if len(runes) <= 200 {
		return name
	}
	extension := filepath.Ext(name)
	baseRunes := []rune(strings.TrimSuffix(name, extension))
	limit := 200 - len([]rune(extension))
	if limit < 1 {
		return string(runes[:200])
	}
	if len(baseRunes) > limit {
		baseRunes = baseRunes[:limit]
	}
	return string(baseRunes) + extension
}
