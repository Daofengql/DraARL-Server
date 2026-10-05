package gormdb

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"gorm.io/gorm"
)

var ErrCenterPeerInviteNotFound = errors.New("center peer invite not found")

type CenterPeerInviteRepository struct{ db *gorm.DB }

func NewCenterPeerInviteRepository() *CenterPeerInviteRepository {
	return &CenterPeerInviteRepository{db: Get()}
}

func HashCenterPeerInviteToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func (r *CenterPeerInviteRepository) Create(invite *CenterPeerInvite) error {
	if r == nil || r.db == nil || invite == nil {
		return errors.New("invalid center peer invite")
	}
	return r.db.Create(invite).Error
}

func (r *CenterPeerInviteRepository) GetByTokenHash(hash string) (*CenterPeerInvite, error) {
	var invite CenterPeerInvite
	err := r.db.Where("token_hash = ?", strings.TrimSpace(hash)).First(&invite).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCenterPeerInviteNotFound
	}
	return &invite, err
}

func (r *CenterPeerInviteRepository) GetByID(id int) (*CenterPeerInvite, error) {
	var invite CenterPeerInvite
	err := r.db.First(&invite, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCenterPeerInviteNotFound
	}
	return &invite, err
}

func (r *CenterPeerInviteRepository) Update(id int, fields map[string]interface{}) error {
	result := r.db.Model(&CenterPeerInvite{}).Where("id = ?", id).Updates(fields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrCenterPeerInviteNotFound
	}
	return nil
}

func (r *CenterPeerInviteRepository) List() ([]*CenterPeerInvite, error) {
	var items []*CenterPeerInvite
	err := r.db.Order("id DESC").Find(&items).Error
	return items, err
}
