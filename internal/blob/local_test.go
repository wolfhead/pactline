package blob

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestLocalStoreRoundTrip(t *testing.T) {
	t.Parallel()
	storage, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Put(context.Background(), "tasks/one/file", strings.NewReader("hello"), 5, "text/plain"); err != nil {
		t.Fatal(err)
	}
	body, info, err := storage.Open(context.Background(), "tasks/one/file")
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	content, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "hello" || info.Size != 5 {
		t.Fatalf("content = %q, size = %d", content, info.Size)
	}
}

func TestLocalStoreRejectsTraversalAndMismatchedSize(t *testing.T) {
	t.Parallel()
	storage, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Put(context.Background(), "../escape", strings.NewReader("x"), 1, "text/plain"); err == nil {
		t.Fatal("expected traversal rejection")
	}
	if err := storage.Put(context.Background(), "short", strings.NewReader("x"), 2, "text/plain"); err == nil {
		t.Fatal("expected size mismatch rejection")
	}
	if err := storage.Put(context.Background(), "long", strings.NewReader("xx"), 1, "text/plain"); err == nil {
		t.Fatal("expected oversized body rejection")
	}
}
