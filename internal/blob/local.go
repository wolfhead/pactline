package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
)

type LocalStore struct {
	root string
}

func NewLocalStore(root string) (*LocalStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("local attachment root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve local attachment root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("create local attachment root: %w", err)
	}
	return &LocalStore{root: abs}, nil
}

func (s *LocalStore) Provider() domain.AttachmentProvider { return domain.AttachmentProviderLocal }

func (s *LocalStore) CreateUpload(_ context.Context, _ string, _ string, _ int64, expiresAt time.Time) (UploadInstruction, error) {
	return UploadInstruction{Method: http.MethodPut, Direct: false, ExpiresAt: expiresAt}, nil
}

func (s *LocalStore) Put(ctx context.Context, key string, body io.Reader, size int64, _ string) error {
	if size <= 0 || size > domain.MaxAttachmentBytes {
		return fmt.Errorf("invalid local attachment size %d", size)
	}
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create attachment directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return fmt.Errorf("create temporary attachment: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName) //nolint:errcheck
	limited := &io.LimitedReader{R: body, N: size + 1}
	written, copyErr := io.Copy(temporary, &contextReader{ctx: ctx, reader: limited})
	closeErr := temporary.Close()
	if copyErr != nil {
		return fmt.Errorf("store local attachment: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close local attachment: %w", closeErr)
	}
	if written != size || limited.N == 0 {
		return fmt.Errorf("attachment body size %d does not match declared size %d", written, size)
	}
	if err := os.Chmod(temporaryName, 0o600); err != nil {
		return fmt.Errorf("secure local attachment permissions: %w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("commit local attachment: %w", err)
	}
	return nil
}

func (s *LocalStore) Stat(_ context.Context, key string) (ObjectInfo, error) {
	path, err := s.path(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return ObjectInfo{}, err
	}
	return ObjectInfo{Size: info.Size(), MediaType: mime.TypeByExtension(filepath.Ext(path))}, nil
}

func (s *LocalStore) Open(_ context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	path, err := s.path(key)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close() //nolint:errcheck
		return nil, ObjectInfo{}, err
	}
	return file, ObjectInfo{Size: info.Size()}, nil
}

func (s *LocalStore) SignedGet(_ context.Context, _ string, _ time.Time, _ string, _ string) (string, error) {
	return "", errors.New("local attachment storage does not provide signed URLs")
}

func (s *LocalStore) Delete(_ context.Context, key string) error {
	path, err := s.path(key)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *LocalStore) path(key string) (string, error) {
	key = filepath.Clean(strings.TrimSpace(key))
	if key == "." || filepath.IsAbs(key) || strings.HasPrefix(key, ".."+string(filepath.Separator)) || key == ".." {
		return "", errors.New("invalid attachment object key")
	}
	path := filepath.Join(s.root, key)
	relative, err := filepath.Rel(s.root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("attachment object key escapes storage root")
	}
	return path, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(buffer)
	}
}
