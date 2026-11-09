package gormdb

import (
	"testing"
)

func TestSingleNodeAccessUpgradeMySQL(t *testing.T) {
	db := openClientResourceMySQLTestDB(t)
	if err := migrateCoreSchema(db); err != nil {
		t.Fatal(err)
	}
	user := User{Name: "endpoint-upgrade", Email: "endpoint@example.invalid", CallSign: "UPGRADE", Status: 1}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	device := Device{Name: "ungrouped", OwnerID: user.ID, SSID: 1}
	if err := db.Create(&device).Error; err != nil {
		t.Fatal(err)
	}
	group := Group{Name: "preserved-source", Type: 1, OwerID: user.ID, Status: 1}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	link := InterCenterLink{LinkID: "preserved-site-link", LocalCenterID: "site-a", RemoteCenterID: "site-b", LocalGroupID: group.ID, RemoteGroupID: 888,
		InitiatorCenterID: "site-a", Direction: "bidirectional", Enabled: true, Accepted: true, ForwardAudio: true, VirtualDeviceName: "Server-B",
		SessionID: "preserved-session", SessionKeyCiphertext: "preserved-ciphertext", RemoteUDPAddress: "127.0.0.1:16051"}
	if err := db.Create(&link).Error; err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"ALTER TABLE devices ADD COLUMN current_entry_node_id VARCHAR(64), ADD COLUMN current_entry_session_id BIGINT UNSIGNED, ADD COLUMN last_entry_node_id VARCHAR(64), ADD COLUMN last_entry_at DATETIME, ADD COLUMN entry_mode VARCHAR(16)",
		"ALTER TABLE public_groups ADD COLUMN master_server INT, ADD COLUMN slave_server INT",
		"ALTER TABLE relay ADD COLUMN current_entry_node_id VARCHAR(64), ADD COLUMN last_entry_node_id VARCHAR(64), ADD COLUMN last_entry_at DATETIME, ADD COLUMN entry_mode VARCHAR(16)",
		"CREATE TABLE servers (id INT PRIMARY KEY, name VARCHAR(255), join_key VARCHAR(255), node_id VARCHAR(64), node_token_hash CHAR(64), public_access_id VARCHAR(64), UNIQUE INDEX idx_servers_node_id(node_id))",
		"INSERT INTO servers(id,name,join_key,node_id,node_token_hash) VALUES(1,'historical','obsolete','old-edge','obsolete')",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	for key, value := range map[string]string{"enabled": "false", "public_id": "site-a", "udp_host": "radio.example.com", "udp_port": "16050", "region": "removed", "priority": "-9"} {
		if err := db.Create(&SiteConfig{Key: "access_discovery.center." + key, Value: value, Category: CategoryAccessDiscovery}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&SiteConfig{Key: "access_discovery.token_ttl_seconds", Value: "1", Category: CategoryAccessDiscovery}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := migrateSingleNodeAccess(db); err != nil {
			t.Fatalf("migration pass %d: %v", i, err)
		}
	}
	c, err := (&SiteConfigRepository{db: db}).GetAccessDiscoveryConfig()
	if err != nil || c.NodeID != "site-a" || c.UDPHost != "radio.example.com" || c.UDPPort != 16050 {
		t.Fatalf("config=%+v err=%v", c, err)
	}
	for _, column := range []string{"current_entry_node_id", "current_entry_session_id", "last_entry_node_id", "last_entry_at", "entry_mode"} {
		if db.Migrator().HasColumn("devices", column) {
			t.Fatalf("obsolete device column %s", column)
		}
	}
	if db.Migrator().HasColumn("servers", "join_key") || db.Migrator().HasColumn("servers", "node_id") || db.Migrator().HasColumn("servers", "node_token_hash") || db.Migrator().HasIndex("servers", "idx_servers_node_id") {
		t.Fatal("edge credentials or index survived")
	}
	for _, column := range []string{"master_server", "slave_server"} {
		if db.Migrator().HasColumn("public_groups", column) {
			t.Fatal("obsolete group server reference survived", column)
		}
	}
	for _, column := range []string{"current_entry_node_id", "last_entry_node_id", "last_entry_at", "entry_mode"} {
		if db.Migrator().HasColumn("relay", column) {
			t.Fatal("obsolete relay ownership survived", column)
		}
	}
	var savedLink InterCenterLink
	if err := db.First(&savedLink, link.ID).Error; err != nil || savedLink.SessionID != link.SessionID || savedLink.SessionKeyCiphertext != link.SessionKeyCiphertext || savedLink.RemoteUDPAddress != link.RemoteUDPAddress || savedLink.LocalGroupID != group.ID || !savedLink.Accepted {
		t.Fatal("independent site session changed during edge cleanup", err)
	}
	var count int64
	if err := db.Table("servers").Where("id = 1 AND name = ?", "historical").Count(&count).Error; err != nil || count != 1 {
		t.Fatal("historical catalog row lost", err)
	}
	var saved Device
	if err := db.First(&saved, device.ID).Error; err != nil || saved.OwnerID != user.ID || saved.GroupID != nil {
		t.Fatal("ungrouped device changed", err)
	}
	if err := db.Model(&SiteConfig{}).Where("category = ?", CategoryAccessDiscovery).Count(&count).Error; err != nil || count != 3 {
		t.Fatal("obsolete endpoint settings survived", count, err)
	}
	// On a retry an administrator's newer endpoint must not be overwritten.
	if err := db.Model(&SiteConfig{}).Where("config_key = ?", "access_discovery.udp_host").Update("config_value", "updated.example.com").Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateSingleNodeAccess(db); err != nil {
		t.Fatal(err)
	}
	c, err = (&SiteConfigRepository{db: db}).GetAccessDiscoveryConfig()
	if err != nil || c.UDPHost != "updated.example.com" {
		t.Fatal("migration overwrote a newer endpoint", err)
	}
	// Clearing a NAT override must restore the request-host default, including
	// after a repeated migration. An empty value is a deliberate setting.
	c.UDPHost = ""
	repo := &SiteConfigRepository{db: db}
	if err := repo.SetAccessDiscoveryConfig(*c); err != nil {
		t.Fatal(err)
	}
	if err := migrateSingleNodeAccess(db); err != nil {
		t.Fatal(err)
	}
	c, err = repo.GetAccessDiscoveryConfig()
	if err != nil || c.UDPHost != "" || c.UDPPort != 16050 {
		t.Fatal("cleared NAT override was not preserved", err)
	}
}
