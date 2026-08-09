package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"draarl/internal/config"
)

// UploadGrant binds a staged object to the authenticated user and declared metadata.
type UploadGrant struct {
	ObjectKey   string `json:"object_key"`
	FileType    string `json:"file_type"`
	UserID      int    `json:"user_id"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	ExpiresAt   int64  `json:"expires_at"`
}

func CreateUploadGrant(objectKey, fileType string, userID int, size int64, contentType string, expiry time.Duration) (string, error) {
	if expiry <= 0 {
		expiry = 15 * time.Minute
	}
	grant := UploadGrant{
		ObjectKey:   strings.TrimLeft(objectKey, "/"),
		FileType:    strings.ToLower(strings.TrimSpace(fileType)),
		UserID:      userID,
		Size:        size,
		ContentType: contentType,
		ExpiresAt:   time.Now().Add(expiry).Unix(),
	}
	if grant.ObjectKey == "" || grant.FileType == "" || grant.UserID <= 0 || grant.Size <= 0 {
		return "", fmt.Errorf("invalid upload grant")
	}
	return signToken(config.Get().JWT.Secret, grant)
}

func VerifyUploadGrant(token, objectKey, fileType string, userID int) (*UploadGrant, error) {
	var grant UploadGrant
	if err := verifyToken(config.Get().JWT.Secret, token, &grant); err != nil {
		return nil, fmt.Errorf("invalid upload grant: %w", err)
	}
	if time.Now().Unix() > grant.ExpiresAt {
		return nil, fmt.Errorf("upload grant expired")
	}
	if grant.ObjectKey != strings.TrimLeft(objectKey, "/") ||
		grant.FileType != strings.ToLower(strings.TrimSpace(fileType)) ||
		grant.UserID != userID {
		return nil, fmt.Errorf("upload grant does not match request")
	}
	if grant.Size <= 0 {
		return nil, fmt.Errorf("upload grant has invalid size")
	}
	return &grant, nil
}

func ValidateStagedUpload(ctx context.Context, token, objectKey, fileType string, userID int) (*UploadGrant, string, error) {
	grant, err := VerifyUploadGrant(token, objectKey, fileType, userID)
	if err != nil {
		return nil, "", err
	}
	if !IsStagingObjectKey(grant.ObjectKey, grant.FileType, grant.UserID) {
		return nil, "", fmt.Errorf("invalid staging object key")
	}
	size, contentType, err := Stat(ctx, grant.ObjectKey)
	if err != nil {
		return nil, "", fmt.Errorf("staging object does not exist: %w", err)
	}
	if size != grant.Size {
		_ = Delete(ctx, grant.ObjectKey)
		return nil, "", fmt.Errorf("uploaded size does not match grant: got %d want %d", size, grant.Size)
	}
	if size > MaxSizeForFileType(grant.FileType) {
		_ = Delete(ctx, grant.ObjectKey)
		return nil, "", fmt.Errorf("uploaded object exceeds size limit")
	}
	return grant, contentType, nil
}

func PromoteStagedUpload(ctx context.Context, grant *UploadGrant) (string, error) {
	if grant == nil {
		return "", fmt.Errorf("upload grant is nil")
	}
	finalKey := NewObjectKey(grant.FileType, ExtFromFilename(grant.ObjectKey))
	if err := Promote(ctx, grant.ObjectKey, finalKey); err != nil {
		return "", err
	}
	size, _, err := Stat(ctx, finalKey)
	if err != nil || size != grant.Size {
		_ = Delete(ctx, finalKey)
		if err != nil {
			return "", fmt.Errorf("verify final object: %w", err)
		}
		return "", fmt.Errorf("final object size mismatch: got %d want %d", size, grant.Size)
	}
	return finalKey, nil
}

func signToken(secret string, claims any) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("signing secret is empty")
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(encodedPayload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encodedPayload + "." + signature, nil
}

func verifyToken(secret, token string, claims any) error {
	if secret == "" {
		return fmt.Errorf("signing secret is empty")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return fmt.Errorf("invalid token format")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0]))
	expected := mac.Sum(nil)
	actual, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(expected, actual) {
		return fmt.Errorf("invalid token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("invalid token payload")
	}
	if err := json.Unmarshal(payload, claims); err != nil {
		return fmt.Errorf("invalid token claims")
	}
	return nil
}
