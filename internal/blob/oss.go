package blob

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/wolfhead/pactline/internal/domain"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
)

type OSSConfig struct {
	Region          string
	Endpoint        string
	Bucket          string
	AccessKeyID     string
	AccessKeySecret string
	UsePathStyle    bool
}

type OSSStore struct {
	client *aliyunoss.Client
	bucket string
}

func NewOSSStore(config OSSConfig) (*OSSStore, error) {
	if config.Region == "" || config.Bucket == "" || config.AccessKeyID == "" || config.AccessKeySecret == "" {
		return nil, fmt.Errorf("OSS region, bucket, access key ID, and access key secret are required")
	}
	sdkConfig := aliyunoss.NewConfig().
		WithRegion(config.Region).
		WithCredentialsProvider(credentials.NewStaticCredentialsProvider(config.AccessKeyID, config.AccessKeySecret)).
		WithUsePathStyle(config.UsePathStyle)
	if config.Endpoint != "" {
		sdkConfig.WithEndpoint(config.Endpoint)
	}
	return &OSSStore{client: aliyunoss.NewClient(sdkConfig), bucket: config.Bucket}, nil
}

func (s *OSSStore) Provider() domain.AttachmentProvider { return domain.AttachmentProviderOSS }

func (s *OSSStore) CreateUpload(ctx context.Context, key, mediaType string, size int64, expiresAt time.Time) (UploadInstruction, error) {
	result, err := s.client.Presign(ctx, &aliyunoss.PutObjectRequest{
		Bucket: aliyunoss.Ptr(s.bucket), Key: aliyunoss.Ptr(key),
		ContentLength: aliyunoss.Ptr(size), ContentType: aliyunoss.Ptr(mediaType),
		ForbidOverwrite: aliyunoss.Ptr("true"),
	}, aliyunoss.PresignExpiration(expiresAt))
	if err != nil {
		return UploadInstruction{}, fmt.Errorf("presign OSS attachment upload: %w", err)
	}
	headers := make(http.Header, len(result.SignedHeaders))
	for name, value := range result.SignedHeaders {
		headers.Set(name, value)
	}
	return UploadInstruction{Method: result.Method, URL: result.URL, Headers: headers, Direct: true, ExpiresAt: result.Expiration}, nil
}

func (s *OSSStore) Put(ctx context.Context, key string, body io.Reader, size int64, mediaType string) error {
	_, err := s.client.PutObject(ctx, &aliyunoss.PutObjectRequest{
		Bucket: aliyunoss.Ptr(s.bucket), Key: aliyunoss.Ptr(key), Body: body,
		ContentLength: aliyunoss.Ptr(size), ContentType: aliyunoss.Ptr(mediaType),
		ForbidOverwrite: aliyunoss.Ptr("true"),
	})
	if err != nil {
		return fmt.Errorf("put OSS attachment: %w", err)
	}
	return nil
}

func (s *OSSStore) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	result, err := s.client.HeadObject(ctx, &aliyunoss.HeadObjectRequest{Bucket: aliyunoss.Ptr(s.bucket), Key: aliyunoss.Ptr(key)})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("head OSS attachment: %w", err)
	}
	mediaType := ""
	if result.ContentType != nil {
		mediaType = *result.ContentType
	}
	return ObjectInfo{Size: result.ContentLength, MediaType: mediaType}, nil
}

func (s *OSSStore) Open(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	result, err := s.client.GetObject(ctx, &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(s.bucket), Key: aliyunoss.Ptr(key)})
	if err != nil {
		return nil, ObjectInfo{}, fmt.Errorf("get OSS attachment: %w", err)
	}
	mediaType := ""
	if result.ContentType != nil {
		mediaType = *result.ContentType
	}
	return result.Body, ObjectInfo{Size: result.ContentLength, MediaType: mediaType}, nil
}

func (s *OSSStore) SignedGet(ctx context.Context, key string, expiresAt time.Time, downloadName, mediaType string) (string, error) {
	disposition := fmt.Sprintf("attachment; filename=%q", downloadName)
	request := &aliyunoss.GetObjectRequest{
		Bucket: aliyunoss.Ptr(s.bucket), Key: aliyunoss.Ptr(key),
		ResponseContentDisposition: aliyunoss.Ptr(disposition),
		ResponseContentType:        aliyunoss.Ptr(mediaType),
	}
	result, err := s.client.Presign(ctx, request, aliyunoss.PresignExpiration(expiresAt))
	if err != nil {
		return "", fmt.Errorf("presign OSS attachment download: %w", err)
	}
	return result.URL, nil
}

func (s *OSSStore) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &aliyunoss.DeleteObjectRequest{Bucket: aliyunoss.Ptr(s.bucket), Key: aliyunoss.Ptr(key)})
	if err != nil {
		return fmt.Errorf("delete OSS attachment: %w", err)
	}
	return nil
}
