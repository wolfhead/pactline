package domain

import (
	"bytes"
	"fmt"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	MaxAttachmentBytes       int64 = 100 * 1024 * 1024
	MaxActiveTaskAttachments       = 100
)

type AttachmentProvider string

const (
	AttachmentProviderLocal AttachmentProvider = "local"
	AttachmentProviderOSS   AttachmentProvider = "oss"
	AttachmentProviderCOS   AttachmentProvider = "cos"
)

type AttachmentPreviewKind string

const (
	AttachmentPreviewImage    AttachmentPreviewKind = "image"
	AttachmentPreviewMarkdown AttachmentPreviewKind = "markdown"
	AttachmentPreviewHTML     AttachmentPreviewKind = "html"
	AttachmentPreviewDownload AttachmentPreviewKind = "download"
)

type TaskAttachment struct {
	ID         uuid.UUID
	TaskID     uuid.UUID
	UploaderID uuid.UUID
	Filename   string
	MediaType  string
	SizeBytes  int64
	Provider   AttachmentProvider
	ObjectKey  string
	Version    int64
	DeletedAt  *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type AttachmentUploadSession struct {
	ID           uuid.UUID
	TaskID       uuid.UUID
	UploaderID   uuid.UUID
	Provider     AttachmentProvider
	ObjectKey    string
	Filename     string
	MediaType    string
	ExpectedSize int64
	ExpiresAt    time.Time
	CreatedAt    time.Time
}

var deniedAttachmentExtensions = map[string]struct{}{
	".apk": {}, ".app": {}, ".bat": {}, ".bin": {}, ".cmd": {},
	".com": {}, ".cpl": {}, ".dll": {}, ".dmg": {}, ".exe": {},
	".hta": {}, ".img": {}, ".iso": {}, ".jar": {}, ".js": {},
	".jse": {}, ".lnk": {}, ".msi": {}, ".msp": {}, ".ps1": {},
	".reg": {}, ".scr": {}, ".sh": {}, ".vbe": {}, ".vbs": {},
	".wsf": {},
}

var deniedAttachmentMediaTypes = map[string]struct{}{
	"application/java-archive":                {},
	"application/vnd.android.package-archive": {},
	"application/x-dosexec":                   {},
	"application/x-executable":                {},
	"application/x-msdownload":                {},
	"application/x-sh":                        {},
}

func ValidateAttachmentMetadata(filename, mediaType string, size int64) error {
	filename = strings.TrimSpace(filename)
	if filename == "" || filename == "." || filename == ".." {
		return fmt.Errorf("%w: attachment filename is required", ErrInvalidInput)
	}
	if filepath.Base(filename) != filename || strings.ContainsAny(filename, "/\\\x00") {
		return fmt.Errorf("%w: attachment filename must not contain a path", ErrInvalidInput)
	}
	if size <= 0 || size > MaxAttachmentBytes {
		return fmt.Errorf("%w: attachment size must be between 1 byte and 100 MiB", ErrInvalidInput)
	}
	mediaType = strings.ToLower(strings.TrimSpace(strings.Split(mediaType, ";")[0]))
	if mediaType == "" {
		return fmt.Errorf("%w: attachment media type is required", ErrInvalidInput)
	}
	if _, denied := deniedAttachmentExtensions[strings.ToLower(filepath.Ext(filename))]; denied {
		return fmt.Errorf("%w: this attachment file type is not allowed", ErrInvalidInput)
	}
	if _, denied := deniedAttachmentMediaTypes[mediaType]; denied {
		return fmt.Errorf("%w: this attachment media type is not allowed", ErrInvalidInput)
	}
	return nil
}

func NormalizeAttachmentMediaType(filename, mediaType string) string {
	mediaType = strings.ToLower(strings.TrimSpace(strings.Split(mediaType, ";")[0]))
	if mediaType != "" && mediaType != "application/octet-stream" {
		return mediaType
	}
	if inferred := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename))); inferred != "" {
		return strings.ToLower(strings.TrimSpace(strings.Split(inferred, ";")[0]))
	}
	return "application/octet-stream"
}

func AttachmentPreview(filename, mediaType string) AttachmentPreviewKind {
	mediaType = NormalizeAttachmentMediaType(filename, mediaType)
	ext := strings.ToLower(filepath.Ext(filename))
	if strings.HasPrefix(mediaType, "image/") && mediaType != "image/svg+xml" {
		return AttachmentPreviewImage
	}
	if mediaType == "text/markdown" || ext == ".md" || ext == ".markdown" {
		return AttachmentPreviewMarkdown
	}
	if mediaType == "text/html" || ext == ".html" || ext == ".htm" {
		return AttachmentPreviewHTML
	}
	return AttachmentPreviewDownload
}

func ValidateAttachmentContentSignature(header []byte) error {
	dangerous := bytes.HasPrefix(header, []byte{'M', 'Z'}) ||
		bytes.HasPrefix(header, []byte{0x7f, 'E', 'L', 'F'}) ||
		bytes.HasPrefix(header, []byte("#!")) ||
		bytes.HasPrefix(header, []byte{0xfe, 0xed, 0xfa, 0xce}) ||
		bytes.HasPrefix(header, []byte{0xce, 0xfa, 0xed, 0xfe}) ||
		bytes.HasPrefix(header, []byte{0xfe, 0xed, 0xfa, 0xcf}) ||
		bytes.HasPrefix(header, []byte{0xcf, 0xfa, 0xed, 0xfe})
	if dangerous {
		return fmt.Errorf("%w: executable attachment content is not allowed", ErrInvalidInput)
	}
	return nil
}
