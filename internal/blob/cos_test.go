package blob

import (
	"context"
	"net/url"
	"testing"
	"time"
)

func TestCOSPresignedURLsIncludeTemporaryCredentialToken(t *testing.T) {
	t.Parallel()
	storage, err := NewCOSStore(COSConfig{
		BucketURL:    "https://example-1250000000.cos.ap-shanghai.myqcloud.com",
		AccessKeyID:  "temporary-id",
		AccessSecret: "temporary-secret",
		SessionToken: "temporary-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	upload, err := storage.CreateUpload(
		context.Background(), "tasks/task-id/upload-id", "text/markdown", 12, time.Now().Add(15*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertCOSSessionToken(t, upload.URL)
	if upload.Headers.Get("Content-Type") != "text/markdown" || upload.Headers.Get("Content-Length") != "12" {
		t.Fatalf("unexpected signed upload headers: %v", upload.Headers)
	}

	download, err := storage.SignedGet(
		context.Background(), "tasks/task-id/upload-id", time.Now().Add(15*time.Minute), "decision.md", "text/markdown",
	)
	if err != nil {
		t.Fatal(err)
	}
	assertCOSSessionToken(t, download)
	parsed, err := url.Parse(download)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("response-content-type") != "text/markdown" {
		t.Fatalf("download response content type is not signed: %s", download)
	}
	if parsed.Query().Get("response-content-disposition") == "" {
		t.Fatalf("download response disposition is not signed: %s", download)
	}
}

func TestCOSStoreRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()
	for _, config := range []COSConfig{
		{},
		{BucketURL: "relative", AccessKeyID: "id", AccessSecret: "secret"},
		{BucketURL: "https://bucket.example", ServiceURL: "relative", AccessKeyID: "id", AccessSecret: "secret"},
		{BucketURL: "https://bucket.example", AccessKeyID: "id"},
	} {
		if _, err := NewCOSStore(config); err == nil {
			t.Fatalf("expected invalid COS configuration to fail: %+v", config)
		}
	}
}

func assertCOSSessionToken(t *testing.T, rawURL string) {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("x-cos-security-token") != "temporary-token" {
		t.Fatalf("temporary credential token is missing from presigned URL: %s", rawURL)
	}
}
