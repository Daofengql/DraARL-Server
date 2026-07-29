package gormdb

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrClientReleaseNotDraft     = errors.New("client release is not a draft")
	ErrClientReleaseNotPublished = errors.New("client release is not published")
	ErrClientReleaseHasNoPackage = errors.New("client release has no artifacts")
)

// ClientReleaseRepository persists client releases and their per-target
// artifacts. It deliberately keeps publishing state transitions transactional.
type ClientReleaseRepository struct {
	db *gorm.DB
}

func NewClientReleaseRepository() *ClientReleaseRepository {
	return &ClientReleaseRepository{db: Get()}
}

func newClientReleaseRepository(db *gorm.DB) *ClientReleaseRepository {
	return &ClientReleaseRepository{db: db}
}

type ClientReleaseListFilter struct {
	AppID    string
	Version  string
	Channel  string
	Status   string
	Platform string
	Arch     string
	Page     int
	PageSize int
}

func (r *ClientReleaseRepository) Create(release *ClientRelease) error {
	return r.db.Create(release).Error
}

func (r *ClientReleaseRepository) GetByID(id int) (*ClientRelease, error) {
	var release ClientRelease
	if err := r.db.Preload("Artifacts").First(&release, id).Error; err != nil {
		return nil, err
	}
	return &release, nil
}

func (r *ClientReleaseRepository) List(filter ClientReleaseListFilter) ([]*ClientRelease, int64, error) {
	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	query := r.db.Model(&ClientRelease{})
	if value := strings.TrimSpace(filter.AppID); value != "" {
		query = query.Where("client_releases.app_id = ?", value)
	}
	if value := strings.TrimSpace(filter.Version); value != "" {
		query = query.Where("client_releases.version = ?", value)
	}
	if value := strings.TrimSpace(filter.Channel); value != "" {
		query = query.Where("client_releases.channel = ?", value)
	}
	if value := strings.TrimSpace(filter.Status); value != "" {
		query = query.Where("client_releases.status = ?", value)
	}
	if strings.TrimSpace(filter.Platform) != "" || strings.TrimSpace(filter.Arch) != "" {
		query = query.Joins("JOIN client_release_artifacts AS cra ON cra.release_id = client_releases.id")
		if value := strings.TrimSpace(filter.Platform); value != "" {
			query = query.Where("cra.platform = ?", value)
		}
		if value := strings.TrimSpace(filter.Arch); value != "" {
			query = query.Where("cra.arch = ?", value)
		}
	}

	var total int64
	if err := query.Distinct("client_releases.id").Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var releases []*ClientRelease
	if err := query.
		Distinct("client_releases.*").
		Preload("Artifacts").
		Order("client_releases.create_time DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&releases).Error; err != nil {
		return nil, 0, err
	}
	return releases, total, nil
}

func (r *ClientReleaseRepository) AddArtifact(artifact *ClientReleaseArtifact) error {
	return r.CompleteArtifact(artifact, nil, nil)
}

// CompleteArtifact serializes an artifact's object promotion and metadata
// insertion under the release row lock. The callback runs while the release is
// known to be a draft and its target matrix is stable, so two complete
// requests cannot promote competing payloads into the same immutable final key.
//
// The callback may perform storage I/O. It must only set artifact fields after
// its object has been committed; returning an error rolls back the DB work.
func (r *ClientReleaseRepository) CompleteArtifact(
	artifact *ClientReleaseArtifact,
	beforeCreate func(release *ClientRelease) error,
	onCreateFailure func(),
) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var release ClientRelease
		if err := tx.Clauses(clauseForUpdate()).First(&release, artifact.ReleaseID).Error; err != nil {
			return err
		}
		if release.Status != ClientReleaseStatusDraft {
			return ErrClientReleaseNotDraft
		}
		var existing ClientReleaseArtifact
		err := tx.Where("release_id = ? AND platform = ? AND arch = ? AND package_type = ?", artifact.ReleaseID, artifact.Platform, artifact.Arch, artifact.PackageType).First(&existing).Error
		if err == nil {
			return gorm.ErrDuplicatedKey
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if beforeCreate != nil {
			if err := beforeCreate(&release); err != nil {
				return err
			}
		}
		if err := tx.Create(artifact).Error; err != nil {
			// This failure occurs before Transaction attempts COMMIT, so cleanup is
			// unambiguous and still protected by the release row lock. A COMMIT
			// error is intentionally handled outside this callback without cleanup
			// because the server may have committed despite a lost response.
			if onCreateFailure != nil {
				onCreateFailure()
			}
			return err
		}
		return nil
	})
}

func (r *ClientReleaseRepository) Publish(id int) (*ClientRelease, error) {
	var published ClientRelease
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clauseForUpdate()).First(&published, id).Error; err != nil {
			return err
		}
		if published.Status != ClientReleaseStatusDraft {
			return ErrClientReleaseNotDraft
		}
		var count int64
		if err := tx.Model(&ClientReleaseArtifact{}).
			Where("release_id = ? AND (storage_key <> '' OR external_url <> '')", id).
			Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return ErrClientReleaseHasNoPackage
		}
		now := time.Now()
		if err := tx.Model(&ClientRelease{}).Where("id = ?", id).Updates(map[string]any{
			"status":       ClientReleaseStatusPublished,
			"published_at": now,
		}).Error; err != nil {
			return err
		}
		published.Status = ClientReleaseStatusPublished
		published.PublishedAt = &now
		return tx.Preload("Artifacts").First(&published, id).Error
	})
	if err != nil {
		return nil, err
	}
	return &published, nil
}

func (r *ClientReleaseRepository) Withdraw(id int) (*ClientRelease, error) {
	var release ClientRelease
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clauseForUpdate()).First(&release, id).Error; err != nil {
			return err
		}
		if release.Status != ClientReleaseStatusPublished {
			return ErrClientReleaseNotPublished
		}
		if err := tx.Model(&ClientRelease{}).Where("id = ?", id).Update("status", ClientReleaseStatusWithdrawn).Error; err != nil {
			return err
		}
		release.Status = ClientReleaseStatusWithdrawn
		return tx.Preload("Artifacts").First(&release, id).Error
	})
	if err != nil {
		return nil, err
	}
	return &release, nil
}

// DeleteDraft returns the stored artifacts so the caller can delete their
// unreferenced objects after the transaction commits.
func (r *ClientReleaseRepository) DeleteDraft(id int) (*ClientRelease, error) {
	var release ClientRelease
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clauseForUpdate()).Preload("Artifacts").First(&release, id).Error; err != nil {
			return err
		}
		if release.Status != ClientReleaseStatusDraft {
			return ErrClientReleaseNotDraft
		}
		if err := tx.Where("release_id = ?", id).Delete(&ClientReleaseArtifact{}).Error; err != nil {
			return err
		}
		return tx.Delete(&ClientRelease{}, id).Error
	})
	if err != nil {
		return nil, err
	}
	return &release, nil
}

type ClientArtifactLookup struct {
	AppID         string
	Channel       string
	Platform      string
	PackageType   string
	Architectures []string
}

// ListPublishedArtifacts returns only the target candidates; version and OS
// compatibility ordering is intentionally handled by the domain layer.
func (r *ClientReleaseRepository) ListPublishedArtifacts(query ClientArtifactLookup) ([]*ClientReleaseArtifact, error) {
	var artifacts []*ClientReleaseArtifact
	db := r.db.Model(&ClientReleaseArtifact{}).
		Joins("JOIN client_releases AS cr ON cr.id = client_release_artifacts.release_id").
		Where("cr.app_id = ? AND cr.channel = ? AND cr.status = ?", query.AppID, query.Channel, ClientReleaseStatusPublished).
		Where("client_release_artifacts.platform = ?", query.Platform)
	if query.PackageType != "" {
		db = db.Where("client_release_artifacts.package_type = ?", query.PackageType)
	}
	if len(query.Architectures) > 0 {
		db = db.Where("client_release_artifacts.arch IN ?", query.Architectures)
	}
	if err := db.Preload("Release").Find(&artifacts).Error; err != nil {
		return nil, err
	}
	return artifacts, nil
}

// clauseForUpdate keeps row state stable while artifact or publish operations
// run. MySQL and MariaDB both map this to SELECT ... FOR UPDATE.
func clauseForUpdate() clause.Locking {
	return clause.Locking{Strength: "UPDATE"}
}
