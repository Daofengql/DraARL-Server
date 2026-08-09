package storage

import (
	"context"
	"strings"
	"testing"

	"draarl/internal/config"
)

func TestAuditPrefixIsReadOnlyAndReportsOrphansAndMissingReferences(t *testing.T) {
	cfg := &config.Configuration{}
	cfg.JWT.Secret = "storage-audit-test-secret"
	cfg.Storage.ActiveProfile = "audit-local"
	cfg.Storage.Profiles = map[string]config.StorageProfile{
		"audit-local": {Driver: DriverLocal, Local: config.LocalStorageConfig{RootPath: t.TempDir(), BaseURL: "/files"}},
	}
	if err := Init(cfg); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for key, value := range map[string]string{
		"client-resources/model/a/sha/a.bin":      "one",
		"client-resources/model/orphan/sha/b.bin": "two",
	} {
		if err := Put(ctx, key, strings.NewReader(value), int64(len(value)), "application/octet-stream"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = Delete(ctx, key) })
	}
	result, err := AuditPrefix(ctx, "client-resources/", map[string]struct{}{
		"client-resources/model/a/sha/a.bin":       {},
		"client-resources/model/missing/sha/c.bin": {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ScannedObjects != 2 || result.ScannedBytes != 6 || result.ReferencedObjects != 1 || result.ReferencedBytes != 3 {
		t.Fatalf("unexpected audit counts: %#v", result)
	}
	if len(result.UnreferencedObjects) != 1 || result.UnreferencedObjects[0].Key != "client-resources/model/orphan/sha/b.bin" {
		t.Fatalf("unexpected orphan list: %#v", result.UnreferencedObjects)
	}
	if len(result.MissingReferences) != 1 || result.MissingReferences[0] != "client-resources/model/missing/sha/c.bin" {
		t.Fatalf("unexpected missing references: %#v", result.MissingReferences)
	}
	if _, _, err := Stat(ctx, "client-resources/model/orphan/sha/b.bin"); err != nil {
		t.Fatalf("audit unexpectedly mutated storage: %v", err)
	}

	empty, err := AuditPrefix(ctx, "firmware/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if empty.UnreferencedObjects == nil || empty.MissingReferences == nil {
		t.Fatalf("empty audit collections must encode as arrays: %#v", empty)
	}
}
