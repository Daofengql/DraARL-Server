package gormdb

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestClientReleaseLifecycleMySQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv(nodeMySQLTestDSNEnv))
	if dsn == "" {
		t.Skip("set " + nodeMySQLTestDSNEnv + " to run the client release lifecycle test")
	}
	parsed, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse MySQL test DSN: %v", err)
	}
	if !strings.HasPrefix(parsed.DBName, "draarl_test_") {
		t.Fatalf("refusing non-test database %q; name must start with draarl_test_", parsed.DBName)
	}
	parsed.ParseTime = true
	db, err := gorm.Open(gormmysql.Open(parsed.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open MySQL test database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&ClientRelease{}, &ClientReleaseArtifact{}); err != nil {
		t.Fatalf("migrate client release tables: %v", err)
	}

	appID := fmt.Sprintf("client-release-contract-%x", time.Now().UnixNano())
	t.Cleanup(func() {
		_ = db.Where("release_id IN (SELECT id FROM client_releases WHERE app_id = ?)", appID).Delete(&ClientReleaseArtifact{}).Error
		_ = db.Where("app_id = ?", appID).Delete(&ClientRelease{}).Error
	})
	repo := newClientReleaseRepository(db)

	first := &ClientRelease{AppID: appID, Version: "1.0.0", Channel: "stable", Status: ClientReleaseStatusDraft}
	if err := repo.Create(first); err != nil {
		t.Fatalf("create first draft: %v", err)
	}
	firstArtifact := &ClientReleaseArtifact{
		ReleaseID: first.ID, Platform: "android", Arch: "arm64", AndroidABI: "arm64-v8a",
		PackageType: "apk", FileName: "app-arm64.apk", StorageKey: "client-releases/contract/1.0.0/app-arm64.apk",
		FileSize: 7, SHA256: strings.Repeat("a", 64),
	}
	if err := repo.AddArtifact(firstArtifact); err != nil {
		t.Fatalf("add first artifact: %v", err)
	}
	duplicate := *firstArtifact
	duplicate.ID = 0
	duplicate.StorageKey = "client-releases/contract/1.0.0/duplicate.apk"
	if err := repo.AddArtifact(&duplicate); !IsDuplicateKeyError(err) {
		t.Fatalf("duplicate target error=%v, want duplicate-key error", err)
	}
	published, err := repo.Publish(first.ID)
	if err != nil || published.Status != ClientReleaseStatusPublished || published.PublishedAt == nil {
		t.Fatalf("publish result=%#v err=%v", published, err)
	}
	if err := repo.AddArtifact(&ClientReleaseArtifact{
		ReleaseID: first.ID, Platform: "android", Arch: "armv7", PackageType: "apk",
		StorageKey: "client-releases/contract/1.0.0/app-armv7.apk",
	}); !errors.Is(err, ErrClientReleaseNotDraft) {
		t.Fatalf("published release accepted another artifact: %v", err)
	}
	withdrawn, err := repo.Withdraw(first.ID)
	if err != nil || withdrawn.Status != ClientReleaseStatusWithdrawn {
		t.Fatalf("withdraw result=%#v err=%v", withdrawn, err)
	}
	if _, err := repo.DeleteDraft(first.ID); !errors.Is(err, ErrClientReleaseNotDraft) {
		t.Fatalf("withdrawn release was deleted: %v", err)
	}
	withdrawnCandidates, err := repo.ListPublishedArtifacts(ClientArtifactLookup{
		AppID: appID, Channel: "stable", Platform: "android", Architectures: []string{"arm64", "universal"},
	})
	if err != nil || len(withdrawnCandidates) != 0 {
		t.Fatalf("withdrawn release remained public: count=%d err=%v", len(withdrawnCandidates), err)
	}

	second := &ClientRelease{AppID: appID, Version: "1.1.0", Channel: "stable", Status: ClientReleaseStatusDraft}
	if err := repo.Create(second); err != nil {
		t.Fatalf("create second draft: %v", err)
	}
	if err := repo.AddArtifact(&ClientReleaseArtifact{
		ReleaseID: second.ID, Platform: "android", Arch: "universal", AndroidABI: "universal",
		PackageType: "apk", FileName: "app-universal.apk", StorageKey: "client-releases/contract/1.1.0/app-universal.apk",
		FileSize: 9, SHA256: strings.Repeat("b", 64),
	}); err != nil {
		t.Fatalf("add universal artifact: %v", err)
	}
	if _, err := repo.Publish(second.ID); err != nil {
		t.Fatalf("publish second release: %v", err)
	}
	listed, total, err := repo.List(ClientReleaseListFilter{AppID: appID, Version: "1.1.0", Page: 1, PageSize: 20})
	if err != nil || total != 1 || len(listed) != 1 || listed[0].ID != second.ID {
		t.Fatalf("version filter total=%d items=%d err=%v", total, len(listed), err)
	}
	candidates, err := repo.ListPublishedArtifacts(ClientArtifactLookup{
		AppID: appID, Channel: "stable", Platform: "android", Architectures: []string{"armv7", "universal"},
	})
	if err != nil || len(candidates) != 1 || candidates[0].Arch != "universal" {
		t.Fatalf("universal lookup candidates=%#v err=%v", candidates, err)
	}

	draft := &ClientRelease{AppID: appID, Version: "2.0.0", Channel: "beta", Status: ClientReleaseStatusDraft}
	if err := repo.Create(draft); err != nil {
		t.Fatalf("create deletable draft: %v", err)
	}
	if _, err := repo.DeleteDraft(draft.ID); err != nil {
		t.Fatalf("delete draft: %v", err)
	}
	if _, err := repo.GetByID(draft.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deleted draft still exists: %v", err)
	}

	concurrent := &ClientRelease{AppID: appID, Version: "3.0.0", Channel: "beta", Status: ClientReleaseStatusDraft}
	if err := repo.Create(concurrent); err != nil {
		t.Fatalf("create concurrent draft: %v", err)
	}
	var promotions atomic.Int32
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			artifact := &ClientReleaseArtifact{
				ReleaseID: concurrent.ID, Platform: "android", Arch: "armv7", PackageType: "apk", FileName: "concurrent.apk",
			}
			err := repo.CompleteArtifact(artifact, func(_ *ClientRelease) error {
				promotions.Add(1)
				time.Sleep(100 * time.Millisecond)
				artifact.StorageKey = fmt.Sprintf("client-releases/contract/3.0.0/concurrent-%d.apk", i)
				return nil
			}, nil)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	succeeded, duplicated := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			succeeded++
		case IsDuplicateKeyError(err):
			duplicated++
		default:
			t.Fatalf("concurrent complete returned unexpected error: %v", err)
		}
	}
	if succeeded != 1 || duplicated != 1 || promotions.Load() != 1 {
		t.Fatalf("concurrent complete succeeded=%d duplicate=%d promotions=%d, want 1/1/1", succeeded, duplicated, promotions.Load())
	}

	rollbackDraft := &ClientRelease{AppID: appID, Version: "4.0.0", Channel: "beta", Status: ClientReleaseStatusDraft}
	if err := repo.Create(rollbackDraft); err != nil {
		t.Fatalf("create rollback draft: %v", err)
	}
	promoted, cleaned := false, false
	err = repo.CompleteArtifact(&ClientReleaseArtifact{
		ReleaseID: rollbackDraft.ID, Platform: "windows", Arch: "x86_64", PackageType: "exe",
		FileName: strings.Repeat("x", 300), StorageKey: "client-releases/contract/4.0.0/oversized.exe",
	}, func(_ *ClientRelease) error {
		promoted = true
		return nil
	}, func() {
		cleaned = true
	})
	if err == nil || !promoted || !cleaned {
		t.Fatalf("failed insert err=%v promoted=%t cleaned=%t, want error/true/true", err, promoted, cleaned)
	}

	formatRelease := &ClientRelease{AppID: appID, Version: "5.0.0", Channel: "beta", Status: ClientReleaseStatusDraft}
	if err := repo.Create(formatRelease); err != nil {
		t.Fatalf("create package-format draft: %v", err)
	}
	for _, packageType := range []string{"exe", "msix"} {
		if err := repo.AddArtifact(&ClientReleaseArtifact{
			ReleaseID: formatRelease.ID, Platform: "windows", Arch: "x86_64", PackageType: packageType,
			FileName: "client." + packageType, StorageKey: "client-releases/contract/5.0.0/client." + packageType,
		}); err != nil {
			t.Fatalf("add %s artifact: %v", packageType, err)
		}
	}
	if _, err := repo.Publish(formatRelease.ID); err != nil {
		t.Fatalf("publish package-format release: %v", err)
	}
	for _, packageType := range []string{"exe", "msix"} {
		found, err := repo.ListPublishedArtifacts(ClientArtifactLookup{
			AppID: appID, Channel: "beta", Platform: "windows", PackageType: packageType, Architectures: []string{"x86_64"},
		})
		if err != nil || len(found) != 1 || found[0].PackageType != packageType {
			t.Fatalf("package_type=%s candidates=%#v err=%v", packageType, found, err)
		}
	}
}
