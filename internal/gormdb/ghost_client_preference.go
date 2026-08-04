package gormdb

import (
	"errors"
	"sort"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GhostRoutingPreference struct {
	ID               uint
	UserID           int
	DevModel         uint8
	ClientInstanceID string
	TxGroupID        int
	RxGroupIDs       []int
}

type GhostClientPreferenceRepository struct {
	db *gorm.DB
}

func NewGhostClientPreferenceRepository() *GhostClientPreferenceRepository {
	return &GhostClientPreferenceRepository{db: Get()}
}

func normalizeRoutingGroupIDs(groupIDs []int) []int {
	set := make(map[int]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if groupID > 0 {
			set[groupID] = struct{}{}
		}
	}
	result := make([]int, 0, len(set))
	for groupID := range set {
		result = append(result, groupID)
	}
	sort.Ints(result)
	return result
}

func routingPreferenceFromModel(pref *GhostClientPreference) *GhostRoutingPreference {
	if pref == nil {
		return nil
	}
	result := &GhostRoutingPreference{
		ID: pref.ID, UserID: pref.UserID, DevModel: pref.DevModel,
		ClientInstanceID: pref.ClientInstanceID,
	}
	if pref.TxGroupID != nil {
		result.TxGroupID = *pref.TxGroupID
	}
	result.RxGroupIDs = make([]int, 0, len(pref.Subscriptions))
	for _, subscription := range pref.Subscriptions {
		result.RxGroupIDs = append(result.RxGroupIDs, subscription.GroupID)
	}
	result.RxGroupIDs = normalizeRoutingGroupIDs(result.RxGroupIDs)
	return result
}

func (r *GhostClientPreferenceRepository) Get(userID int, devModel uint8, clientInstanceID string) (*GhostRoutingPreference, error) {
	var pref GhostClientPreference
	err := r.db.Preload("Subscriptions").
		Where("user_id = ? AND dev_model = ? AND client_instance_id = ?", userID, devModel, strings.ToLower(clientInstanceID)).
		First(&pref).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return routingPreferenceFromModel(&pref), nil
}

// GetOrCreate initializes a new installation from the platform-level legacy
// preference. Concurrent first-auth requests converge on the unique instance
// key and then reload the committed routing state.
func (r *GhostClientPreferenceRepository) GetOrCreate(userID int, devModel uint8, clientInstanceID string, initialGroupID int) (*GhostRoutingPreference, error) {
	clientInstanceID = strings.ToLower(clientInstanceID)
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&GhostClientPreference{}).
			Where("user_id = ? AND dev_model = ? AND client_instance_id = ?", userID, devModel, clientInstanceID).
			Count(&count).Error; err != nil || count > 0 {
			return err
		}
		var txGroupID *int
		if initialGroupID > 0 {
			value := initialGroupID
			txGroupID = &value
		}
		pref := &GhostClientPreference{UserID: userID, DevModel: devModel, ClientInstanceID: clientInstanceID, TxGroupID: txGroupID}
		if err := tx.Create(pref).Error; err != nil {
			return err
		}
		if initialGroupID > 0 {
			return tx.Create(&GhostClientSubscription{PreferenceID: pref.ID, GroupID: initialGroupID}).Error
		}
		return nil
	})
	if err != nil && !IsDuplicateKeyError(err) {
		return nil, err
	}
	return r.Get(userID, devModel, clientInstanceID)
}

// ReplaceRouting atomically changes the persisted transmit group and receive
// subscription set for one installed client.
func (r *GhostClientPreferenceRepository) ReplaceRouting(userID int, devModel uint8, clientInstanceID string, txGroupID int, rxGroupIDs []int) error {
	clientInstanceID = strings.ToLower(clientInstanceID)
	rxGroupIDs = normalizeRoutingGroupIDs(rxGroupIDs)
	return r.db.Transaction(func(tx *gorm.DB) error {
		var pref GhostClientPreference
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND dev_model = ? AND client_instance_id = ?", userID, devModel, clientInstanceID).
			First(&pref).Error; err != nil {
			return err
		}
		var value *int
		if txGroupID > 0 {
			groupID := txGroupID
			value = &groupID
		}
		if err := tx.Model(&pref).Update("tx_group_id", value).Error; err != nil {
			return err
		}
		if err := tx.Where("preference_id = ?", pref.ID).Delete(&GhostClientSubscription{}).Error; err != nil {
			return err
		}
		if len(rxGroupIDs) == 0 {
			return nil
		}
		subscriptions := make([]GhostClientSubscription, len(rxGroupIDs))
		for i, groupID := range rxGroupIDs {
			subscriptions[i] = GhostClientSubscription{PreferenceID: pref.ID, GroupID: groupID}
		}
		return tx.Create(&subscriptions).Error
	})
}

func clearGhostClientGroupReferences(tx *gorm.DB, userID *int, groupIDs []int) error {
	groupIDs = normalizeRoutingGroupIDs(groupIDs)
	if len(groupIDs) == 0 {
		return nil
	}
	prefQuery := tx.Model(&GhostClientPreference{}).Select("id")
	if userID != nil {
		prefQuery = prefQuery.Where("user_id = ?", *userID)
	}
	if err := tx.Where("preference_id IN (?) AND group_id IN ?", prefQuery, groupIDs).
		Delete(&GhostClientSubscription{}).Error; err != nil {
		return err
	}
	updates := tx.Model(&GhostClientPreference{}).Where("tx_group_id IN ?", groupIDs)
	if userID != nil {
		updates = updates.Where("user_id = ?", *userID)
	}
	return updates.Update("tx_group_id", nil).Error
}

func deleteGhostClientPreferencesByUser(tx *gorm.DB, userID int) error {
	prefIDs := tx.Model(&GhostClientPreference{}).Select("id").Where("user_id = ?", userID)
	if err := tx.Where("preference_id IN (?)", prefIDs).Delete(&GhostClientSubscription{}).Error; err != nil {
		return err
	}
	return tx.Where("user_id = ?", userID).Delete(&GhostClientPreference{}).Error
}
