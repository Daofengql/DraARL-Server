package gormdb

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"draarl/pkg/crypto"
)

const siteConfigCryptoE2EEnv = "DRAARL_SITECONFIG_CRYPTO_E2E"

func TestSiteConfigLegacyPlaintextLazyEncryptionMySQL(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv(siteConfigCryptoE2EEnv)), "true") {
		t.Skip("set " + siteConfigCryptoE2EEnv + "=true and DRAARL_TEST_MYSQL_DSN to run the site-config crypto E2E")
	}
	dsn := strings.TrimSpace(os.Getenv("DRAARL_TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Fatal("DRAARL_TEST_MYSQL_DSN is required")
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
		t.Fatalf("open MySQL test database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&SiteConfig{}); err != nil {
		t.Fatalf("migrate site_configs: %v", err)
	}
	if err := crypto.InitAES("01234567890123456789012345678901"); err != nil {
		t.Fatal(err)
	}

	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	category := "codex-site-config-" + suffix
	legacyKey := "codex.smtp.password." + suffix
	ordinaryKey := "codex.smtp.host." + suffix
	sealedKey := "codex.api_token." + suffix
	concurrentKey := "codex.client_secret." + suffix
	legacyValue := "legacy-secret-" + suffix
	ordinaryValue := "smtp.example.test"
	staleValue := "stale-secret-" + suffix
	concurrentValue := "concurrent-secret-" + suffix
	sealedValue, err := sealSiteConfigValue(sealedKey, "already-sealed-"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	rows := []SiteConfig{
		{Key: legacyKey, Value: legacyValue, Category: category},
		{Key: ordinaryKey, Value: ordinaryValue, Category: category},
		{Key: sealedKey, Value: sealedValue, Category: category},
		{Key: concurrentKey, Value: staleValue, Category: category},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("create site-config fixtures: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Unscoped().Where("category = ?", category).Delete(&SiteConfig{}).Error
	})

	repo := &SiteConfigRepository{db: db}
	staleConfig := rows[3]
	if err := repo.Set(concurrentKey, concurrentValue, category, ""); err != nil {
		t.Fatalf("write concurrent replacement: %v", err)
	}
	if err := repo.unsealAndMigrateSiteConfig(&staleConfig); err != nil {
		t.Fatalf("migrate stale snapshot: %v", err)
	}
	configs, err := repo.GetByCategory(category)
	if err != nil {
		t.Fatalf("read category: %v", err)
	}
	values := make(map[string]string, len(configs))
	for _, config := range configs {
		values[config.Key] = config.Value
	}
	if values[legacyKey] != legacyValue || values[ordinaryKey] != ordinaryValue || values[sealedKey] != "already-sealed-"+suffix || values[concurrentKey] != concurrentValue {
		t.Fatalf("unexpected plaintext readback: %#v", values)
	}

	var stored []SiteConfig
	if err := db.Where("category = ?", category).Find(&stored).Error; err != nil {
		t.Fatalf("reload stored values: %v", err)
	}
	storedValues := make(map[string]string, len(stored))
	for _, config := range stored {
		storedValues[config.Key] = config.Value
	}
	if !strings.HasPrefix(storedValues[legacyKey], encryptedSiteConfigPrefix) || strings.Contains(storedValues[legacyKey], legacyValue) {
		t.Fatalf("legacy secret was not encrypted at rest: %q", storedValues[legacyKey])
	}
	if opened, err := unsealSiteConfigValue(legacyKey, storedValues[legacyKey]); err != nil || opened != legacyValue {
		t.Fatalf("lazy-encrypted value cannot be opened: value=%q err=%v", opened, err)
	}
	if storedValues[ordinaryKey] != ordinaryValue {
		t.Fatalf("ordinary configuration changed: %q", storedValues[ordinaryKey])
	}
	if storedValues[sealedKey] != sealedValue {
		t.Fatalf("existing ciphertext was rewritten: got=%q want=%q", storedValues[sealedKey], sealedValue)
	}
	if opened, err := unsealSiteConfigValue(concurrentKey, storedValues[concurrentKey]); err != nil || opened != concurrentValue {
		t.Fatalf("stale migration overwrote concurrent update: value=%q err=%v", opened, err)
	}
}
