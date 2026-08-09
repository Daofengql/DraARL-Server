package gormdb

import (
	broadcastmodel "draarl/internal/broadcast/model"

	"gorm.io/gorm"
)

// ManagedStorageReferences returns final object keys that are referenced by
// managed immutable object domains. It deliberately excludes staging, legacy
// firmware paths, and mutable site assets.
func ManagedStorageReferences() (map[string]map[string]struct{}, error) {
	return managedStorageReferences(Get())
}

func managedStorageReferences(db *gorm.DB) (map[string]map[string]struct{}, error) {
	references := map[string]map[string]struct{}{
		"client-resources/": {},
		"firmware/":         {},
		"broadcast-audios/": {},
	}
	var clientKeys []string
	if err := db.Model(&ClientResourceArtifact{}).
		Where("storage_key IS NOT NULL AND storage_key <> ''").
		Pluck("storage_key", &clientKeys).Error; err != nil {
		return nil, err
	}
	for _, key := range clientKeys {
		references["client-resources/"][key] = struct{}{}
	}
	var firmwareKeys []string
	if err := db.Model(&FirmwareRelease{}).
		Where("minio_path IS NOT NULL AND minio_path <> ''").
		Pluck("minio_path", &firmwareKeys).Error; err != nil {
		return nil, err
	}
	for _, key := range firmwareKeys {
		references["firmware/"][key] = struct{}{}
	}
	type broadcastKeys struct {
		Original string
		Playback string
	}
	var audioKeys []broadcastKeys
	if err := db.Model(&broadcastmodel.BroadcastAudio{}).
		Select("original_object_key AS original, playback_object_key AS playback").
		Scan(&audioKeys).Error; err != nil {
		return nil, err
	}
	for _, keys := range audioKeys {
		if keys.Original != "" {
			references["broadcast-audios/"][keys.Original] = struct{}{}
		}
		if keys.Playback != "" {
			references["broadcast-audios/"][keys.Playback] = struct{}{}
		}
	}
	return references, nil
}
