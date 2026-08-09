package gormdb

import "gorm.io/gorm"

// ManagedStorageReferences returns final object keys that are referenced by
// the two immutable distribution domains. It deliberately excludes staging,
// legacy firmware paths, and mutable site assets.
func ManagedStorageReferences() (map[string]map[string]struct{}, error) {
	return managedStorageReferences(Get())
}

func managedStorageReferences(db *gorm.DB) (map[string]map[string]struct{}, error) {
	references := map[string]map[string]struct{}{
		"client-resources/": {},
		"firmware/":         {},
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
	return references, nil
}
