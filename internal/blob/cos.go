package blob

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/wolfhead/pactline/internal/domain"

	cos "github.com/tencentyun/cos-go-sdk-v5"
)

type COSConfig struct {
	BucketURL    string
	ServiceURL   string
	AccessKeyID  string
	AccessSecret string
	SessionToken string
}

type COSStore struct {
	client *cos.Client
}

func NewCOSStore(config COSConfig) (*COSStore, error) {
	bucketURL, err := url.Parse(config.BucketURL)
	if err != nil || bucketURL.Scheme == "" || bucketURL.Host == "" {
		return nil, fmt.Errorf("COS bucket URL must be absolute")
	}
	var serviceURL *url.URL
	if config.ServiceURL != "" {
		serviceURL, err = url.Parse(config.ServiceURL)
		if err != nil || serviceURL.Scheme == "" || serviceURL.Host == "" {
			return nil, fmt.Errorf("COS service URL must be absolute")
		}
	}
	if config.AccessKeyID == "" || config.AccessSecret == "" {
		return nil, fmt.Errorf("COS access key ID and secret are required")
	}
	client := cos.NewClient(&cos.BaseURL{BucketURL: bucketURL, ServiceURL: serviceURL}, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID: config.AccessKeyID, SecretKey: config.AccessSecret,
			SessionToken: config.SessionToken,
		},
	})
	return &COSStore{client: client}, nil
}

func (s *COSStore) Provider() domain.AttachmentProvider { return domain.AttachmentProviderCOS }

func (s *COSStore) CreateUpload(ctx context.Context, key, mediaType string, size int64, expiresAt time.Time) (UploadInstruction, error) {
	headers := make(http.Header)
	headers.Set("Content-Type", mediaType)
	headers.Set("Content-Length", strconv.FormatInt(size, 10))
	options := &cos.PresignedURLOptions{Header: &headers}
	result, err := s.client.Object.GetPresignedURL2(ctx, http.MethodPut, key, time.Until(expiresAt), options)
	if err != nil {
		return UploadInstruction{}, fmt.Errorf("presign COS attachment upload: %w", err)
	}
	return UploadInstruction{Method: http.MethodPut, URL: result.String(), Headers: headers, Direct: true, ExpiresAt: expiresAt}, nil
}

func (s *COSStore) Put(ctx context.Context, key string, body io.Reader, size int64, mediaType string) error {
	_, err := s.client.Object.Put(ctx, key, body, &cos.ObjectPutOptions{ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
		ContentLength: size, ContentType: mediaType,
	}})
	if err != nil {
		return fmt.Errorf("put COS attachment: %w", err)
	}
	return nil
}

func (s *COSStore) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	response, err := s.client.Object.Head(ctx, key, nil)
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("head COS attachment: %w", err)
	}
	return ObjectInfo{Size: response.ContentLength, MediaType: response.Header.Get("Content-Type")}, nil
}

func (s *COSStore) Open(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	response, err := s.client.Object.Get(ctx, key, nil)
	if err != nil {
		return nil, ObjectInfo{}, fmt.Errorf("get COS attachment: %w", err)
	}
	return response.Body, ObjectInfo{Size: response.ContentLength, MediaType: response.Header.Get("Content-Type")}, nil
}

func (s *COSStore) SignedGet(ctx context.Context, key string, expiresAt time.Time, downloadName, mediaType string) (string, error) {
	query := make(url.Values)
	query.Set("response-content-disposition", fmt.Sprintf("attachment; filename=%q", downloadName))
	query.Set("response-content-type", mediaType)
	options := &cos.PresignedURLOptions{Query: &query}
	result, err := s.client.Object.GetPresignedURL2(ctx, http.MethodGet, key, time.Until(expiresAt), options)
	if err != nil {
		return "", fmt.Errorf("presign COS attachment download: %w", err)
	}
	return result.String(), nil
}

func (s *COSStore) Delete(ctx context.Context, key string) error {
	_, err := s.client.Object.Delete(ctx, key)
	if err != nil {
		return fmt.Errorf("delete COS attachment: %w", err)
	}
	return nil
}
