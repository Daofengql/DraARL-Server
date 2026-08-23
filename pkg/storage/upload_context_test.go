package storage

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http/httptest"
	"net/textproto"
	"testing"

	"draarl/internal/config"
)

func TestUploadAvatarContextHonorsCancellation(t *testing.T) {
	store, err := newLocalStorageWithConfig(config.LocalStorageConfig{RootPath: t.TempDir()}, "test-secret")
	if err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	previous := current
	current = store
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		current = previous
		mu.Unlock()
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := UploadAvatarContext(ctx, 1, []byte("processed-avatar"), ".jpg"); !errors.Is(err, context.Canceled) {
		t.Fatalf("UploadAvatarContext() error=%v, want context.Canceled", err)
	}
}

func TestProcessAvatarContextHonorsCancellation(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "avatar.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("not read after cancellation")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest("POST", "/", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if err := request.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	fileHeader := request.MultipartForm.File["file"][0]

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := ProcessAvatarContext(ctx, fileHeader); !errors.Is(err, context.Canceled) {
		t.Fatalf("ProcessAvatarContext() error=%v, want context.Canceled", err)
	}
}

func TestUploadMultipartReturnsSniffedContentType(t *testing.T) {
	store, err := newLocalStorageWithConfig(config.LocalStorageConfig{RootPath: t.TempDir()}, "test-secret")
	if err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	previous := current
	current = store
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		current = previous
		mu.Unlock()
	})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="note.txt"`)
	header.Set("Content-Type", "image/png")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("plain text content")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest("POST", "/", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if err := request.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	fileHeader := request.MultipartForm.File["file"][0]

	_, _, contentType, err := UploadMultipartFileWithContentTypeContext(context.Background(), fileHeader, 1, "assets")
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "text/plain; charset=utf-8" {
		t.Fatalf("contentType=%q, want sniffed text/plain", contentType)
	}
}
