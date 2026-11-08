package gormdb

import (
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// migrateSingleNodeAccess is restartable across MySQL's implicit DDL commits.
// Business rows and site links remain intact. Old server catalog rows are left
// unmanaged, with all edge access, ownership and credentials removed.
func migrateSingleNodeAccess(db *gorm.DB) error {
	if err := migrateCoreSchema(db); err != nil {
		return err
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		keys := []string{"node_id", "udp_host", "udp_port"}
		legacy := []string{"public_id", "udp_host", "udp_port"}
		for i, key := range keys {
			var rows []SiteConfig
			if err := tx.Where("config_key = ?", "access_discovery.center."+legacy[i]).Find(&rows).Error; err != nil {
				return err
			}
			if len(rows) == 0 {
				continue
			}
			row := SiteConfig{Key: "access_discovery." + key, Value: rows[0].Value, Category: CategoryAccessDiscovery}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
				return err
			}
		}
		allowed := make([]string, len(keys))
		for i, key := range keys {
			allowed[i] = "access_discovery." + key
		}
		return tx.Where("category = ? AND config_key NOT IN ?", CategoryAccessDiscovery, allowed).Delete(&SiteConfig{}).Error
	}); err != nil {
		return fmt.Errorf("migrate device endpoint settings: %w", err)
	}
	for table, columns := range map[string][]string{
		"public_groups": {"master_server", "slave_server"},
		"devices":       {"current_entry_node_id", "current_entry_session_id", "last_entry_node_id", "last_entry_at", "entry_mode"},
		"relay":         {"current_entry_node_id", "last_entry_node_id", "last_entry_at", "entry_mode"},
		"servers": {
			"join_key", "node_id", "display_name", "public_access_id", "public_access_enabled", "public_udp_host", "public_udp_port", "public_region", "public_network", "public_priority",
			"node_token_hash", "node_previous_token_hash", "node_previous_token_expires_at", "node_registration_token_hash", "node_registration_expires_at", "node_registered_at", "node_credential_epoch", "node_instance_id", "node_remote_addr", "node_control_session_id", "node_projection_version", "node_acked_projection_version", "node_last_seen_at", "node_connection_count", "node_device_in_packets", "node_device_in_bytes", "node_device_out_packets", "node_device_out_bytes", "node_interconnect_in_packets", "node_interconnect_in_bytes", "node_interconnect_out_packets", "node_interconnect_out_bytes", "node_metrics_sampled_at",
		},
	} {
		if !db.Migrator().HasTable(table) {
			continue
		}
		for _, column := range columns {
			if db.Migrator().HasColumn(table, column) {
				if err := db.Migrator().DropColumn(table, column); err != nil {
					return fmt.Errorf("remove obsolete %s.%s: %w", table, column, err)
				}
			}
		}
	}
	return nil
}
