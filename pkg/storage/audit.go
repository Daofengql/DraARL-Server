package storage

import (
	"context"
	"errors"
	"sort"
	"strings"
)

var (
	ErrInvalidObjectKey      = errors.New("invalid object key")
	ErrStorageNotInitialized = errors.New("storage is not initialized")
)

// AuditObject describes a final object which is not currently referenced by
// the database. The audit is intentionally read-only; callers decide whether
// an object should be retained or removed after inspecting the result.
type AuditObject struct {
	Key  string `json:"key"`
	Size int64  `json:"size"`
}

// AuditResult contains the object and byte counts for one managed prefix.
type AuditResult struct {
	Prefix              string        `json:"prefix"`
	ScannedObjects      int64         `json:"scanned_objects"`
	ScannedBytes        int64         `json:"scanned_bytes"`
	ReferencedObjects   int64         `json:"referenced_objects"`
	ReferencedBytes     int64         `json:"referenced_bytes"`
	UnreferencedObjects []AuditObject `json:"unreferenced_objects"`
	MissingReferences   []string      `json:"missing_references"`
}

// AuditPrefix walks a final-object prefix and compares it with the supplied
// database references. It never deletes or mutates storage.
func AuditPrefix(ctx context.Context, prefix string, references map[string]struct{}) (AuditResult, error) {
	prefix = strings.TrimLeft(strings.ReplaceAll(prefix, "\\", "/"), "/")
	if prefix == "" {
		return AuditResult{}, ErrInvalidObjectKey
	}
	result := AuditResult{
		Prefix:              prefix,
		UnreferencedObjects: make([]AuditObject, 0),
		MissingReferences:   make([]string, 0),
	}
	seen := make(map[string]struct{})
	store := Get()
	if store == nil {
		return AuditResult{}, ErrStorageNotInitialized
	}
	if err := store.Walk(ctx, prefix, func(object ObjectInfo) error {
		key := strings.TrimLeft(strings.ReplaceAll(object.Key, "\\", "/"), "/")
		if key == "" {
			return nil
		}
		seen[key] = struct{}{}
		result.ScannedObjects++
		result.ScannedBytes += object.Size
		if _, ok := references[key]; ok {
			result.ReferencedObjects++
			result.ReferencedBytes += object.Size
			return nil
		}
		result.UnreferencedObjects = append(result.UnreferencedObjects, AuditObject{Key: key, Size: object.Size})
		return nil
	}); err != nil {
		return AuditResult{}, err
	}
	for reference := range references {
		if strings.HasPrefix(reference, prefix) {
			if _, ok := seen[reference]; !ok {
				result.MissingReferences = append(result.MissingReferences, reference)
			}
		}
	}
	sort.Slice(result.UnreferencedObjects, func(i, j int) bool { return result.UnreferencedObjects[i].Key < result.UnreferencedObjects[j].Key })
	sort.Strings(result.MissingReferences)
	return result, nil
}
