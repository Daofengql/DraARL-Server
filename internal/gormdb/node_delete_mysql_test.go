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

func TestDeleteEdgeNodeMySQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv(nodeMySQLTestDSNEnv))
	if dsn == "" {
		t.Skip("set " + nodeMySQLTestDSNEnv + " to run the MySQL edge-node deletion test")
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
	if err := db.AutoMigrate(&User{}, &Device{}, &Server{}); err != nil {
		t.Fatalf("migrate deletion test tables: %v", err)
	}

	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	user := &User{
		Name: "node-delete-" + suffix, Email: "node-delete-" + suffix + "@example.test",
		CallSign: "ND" + suffix[len(suffix)-8:], Status: 1,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create deletion test user: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Where("owner_id = ?", user.ID).Delete(&Device{}).Error
		_ = db.Delete(&User{}, user.ID).Error
	})

	nodeID := "edge-delete-" + suffix
	node := &Server{
		Name: "delete-test", DisplayName: "delete-test", Status: 1, ServerType: 3,
		NodeID: &nodeID, NodeTokenHash: credentialTestHash("delete-credential-" + suffix),
	}
	if err := db.Create(node).Error; err != nil {
		t.Fatalf("create deletion test node: %v", err)
	}
	t.Cleanup(func() { _ = db.Where("node_id = ?", nodeID).Delete(&Server{}).Error })

	attached := &Device{Name: "attached", OwnerID: user.ID, SSID: 1, CurrentEntryNodeID: nodeID, CurrentEntrySessionID: 42, ISOnline: true}
	unrelated := &Device{Name: "unrelated", OwnerID: user.ID, SSID: 2, CurrentEntryNodeID: "center", CurrentEntrySessionID: 7, ISOnline: true}
	if err := db.Create([]*Device{attached, unrelated}).Error; err != nil {
		t.Fatalf("create deletion test devices: %v", err)
	}

	repo := &ServerRepository{db: db}
	deleted, err := repo.DeleteEdgeNode(node.ID)
	if err != nil {
		t.Fatalf("delete edge node: %v", err)
	}
	if deleted.Node.NodeID == nil || *deleted.Node.NodeID != nodeID || len(deleted.Devices) != 1 || deleted.Devices[0].ID != attached.ID {
		t.Fatalf("unexpected deletion result: %#v", deleted)
	}
	if found, err := repo.GetServerByNodeID(nodeID); err != nil || found != nil {
		t.Fatalf("deleted node remains queryable: node=%#v err=%v", found, err)
	}
	if result, authErr := repo.AuthenticateNode(nodeID, node.NodeTokenHash, credentialTestHash("unused"), time.Now()); !errors.Is(authErr, ErrNodeNotFound) || result.Accepted {
		t.Fatalf("deleted node credential still authenticates: result=%#v err=%v", result, authErr)
	}

	var gotAttached, gotUnrelated Device
	if err := db.First(&gotAttached, attached.ID).Error; err != nil {
		t.Fatal(err)
	}
	if gotAttached.CurrentEntryNodeID != "" || gotAttached.CurrentEntrySessionID != 0 || gotAttached.ISOnline {
		t.Fatalf("attached device ownership was not cleared: %#v", gotAttached)
	}
	if err := db.First(&gotUnrelated, unrelated.ID).Error; err != nil {
		t.Fatal(err)
	}
	if gotUnrelated.CurrentEntryNodeID != "center" || gotUnrelated.CurrentEntrySessionID != 7 || !gotUnrelated.ISOnline {
		t.Fatalf("unrelated device was modified: %#v", gotUnrelated)
	}
	if _, err := repo.DeleteEdgeNode(node.ID); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("second deletion returned %v, want ErrNodeNotFound", err)
	}

	legacy := &Server{Name: "legacy-delete-guard-" + suffix, Status: 1}
	if err := db.Create(legacy).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Delete(&Server{}, legacy.ID).Error })
	if _, err := repo.DeleteEdgeNode(legacy.ID); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("legacy server deletion returned %v, want ErrNodeNotFound", err)
	}
	if found, err := repo.GetServerByID(legacy.ID); err != nil || found == nil {
		t.Fatalf("legacy server was deleted: server=%#v err=%v", found, err)
	}
}
