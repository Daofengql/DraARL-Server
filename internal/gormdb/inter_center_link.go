package gormdb

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

var (
	ErrInterCenterLinkNotFound = errors.New("inter-center link not found")
	ErrInterCenterLinkConflict = errors.New("inter-center link already exists")
)

type InterCenterLinkRepository struct{ db *gorm.DB }

func NewInterCenterLinkRepository() *InterCenterLinkRepository {
	return &InterCenterLinkRepository{db: Get()}
}

func (r *InterCenterLinkRepository) Create(link *InterCenterLink) error {
	if r == nil || r.db == nil || link == nil {
		return errors.New("invalid inter-center link repository request")
	}
	return r.db.Create(link).Error
}

func (r *InterCenterLinkRepository) List() ([]*InterCenterLink, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("inter-center link database is unavailable")
	}
	var links []*InterCenterLink
	err := r.db.Order("id ASC").Find(&links).Error
	return links, err
}

func (r *InterCenterLinkRepository) GetByID(id int) (*InterCenterLink, error) {
	if id <= 0 {
		return nil, ErrInterCenterLinkNotFound
	}
	var link InterCenterLink
	err := r.db.First(&link, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInterCenterLinkNotFound
	}
	return &link, err
}

func (r *InterCenterLinkRepository) GetByLinkID(linkID string) (*InterCenterLink, error) {
	linkID = strings.TrimSpace(linkID)
	if linkID == "" {
		return nil, ErrInterCenterLinkNotFound
	}
	var link InterCenterLink
	err := r.db.Where("link_id = ?", linkID).First(&link).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInterCenterLinkNotFound
	}
	return &link, err
}

func (r *InterCenterLinkRepository) Update(id int, fields map[string]interface{}) error {
	if id <= 0 || len(fields) == 0 {
		return ErrInterCenterLinkNotFound
	}
	result := r.db.Model(&InterCenterLink{}).Where("id = ?", id).Updates(fields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		_, err := r.GetByID(id)
		return err
	}
	return nil
}

// Authentication is bound to exactly one accepted mapping, never an EdgeNode.
func (r *InterCenterLinkRepository) Authenticate(transportID, hash, localCenterID string) (string, uint32, error) {
	links, err := r.List()
	if err != nil {
		return "", 0, err
	}
	for _, l := range links {
		digest := sha256.Sum256([]byte(l.LinkID))
		if transportID != "cp-"+hex.EncodeToString(digest[:])[:29] {
			continue
		}
		if l.LocalCenterID != localCenterID || l.InitiatorCenterID != l.RemoteCenterID || !l.Enabled || !l.Accepted || !l.ForwardAudio || len(l.CredentialHash) != 64 || subtle.ConstantTimeCompare([]byte(hash), []byte(l.CredentialHash)) != 1 {
			return "", 0, nil
		}
		return l.LinkID, l.CredentialEpoch, nil
	}
	return "", 0, nil
}

func (r *InterCenterLinkRepository) Delete(id int) error {
	if id <= 0 {
		return ErrInterCenterLinkNotFound
	}
	result := r.db.Delete(&InterCenterLink{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrInterCenterLinkNotFound
	}
	return nil
}

func (r *InterCenterLinkRepository) MarkState(linkID string, connectedAt *time.Time, lastError string) error {
	fields := map[string]interface{}{"last_error": strings.TrimSpace(lastError)}
	if connectedAt != nil {
		fields["last_connected_at"] = connectedAt
	}
	return r.db.Model(&InterCenterLink{}).Where("link_id = ?", strings.TrimSpace(linkID)).Updates(fields).Error
}
