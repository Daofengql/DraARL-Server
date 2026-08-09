package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"draarl/internal/config"

	"github.com/google/uuid"
)

// TestS3CompatibleContract is an opt-in contract shared by MinIO, R2, COS,
// and OSS. It deliberately uses only the S3-compatible surface used by the
// application. Run it against a dedicated test bucket by setting the
// DRAARL_S3_TEST_* variables documented in docs/usage/01-部署与配置.md.
func TestS3CompatibleContract(t *testing.T) {
	endpoint := strings.TrimSpace(os.Getenv("DRAARL_S3_TEST_ENDPOINT"))
	accessKey := strings.TrimSpace(os.Getenv("DRAARL_S3_TEST_ACCESS_KEY"))
	secretKey := strings.TrimSpace(os.Getenv("DRAARL_S3_TEST_SECRET_KEY"))
	bucket := strings.TrimSpace(os.Getenv("DRAARL_S3_TEST_BUCKET"))
	if endpoint == "" || accessKey == "" || secretKey == "" || bucket == "" {
		t.Skip("set DRAARL_S3_TEST_ENDPOINT, ACCESS_KEY, SECRET_KEY, and BUCKET to run the S3 contract")
	}

	provider := envOrDefault("DRAARL_S3_TEST_PROVIDER", DriverS3)
	sessionToken := ""
	if strings.TrimSpace(os.Getenv("DRAARL_S3_TEST_SESSION_TOKEN")) != "" {
		sessionToken = "${DRAARL_S3_TEST_SESSION_TOKEN}"
	}
	store, err := newS3StorageWithConfig(config.S3Config{
		Provider:        provider,
		Endpoint:        endpoint,
		PresignEndpoint: strings.TrimSpace(os.Getenv("DRAARL_S3_TEST_PRESIGN_ENDPOINT")),
		Region:          strings.TrimSpace(os.Getenv("DRAARL_S3_TEST_REGION")),
		BucketLookup:    strings.TrimSpace(os.Getenv("DRAARL_S3_TEST_BUCKET_LOOKUP")),
		// Exercise the same runtime secret-reference path used by deployment
		// profiles instead of bypassing it with already-expanded credentials.
		AccessKey:        "${DRAARL_S3_TEST_ACCESS_KEY}",
		SecretKey:        "${DRAARL_S3_TEST_SECRET_KEY}",
		SessionToken:     sessionToken,
		UseSSL:           envBool("DRAARL_S3_TEST_USE_SSL", false),
		PresignUseSSL:    envBool("DRAARL_S3_TEST_PRESIGN_USE_SSL", false),
		Bucket:           bucket,
		AutoCreateBucket: envBool("DRAARL_S3_TEST_AUTO_CREATE_BUCKET", false),
	}, provider)
	if err != nil {
		t.Fatalf("initialize %s contract storage: %v", provider, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	prefix := "contract-tests/" + uuid.NewString()
	putKey := prefix + "/put.bin"
	deleteKey := prefix + "/delete.bin"
	stagedKey := "staging/contract-tests/" + uuid.NewString() + "/upload.bin"
	finalKey := prefix + "/promoted.bin"
	cleanupKey := "staging/contract-tests/" + uuid.NewString() + "/cleanup.bin"
	for _, key := range []string{putKey, deleteKey, stagedKey, finalKey, cleanupKey} {
		key := key
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cleanupCancel()
			_ = store.Delete(cleanupCtx, key)
		})
	}

	payload := []byte("draarl-s3-contract-" + uuid.NewString())
	if err := store.Put(ctx, putKey, bytes.NewReader(payload), int64(len(payload)), "application/octet-stream"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	assertStoredObject(t, ctx, store, putKey, payload)
	s3Store, ok := store.(*minioStorage)
	if !ok {
		t.Fatalf("contract storage type=%T, want S3-compatible storage", store)
	}
	assertAnonymousGetDenied(t, ctx, s3Store, putKey)
	assertDownloadURLPrefixRoundTrip(t, ctx, s3Store, putKey, payload)

	found := false
	if err := store.Walk(ctx, prefix+"/", func(object ObjectInfo) error {
		if object.Key == putKey && object.Size == int64(len(payload)) {
			found = true
		}
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !found {
		t.Fatalf("Walk did not return %s", putKey)
	}
	if err := store.Put(ctx, deleteKey, bytes.NewReader(payload), int64(len(payload)), "application/octet-stream"); err != nil {
		t.Fatalf("Put delete target: %v", err)
	}
	if err := store.Delete(ctx, deleteKey); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := store.Stat(ctx, deleteKey); err == nil {
		t.Fatal("Delete left the object readable")
	}

	presignedPut, err := store.PresignPut(ctx, stagedKey, 5*time.Minute, "application/octet-stream", int64(len(payload)))
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}
	corsOrigin := strings.TrimSpace(os.Getenv("DRAARL_S3_TEST_CORS_ORIGIN"))
	if corsOrigin != "" {
		assertPresignedPutCORS(t, ctx, presignedPut.UploadURL, corsOrigin)
	}
	putRequest, err := http.NewRequestWithContext(ctx, http.MethodPut, presignedPut.UploadURL, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	putRequest.Header.Set("Content-Type", "application/octet-stream")
	putResponse, err := (&http.Client{Timeout: 30 * time.Second}).Do(putRequest)
	if err != nil {
		t.Fatalf("presigned PUT request: %v", err)
	}
	defer putResponse.Body.Close()
	if putResponse.StatusCode < 200 || putResponse.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(putResponse.Body, 4096))
		t.Fatalf("presigned PUT status=%d body=%s", putResponse.StatusCode, strings.TrimSpace(string(body)))
	}
	assertStoredObject(t, ctx, store, stagedKey, payload)

	presignedGet, err := store.PresignGet(ctx, stagedKey, 5*time.Minute)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	getRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, presignedGet, nil)
	if err != nil {
		t.Fatal(err)
	}
	if corsOrigin != "" {
		getRequest.Header.Set("Origin", corsOrigin)
	}
	getResponse, err := (&http.Client{Timeout: 30 * time.Second}).Do(getRequest)
	if err != nil {
		t.Fatalf("presigned GET request: %v", err)
	}
	defer getResponse.Body.Close()
	got, err := io.ReadAll(getResponse.Body)
	if err != nil {
		t.Fatal(err)
	}
	if getResponse.StatusCode != http.StatusOK || !bytes.Equal(got, payload) {
		t.Fatalf("presigned GET status=%d payload_match=%t", getResponse.StatusCode, bytes.Equal(got, payload))
	}
	if corsOrigin != "" {
		assertCORSAllowedOrigin(t, getResponse.Header, corsOrigin)
	}

	if err := store.Promote(ctx, stagedKey, finalKey); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if _, _, err := store.Stat(ctx, stagedKey); err == nil {
		t.Fatal("Promote must remove the staging object")
	}
	assertStoredObject(t, ctx, store, finalKey, payload)

	// Final keys are immutable. A second staging object must neither replace the
	// published payload nor make Promote appear successful.
	replacement := []byte("replacement-" + uuid.NewString())
	if err := store.Put(ctx, stagedKey, bytes.NewReader(replacement), int64(len(replacement)), "application/octet-stream"); err != nil {
		t.Fatalf("put replacement staging object: %v", err)
	}
	if err := store.Promote(ctx, stagedKey, finalKey); !errors.Is(err, ErrFinalObjectAlreadyExists) {
		t.Fatalf("second Promote error=%v, want immutable-final conflict", err)
	}
	assertStoredObject(t, ctx, store, finalKey, payload)

	if err := store.Put(ctx, cleanupKey, bytes.NewReader(payload), int64(len(payload)), "application/octet-stream"); err != nil {
		t.Fatalf("put cleanup object: %v", err)
	}
	if envBool("DRAARL_S3_TEST_DESTRUCTIVE_CLEANUP", false) {
		// This scans the bucket-wide staging/ prefix, so enable it only for a
		// dedicated test bucket with no concurrent uploads.
		if err := store.CleanupStaging(ctx, time.Now().Add(time.Minute)); err != nil {
			t.Fatalf("CleanupStaging: %v", err)
		}
		if _, _, err := store.Stat(ctx, cleanupKey); err == nil {
			t.Fatal("CleanupStaging did not delete the expired contract object")
		}
	} else {
		// Exercise the listing path without deleting any normally dated object
		// from a shared bucket.
		if err := store.CleanupStaging(ctx, time.Unix(0, 0)); err != nil {
			t.Fatalf("CleanupStaging active-object check: %v", err)
		}
		if _, _, err := store.Stat(ctx, cleanupKey); err != nil {
			t.Fatalf("CleanupStaging deleted an active object: %v", err)
		}
	}
}

func assertAnonymousGetDenied(t *testing.T, ctx context.Context, store *minioStorage, key string) {
	t.Helper()
	signedURL, err := store.PresignGet(ctx, key, 5*time.Minute)
	if err != nil {
		t.Fatalf("generate anonymous-read probe URL: %v", err)
	}
	target, err := urlWithoutSignature(signedURL)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		t.Fatalf("anonymous GET request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		t.Fatalf("private S3 object accepted anonymous GET with status %d", response.StatusCode)
	}
}

func assertDownloadURLPrefixRoundTrip(t *testing.T, ctx context.Context, store *minioStorage, key string, want []byte) {
	t.Helper()
	originSignedURL, err := store.PresignGet(ctx, key, 5*time.Minute)
	if err != nil {
		t.Fatalf("generate origin URL for prefix proxy: %v", err)
	}
	originTarget, err := url.Parse(originSignedURL)
	if err != nil {
		t.Fatal(err)
	}
	originTarget.RawQuery = ""
	originTarget.ForceQuery = false

	proxyPrefixPath := "/signed-downloads"
	proxy := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		wantPath := proxyPrefixPath + "/" + escapeObjectKeyForURL(key)
		if request.URL.EscapedPath() != wantPath {
			http.Error(responseWriter, "unexpected proxy path", http.StatusBadRequest)
			return
		}
		target := *originTarget
		target.RawQuery = request.URL.RawQuery
		originRequest, requestErr := http.NewRequestWithContext(request.Context(), http.MethodGet, target.String(), nil)
		if requestErr != nil {
			http.Error(responseWriter, requestErr.Error(), http.StatusInternalServerError)
			return
		}
		originRequest.Header = request.Header.Clone()
		originResponse, requestErr := (&http.Client{Timeout: 30 * time.Second}).Do(originRequest)
		if requestErr != nil {
			http.Error(responseWriter, requestErr.Error(), http.StatusBadGateway)
			return
		}
		defer originResponse.Body.Close()
		for name, values := range originResponse.Header {
			for _, value := range values {
				responseWriter.Header().Add(name, value)
			}
		}
		responseWriter.WriteHeader(originResponse.StatusCode)
		_, _ = io.Copy(responseWriter, originResponse.Body)
	}))
	defer proxy.Close()

	prefixedStore := *store
	prefixedStore.downloadURLPrefix = proxy.URL + proxyPrefixPath
	prefixedURL, err := prefixedStore.PresignGet(ctx, key, 5*time.Minute)
	if err != nil {
		t.Fatalf("generate prefixed URL: %v", err)
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).Get(prefixedURL)
	if err != nil {
		t.Fatalf("prefixed GET request: %v", err)
	}
	defer response.Body.Close()
	got, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !bytes.Equal(got, want) {
		t.Fatalf("prefixed GET status=%d payload_match=%t", response.StatusCode, bytes.Equal(got, want))
	}
}

func urlWithoutSignature(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse signed URL: %w", err)
	}
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	return parsed.String(), nil
}

func assertPresignedPutCORS(t *testing.T, ctx context.Context, uploadURL, origin string) {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodOptions, uploadURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", origin)
	request.Header.Set("Access-Control-Request-Method", http.MethodPut)
	request.Header.Set("Access-Control-Request-Headers", "content-type")
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		t.Fatalf("CORS preflight request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		t.Fatalf("CORS preflight status=%d body=%s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	assertCORSAllowedOrigin(t, response.Header, origin)
	if !headerContainsToken(response.Header.Get("Access-Control-Allow-Methods"), http.MethodPut) {
		t.Fatalf("CORS allowed methods=%q, want PUT", response.Header.Get("Access-Control-Allow-Methods"))
	}
	if !headerContainsToken(response.Header.Get("Access-Control-Allow-Headers"), "content-type") && !headerContainsToken(response.Header.Get("Access-Control-Allow-Headers"), "*") {
		t.Fatalf("CORS allowed headers=%q, want Content-Type", response.Header.Get("Access-Control-Allow-Headers"))
	}
}

func assertCORSAllowedOrigin(t *testing.T, header http.Header, origin string) {
	t.Helper()
	allowedOrigin := header.Get("Access-Control-Allow-Origin")
	if allowedOrigin != "*" && allowedOrigin != origin {
		t.Fatalf("CORS allowed origin=%q, want %q or *", allowedOrigin, origin)
	}
}

func headerContainsToken(value, target string) bool {
	for _, item := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(item), target) {
			return true
		}
	}
	return false
}

func assertStoredObject(t *testing.T, ctx context.Context, store Storage, key string, want []byte) {
	t.Helper()
	size, _, err := store.Stat(ctx, key)
	if err != nil {
		t.Fatalf("Stat %s: %v", key, err)
	}
	if size != int64(len(want)) {
		t.Fatalf("Stat %s size=%d want=%d", key, size, len(want))
	}
	reader, err := store.Open(ctx, key)
	if err != nil {
		t.Fatalf("Open %s: %v", key, err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Open %s returned unexpected payload", key)
	}
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		panic(fmt.Sprintf("invalid %s=%q: %v", name, value, err))
	}
	return parsed
}
