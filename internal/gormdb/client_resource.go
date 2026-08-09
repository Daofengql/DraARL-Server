package gormdb

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrClientResourceDisabled           = errors.New("client resource is disabled")
	ErrClientResourceKeyImmutable       = errors.New("client resource key is immutable after publishing")
	ErrClientResourceReleaseNotDraft    = errors.New("client resource release is not a draft")
	ErrClientResourceReleaseNotEditable = errors.New("client resource release does not accept artifacts")
	ErrClientResourceNotPublished       = errors.New("client resource release is not published")
	ErrClientResourceHasNoArtifact      = errors.New("client resource release has no artifacts")
	ErrClientResourceTargetRequired     = errors.New("client resource artifact has no targets")
	ErrClientResourceTargetConflict     = errors.New("client resource artifact target conflicts with an existing artifact")
)

type ClientResourceRepository struct {
	db *gorm.DB
}

func NewClientResourceRepository() *ClientResourceRepository {
	return &ClientResourceRepository{db: Get()}
}

func newClientResourceRepository(db *gorm.DB) *ClientResourceRepository {
	return &ClientResourceRepository{db: db}
}

type ClientResourceListFilter struct {
	ResourceKey string
	Name        string
	Category    string
	Enabled     *bool
	Page        int
	PageSize    int
}

type ClientResourceReleaseListFilter struct {
	ResourceID int
	Version    string
	Channel    string
	Status     string
	Platform   string
	Arch       string
	Page       int
	PageSize   int
}

type ClientResourceUpdate struct {
	ResourceKey string
	Name        string
	Category    string
	Description string
	Required    bool
	Enabled     bool
}

type ClientResourceManifestLookup struct {
	Channel  string
	Platform string
	Arch     string
}

func normalizedPage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}

func (r *ClientResourceRepository) CreateResource(resource *ClientResource) error {
	return r.db.Create(resource).Error
}

func (r *ClientResourceRepository) GetResourceByID(id int) (*ClientResource, error) {
	var resource ClientResource
	if err := r.db.Preload("Releases", "status = ?", ClientResourceReleaseStatusPublished).First(&resource, id).Error; err != nil {
		return nil, err
	}
	return &resource, nil
}

func (r *ClientResourceRepository) ListResources(filter ClientResourceListFilter) ([]*ClientResource, int64, error) {
	page, pageSize := normalizedPage(filter.Page, filter.PageSize)
	query := r.db.Model(&ClientResource{})
	if value := strings.TrimSpace(filter.ResourceKey); value != "" {
		query = query.Where("resource_key = ?", value)
	}
	if value := strings.TrimSpace(filter.Name); value != "" {
		query = query.Where("name LIKE ?", "%"+value+"%")
	}
	if value := strings.TrimSpace(filter.Category); value != "" {
		query = query.Where("category = ?", value)
	}
	if filter.Enabled != nil {
		query = query.Where("enabled = ?", *filter.Enabled)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var resources []*ClientResource
	if err := query.Preload("Releases", "status = ?", ClientResourceReleaseStatusPublished).
		Order("create_time DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&resources).Error; err != nil {
		return nil, 0, err
	}
	return resources, total, nil
}

func (r *ClientResourceRepository) UpdateResource(id int, update ClientResourceUpdate) (*ClientResource, error) {
	var resource ClientResource
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clauseForUpdate()).First(&resource, id).Error; err != nil {
			return err
		}
		if resource.ResourceKey != update.ResourceKey {
			var published int64
			if err := tx.Model(&ClientResourceRelease{}).
				Where("resource_id = ? AND status IN ?", id, []string{ClientResourceReleaseStatusPublished, ClientResourceReleaseStatusWithdrawn}).
				Count(&published).Error; err != nil {
				return err
			}
			if published > 0 {
				return ErrClientResourceKeyImmutable
			}
		}
		updates := map[string]any{
			"resource_key": update.ResourceKey,
			"name":         update.Name,
			"category":     update.Category,
			"description":  update.Description,
			"required":     update.Required,
			"enabled":      update.Enabled,
		}
		if err := tx.Model(&ClientResource{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Preload("Releases", "status = ?", ClientResourceReleaseStatusPublished).First(&resource, id).Error
	})
	if err != nil {
		return nil, err
	}
	return &resource, nil
}

// DeleteResource removes the resource and every release, artifact, and target
// in one transaction. The returned graph lets the caller clean up object files
// after the database commit.
func (r *ClientResourceRepository) DeleteResource(id int) (*ClientResource, error) {
	var resource ClientResource
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clauseForUpdate()).Preload("Releases.Artifacts.Targets").First(&resource, id).Error; err != nil {
			return err
		}

		releaseIDs := make([]int, 0, len(resource.Releases))
		artifactIDs := make([]int, 0)
		for _, release := range resource.Releases {
			releaseIDs = append(releaseIDs, release.ID)
			for _, artifact := range release.Artifacts {
				artifactIDs = append(artifactIDs, artifact.ID)
			}
		}
		if len(artifactIDs) > 0 {
			if err := tx.Where("artifact_id IN ?", artifactIDs).Delete(&ClientResourceArtifactTarget{}).Error; err != nil {
				return err
			}
		}
		if len(releaseIDs) > 0 {
			if err := tx.Where("release_id IN ?", releaseIDs).Delete(&ClientResourceArtifact{}).Error; err != nil {
				return err
			}
			if err := tx.Where("resource_id = ?", id).Delete(&ClientResourceRelease{}).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&ClientResource{}, id).Error
	})
	if err != nil {
		return nil, err
	}
	return &resource, nil
}

func (r *ClientResourceRepository) CreateRelease(release *ClientResourceRelease) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var resource ClientResource
		if err := tx.First(&resource, release.ResourceID).Error; err != nil {
			return err
		}
		if !resource.Enabled {
			return ErrClientResourceDisabled
		}
		if err := tx.Create(release).Error; err != nil {
			return err
		}
		release.Resource = &resource
		return nil
	})
}

func preloadClientResourceRelease(db *gorm.DB) *gorm.DB {
	return db.Preload("Resource").Preload("Artifacts.Targets")
}

func (r *ClientResourceRepository) GetReleaseByID(id int) (*ClientResourceRelease, error) {
	var release ClientResourceRelease
	if err := preloadClientResourceRelease(r.db).First(&release, id).Error; err != nil {
		return nil, err
	}
	return &release, nil
}

func (r *ClientResourceRepository) ListReleases(filter ClientResourceReleaseListFilter) ([]*ClientResourceRelease, int64, error) {
	page, pageSize := normalizedPage(filter.Page, filter.PageSize)
	query := r.db.Model(&ClientResourceRelease{}).Where("resource_id = ?", filter.ResourceID)
	if value := strings.TrimSpace(filter.Version); value != "" {
		query = query.Where("client_resource_releases.version = ?", value)
	}
	if value := strings.TrimSpace(filter.Channel); value != "" {
		query = query.Where("client_resource_releases.channel = ?", value)
	}
	if value := strings.TrimSpace(filter.Status); value != "" {
		query = query.Where("client_resource_releases.status = ?", value)
	}
	if strings.TrimSpace(filter.Platform) != "" || strings.TrimSpace(filter.Arch) != "" {
		query = query.Joins("JOIN client_resource_artifacts AS cra ON cra.release_id = client_resource_releases.id").
			Joins("JOIN client_resource_artifact_targets AS crat ON crat.artifact_id = cra.id")
		if value := strings.TrimSpace(filter.Platform); value != "" {
			query = query.Where("crat.platform = ?", value)
		}
		if value := strings.TrimSpace(filter.Arch); value != "" {
			query = query.Where("crat.arch = ?", value)
		}
	}
	var total int64
	if err := query.Distinct("client_resource_releases.id").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var releases []*ClientResourceRelease
	if err := query.Distinct("client_resource_releases.*").Preload("Resource").Preload("Artifacts.Targets").
		Order("client_resource_releases.create_time DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&releases).Error; err != nil {
		return nil, 0, err
	}
	return releases, total, nil
}

// CompleteArtifact serializes object promotion and artifact/target insertion
// under the release lock. beforeCreate may perform storage I/O after all
// target conflicts have been checked.
func (r *ClientResourceRepository) CompleteArtifact(
	artifact *ClientResourceArtifact,
	beforeCreate func(release *ClientResourceRelease) error,
	onCreateFailure func(),
) error {
	if artifact == nil {
		return fmt.Errorf("client resource artifact is nil")
	}
	if strings.TrimSpace(artifact.Metadata) == "" {
		artifact.Metadata = "{}"
	}
	finalized := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var release ClientResourceRelease
		if err := tx.Clauses(clauseForUpdate()).Preload("Resource").First(&release, artifact.ReleaseID).Error; err != nil {
			return err
		}
		if release.Status != ClientResourceReleaseStatusDraft && release.Status != ClientResourceReleaseStatusPublished {
			return ErrClientResourceReleaseNotEditable
		}
		if release.Resource == nil || !release.Resource.Enabled {
			return ErrClientResourceDisabled
		}
		if len(artifact.Targets) == 0 {
			return ErrClientResourceTargetRequired
		}
		if err := ensureNoClientResourceTargetConflict(tx, artifact); err != nil {
			return err
		}
		if beforeCreate != nil {
			if err := beforeCreate(&release); err != nil {
				return err
			}
			finalized = true
		}
		if err := tx.Omit("Release").Create(artifact).Error; err != nil {
			return err
		}
		return tx.Model(&ClientResourceRelease{}).Where("id = ?", release.ID).Update("update_time", time.Now()).Error
	})
	if err != nil && finalized && onCreateFailure != nil {
		onCreateFailure()
	}
	return err
}

func ensureNoClientResourceTargetConflict(tx *gorm.DB, artifact *ClientResourceArtifact) error {
	parts := make([]string, 0, len(artifact.Targets))
	args := make([]any, 0, len(artifact.Targets)*2)
	for _, target := range artifact.Targets {
		parts = append(parts, "(crat.platform = ? AND crat.arch = ?)")
		args = append(args, target.Platform, target.Arch)
	}
	var count int64
	query := tx.Table("client_resource_artifact_targets AS crat").
		Joins("JOIN client_resource_artifacts AS cra ON cra.id = crat.artifact_id").
		Where("cra.release_id = ? AND cra.format = ? AND cra.runtime = ? AND cra.variant = ?", artifact.ReleaseID, artifact.Format, artifact.Runtime, artifact.Variant).
		Where(strings.Join(parts, " OR "), args...)
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrClientResourceTargetConflict
	}
	return nil
}

func (r *ClientResourceRepository) PublishRelease(id int) (*ClientResourceRelease, error) {
	var release ClientResourceRelease
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clauseForUpdate()).Preload("Resource").First(&release, id).Error; err != nil {
			return err
		}
		if release.Status != ClientResourceReleaseStatusDraft {
			return ErrClientResourceReleaseNotDraft
		}
		if release.Resource == nil || !release.Resource.Enabled {
			return ErrClientResourceDisabled
		}
		var artifactCount int64
		if err := tx.Model(&ClientResourceArtifact{}).Where("release_id = ? AND (storage_key <> '' OR external_url <> '')", id).Count(&artifactCount).Error; err != nil {
			return err
		}
		if artifactCount == 0 {
			return ErrClientResourceHasNoArtifact
		}
		var targetedCount int64
		if err := tx.Table("client_resource_artifacts AS cra").
			Joins("JOIN client_resource_artifact_targets AS crat ON crat.artifact_id = cra.id").
			Where("cra.release_id = ?", id).Distinct("cra.id").Count(&targetedCount).Error; err != nil {
			return err
		}
		if targetedCount != artifactCount {
			return ErrClientResourceTargetRequired
		}
		now := time.Now()
		if err := tx.Model(&ClientResourceRelease{}).Where("id = ?", id).Updates(map[string]any{
			"status": ClientResourceReleaseStatusPublished, "published_at": now,
		}).Error; err != nil {
			return err
		}
		release.Status = ClientResourceReleaseStatusPublished
		release.PublishedAt = &now
		return preloadClientResourceRelease(tx).First(&release, id).Error
	})
	if err != nil {
		return nil, err
	}
	return &release, nil
}

// DeleteRelease removes a release in any lifecycle state and returns its
// artifacts so the caller can delete their objects after the transaction.
func (r *ClientResourceRepository) DeleteRelease(id int) (*ClientResourceRelease, error) {
	var release ClientResourceRelease
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clauseForUpdate()).Preload("Artifacts.Targets").First(&release, id).Error; err != nil {
			return err
		}
		artifactIDs := make([]int, 0, len(release.Artifacts))
		for _, artifact := range release.Artifacts {
			artifactIDs = append(artifactIDs, artifact.ID)
		}
		if len(artifactIDs) > 0 {
			if err := tx.Where("artifact_id IN ?", artifactIDs).Delete(&ClientResourceArtifactTarget{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("release_id = ?", id).Delete(&ClientResourceArtifact{}).Error; err != nil {
			return err
		}
		return tx.Delete(&ClientResourceRelease{}, id).Error
	})
	if err != nil {
		return nil, err
	}
	return &release, nil
}

func (r *ClientResourceRepository) ListManifestArtifacts(query ClientResourceManifestLookup) ([]*ClientResourceArtifact, error) {
	channels := []string{"stable"}
	if query.Channel == "beta" {
		channels = []string{"beta", "stable"}
	}
	var artifacts []*ClientResourceArtifact
	err := r.db.Model(&ClientResourceArtifact{}).
		Joins("JOIN client_resource_artifact_targets AS crat ON crat.artifact_id = client_resource_artifacts.id").
		Joins("JOIN client_resource_releases AS crr ON crr.id = client_resource_artifacts.release_id").
		Joins("JOIN client_resources AS cr ON cr.id = crr.resource_id").
		Where("cr.enabled = ? AND crr.status = ? AND crr.channel IN ?", true, ClientResourceReleaseStatusPublished, channels).
		Where("crat.platform = ? AND crat.arch = ?", query.Platform, query.Arch).
		Distinct("client_resource_artifacts.*").
		Preload("Targets").Preload("Release.Resource").Find(&artifacts).Error
	return artifacts, err
}

func (r *ClientResourceRepository) GetDownloadableArtifact(id int) (*ClientResourceArtifact, error) {
	var artifact ClientResourceArtifact
	err := r.db.Preload("Targets").Preload("Release.Resource").First(&artifact, id).Error
	if err != nil {
		return nil, err
	}
	if artifact.Release == nil || artifact.Release.Resource == nil ||
		artifact.Release.Status != ClientResourceReleaseStatusPublished || !artifact.Release.Resource.Enabled {
		return nil, ErrClientResourceNotPublished
	}
	return &artifact, nil
}

func (r *ClientResourceRepository) String() string {
	return fmt.Sprintf("ClientResourceRepository(%p)", r.db)
}

func clauseForUpdate() clause.Locking {
	return clause.Locking{Strength: "UPDATE"}
}
