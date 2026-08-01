package blob

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
)

type UploadInstruction struct {
	Method    string
	URL       string
	Headers   http.Header
	Direct    bool
	ExpiresAt time.Time
}

type ObjectInfo struct {
	Size      int64
	MediaType string
}

type Store interface {
	Provider() domain.AttachmentProvider
	CreateUpload(context.Context, string, string, int64, time.Time) (UploadInstruction, error)
	Put(context.Context, string, io.Reader, int64, string) error
	Stat(context.Context, string) (ObjectInfo, error)
	Open(context.Context, string) (io.ReadCloser, ObjectInfo, error)
	SignedGet(context.Context, string, time.Time, string, string) (string, error)
	Delete(context.Context, string) error
}
