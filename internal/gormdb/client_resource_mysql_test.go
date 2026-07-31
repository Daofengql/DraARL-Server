package gormdb

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestClientResourceLifecycleMySQL(t *testing.T) {
	db := openClientResourceMySQLTestDB(t)
	if err := db.AutoMigrate(&ClientResource{}, &ClientResourceRelease{}, &ClientResourceArtifact{}, &ClientResourceArtifactTarget{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	key := fmt.Sprintf("test/model-%x", time.Now().UnixNano())
	resource := &ClientResource{ResourceKey: key, Name: "test model", Category: "model", Enabled: true}
	repo := newClientResourceRepository(db)
	if err := repo.CreateResource(resource); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		var releases []int
		_ = db.Model(&ClientResourceRelease{}).Where("resource_id = ?", resource.ID).Pluck("id", &releases).Error
		if len(releases) > 0 {
			var artifacts []int
			_ = db.Model(&ClientResourceArtifact{}).Where("release_id IN ?", releases).Pluck("id", &artifacts).Error
			if len(artifacts) > 0 {
				_ = db.Where("artifact_id IN ?", artifacts).Delete(&ClientResourceArtifactTarget{}).Error
			}
			_ = db.Where("release_id IN ?", releases).Delete(&ClientResourceArtifact{}).Error
			_ = db.Where("id IN ?", releases).Delete(&ClientResourceRelease{}).Error
		}
		_ = db.Delete(&ClientResource{}, resource.ID).Error
	})

	release := &ClientResourceRelease{ResourceID: resource.ID, Version: "1.0.0", Channel: "stable", Status: ClientResourceReleaseStatusDraft}
	if err := repo.CreateRelease(release); err != nil {
		t.Fatal(err)
	}
	rollbackCalled := false
	invalid := &ClientResourceArtifact{
		ReleaseID: release.ID, Format: "onnx", Runtime: "cpu", Variant: "invalid", FileName: "invalid.onnx",
		StorageKey: "client-resources/test/invalid.onnx", Metadata: "not-json",
		Targets: []ClientResourceArtifactTarget{{Platform: "windows", Arch: "x86_64"}},
	}
	if err := repo.CompleteArtifact(invalid, func(*ClientResourceRelease) error { return nil }, func() { rollbackCalled = true }); err == nil || !rollbackCalled {
		t.Fatalf("failed transaction err=%v rollback_called=%t", err, rollbackCalled)
	}
	artifact := &ClientResourceArtifact{
		ReleaseID: release.ID, Format: "onnx", Runtime: "cpu", Variant: "default", FileName: "model.onnx",
		StorageKey: "client-resources/test/model.onnx", FileSize: 10, SHA256: strings.Repeat("a", 64),
		Targets: []ClientResourceArtifactTarget{{Platform: "windows", Arch: "x86_64"}, {Platform: "macos", Arch: "arm64"}},
	}
	if err := repo.CompleteArtifact(artifact, nil, nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	loaded, err := repo.GetReleaseByID(release.ID)
	if err != nil || len(loaded.Artifacts) != 1 || len(loaded.Artifacts[0].Targets) != 2 || loaded.Artifacts[0].Metadata != "{}" {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}

	conflict := &ClientResourceArtifact{
		ReleaseID: release.ID, Format: "onnx", Runtime: "cpu", Variant: "default", FileName: "other.onnx",
		StorageKey: "client-resources/test/other.onnx", Targets: []ClientResourceArtifactTarget{{Platform: "macos", Arch: "arm64"}},
	}
	if err := repo.CompleteArtifact(conflict, nil, nil); !errors.Is(err, ErrClientResourceTargetConflict) {
		t.Fatalf("conflict err=%v", err)
	}

	published, err := repo.PublishRelease(release.ID)
	if err != nil || published.Status != ClientResourceReleaseStatusPublished || published.PublishedAt == nil {
		t.Fatalf("published=%#v err=%v", published, err)
	}
	if err := repo.CompleteArtifact(&ClientResourceArtifact{ReleaseID: release.ID, Format: "onnx", Runtime: "gpu", Variant: "default", Targets: []ClientResourceArtifactTarget{{Platform: "windows", Arch: "x86_64"}}}, nil, nil); !errors.Is(err, ErrClientResourceReleaseNotDraft) {
		t.Fatalf("published mutation err=%v", err)
	}
	manifest, err := repo.ListManifestArtifacts(ClientResourceManifestLookup{Channel: "stable", Platform: "macos", Arch: "arm64"})
	if err != nil || len(manifest) != 1 || len(manifest[0].Targets) != 2 {
		t.Fatalf("manifest=%#v err=%v", manifest, err)
	}
	if _, err := repo.WithdrawRelease(release.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetDownloadableArtifact(artifact.ID); !errors.Is(err, ErrClientResourceNotPublished) {
		t.Fatalf("withdrawn download err=%v", err)
	}

	updated, err := repo.UpdateResource(resource.ID, ClientResourceUpdate{ResourceKey: resource.ResourceKey, Name: resource.Name, Category: resource.Category, Enabled: false})
	if err != nil || updated.Enabled {
		t.Fatalf("disable=%#v err=%v", updated, err)
	}
	if err := repo.CreateRelease(&ClientResourceRelease{ResourceID: resource.ID, Version: "2.0.0", Channel: "stable", Status: ClientResourceReleaseStatusDraft}); !errors.Is(err, ErrClientResourceDisabled) {
		t.Fatalf("disabled create err=%v", err)
	}
	if _, err := repo.UpdateResource(resource.ID, ClientResourceUpdate{ResourceKey: key + "-changed", Name: resource.Name, Category: resource.Category, Enabled: false}); !errors.Is(err, ErrClientResourceKeyImmutable) {
		t.Fatalf("immutable key err=%v", err)
	}

	deleted, err := repo.DeleteResource(resource.ID)
	if err != nil {
		t.Fatalf("delete resource: %v", err)
	}
	if len(deleted.Releases) != 1 || len(deleted.Releases[0].Artifacts) != 1 || len(deleted.Releases[0].Artifacts[0].Targets) != 2 {
		t.Fatalf("deleted resource graph=%#v", deleted)
	}
	for name, model := range map[string]any{
		"resources": &ClientResource{}, "releases": &ClientResourceRelease{},
		"artifacts": &ClientResourceArtifact{}, "targets": &ClientResourceArtifactTarget{},
	} {
		var count int64
		query := db.Model(model)
		switch name {
		case "resources":
			query = query.Where("id = ?", resource.ID)
		case "releases":
			query = query.Where("resource_id = ?", resource.ID)
		case "artifacts":
			query = query.Where("release_id = ?", release.ID)
		case "targets":
			query = query.Where("artifact_id = ?", artifact.ID)
		}
		if err := query.Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("cascade delete %s count=%d err=%v", name, count, err)
		}
	}
}

func TestLegacyClientReleaseMigrationGuardMySQL(t *testing.T) {
	db := openClientResourceMySQLTestDB(t)
	legacyTables := []string{"client_release_artifacts", "client_releases"}
	for _, tableName := range legacyTables {
		if db.Migrator().HasTable(tableName) {
			t.Fatalf("legacy table %s already exists in the dedicated test database", tableName)
		}
	}
	t.Cleanup(func() {
		_ = db.Exec("SET FOREIGN_KEY_CHECKS = 0").Error
		for _, tableName := range legacyTables {
			_ = db.Migrator().DropTable(tableName)
		}
		_ = db.Exec("SET FOREIGN_KEY_CHECKS = 1").Error
	})

	if err := db.Exec(`CREATE TABLE client_releases (id BIGINT PRIMARY KEY) ENGINE=InnoDB`).Error; err != nil {
		t.Fatalf("create legacy releases: %v", err)
	}
	if err := db.Exec(`CREATE TABLE client_release_artifacts (id BIGINT PRIMARY KEY, release_id BIGINT, CONSTRAINT fk_legacy_release FOREIGN KEY (release_id) REFERENCES client_releases(id)) ENGINE=InnoDB`).Error; err != nil {
		t.Fatalf("create legacy artifacts: %v", err)
	}
	if err := db.Exec(`INSERT INTO client_releases (id) VALUES (1)`).Error; err != nil {
		t.Fatalf("insert legacy release: %v", err)
	}

	err := dropEmptyLegacyClientReleaseTables(db)
	if err == nil || !strings.Contains(err.Error(), "contains 1 rows") {
		t.Fatalf("non-empty legacy migration error=%v", err)
	}
	for _, tableName := range legacyTables {
		if !db.Migrator().HasTable(tableName) {
			t.Fatalf("guard unexpectedly dropped %s", tableName)
		}
	}

	if err := db.Exec(`DELETE FROM client_releases`).Error; err != nil {
		t.Fatalf("clear legacy release: %v", err)
	}
	if err := dropEmptyLegacyClientReleaseTables(db); err != nil {
		t.Fatalf("drop empty legacy tables: %v", err)
	}
	for _, tableName := range legacyTables {
		if db.Migrator().HasTable(tableName) {
			t.Fatalf("empty legacy table %s was not dropped", tableName)
		}
	}
}

func openClientResourceMySQLTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(nodeMySQLTestDSNEnv))
	if dsn == "" {
		t.Skip("set " + nodeMySQLTestDSNEnv + " to run client resource MySQL tests")
	}
	parsed, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse MySQL test DSN: %v", err)
	}
	if !strings.HasPrefix(parsed.DBName, "draarl_test_") {
		t.Fatalf("refusing non-test database %q", parsed.DBName)
	}
	parsed.ParseTime = true
	db, err := gorm.Open(gormmysql.Open(parsed.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open MySQL: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get MySQL connection: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}
