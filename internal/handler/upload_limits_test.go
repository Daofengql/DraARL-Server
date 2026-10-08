package handler

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type countedUploadReader struct {
	io.Reader
	count int
}

func (r *countedUploadReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.count += n
	return n, err
}

func TestUploadFormBoundsKnownAndChunkedRequests(t *testing.T) {
	const maxFileBytes = 1024
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	field, _ := form.CreateFormField("name")
	_, _ = io.WriteString(field, strings.Repeat("x", maxFileBytes+uploadFormOverhead+4096))
	_ = form.Close()
	for _, chunked := range []bool{false, true} {
		t.Run(map[bool]string{false: "known length", true: "chunked"}[chunked], func(t *testing.T) {
			reader := &countedUploadReader{Reader: bytes.NewReader(body.Bytes())}
			req := httptest.NewRequest(http.MethodPost, "/upload", reader)
			req.Header.Set("Content-Type", form.FormDataContentType())
			if chunked {
				req.ContentLength = -1
			} else {
				req.ContentLength = int64(body.Len())
			}
			writer := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(writer)
			ctx.Request = req
			if parseLimitedUploadForm(ctx, maxFileBytes) || writer.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("oversized upload accepted: %d", writer.Code)
			}
			if reader.count > maxFileBytes+uploadFormOverhead+1 {
				t.Fatalf("read beyond bound: %d", reader.count)
			}
		})
	}
}

func TestUploadFormSupportsMultipartAndDirectCertificateForms(t *testing.T) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("callsign", "TEST")
	file, _ := form.CreateFormFile("file", "test.txt")
	_, _ = file.Write([]byte("data"))
	_ = form.Close()
	for _, tc := range []struct{ contentType, body string }{
		{form.FormDataContentType(), body.String()},
		{"application/x-www-form-urlencoded", "callsign=TEST&object_key=test&upload_token=sample"},
	} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(tc.body))
		ctx.Request.Header.Set("Content-Type", tc.contentType)
		if !parseLimitedUploadForm(ctx, 1024) || ctx.PostForm("callsign") != "TEST" {
			t.Fatal("valid form rejected")
		}
		if ctx.Request.MultipartForm != nil {
			_ = ctx.Request.MultipartForm.RemoveAll()
		}
	}
}
