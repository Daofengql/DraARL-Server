package groupaccess

import (
	"slices"

	"draarl/internal/ghostsession"
	"draarl/internal/gormdb"

	"gorm.io/gorm"
)

const (
	TypePublic  = 1
	TypePrivate = 2
)

func IsSupportedType(groupType int) bool {
	return groupType == TypePublic || groupType == TypePrivate
}

func canAccessGroup(user *gormdb.User, group *gormdb.Group, isVerifiedMember bool) bool {
	if user == nil || group == nil || group.Status != 1 || group.IsVirtual || !IsSupportedType(group.Type) {
		return false
	}
	if user.HasRole("admin") || group.Type == TypePublic || group.OwerID == user.ID {
		return true
	}
	return group.Type == TypePrivate && isVerifiedMember
}

// CanReceiveGroup is the account/group rule for subscriptions and history.
func CanReceiveGroup(user *gormdb.User, group *gormdb.Group, isVerifiedMember bool) bool {
	return canAccessGroup(user, group, isVerifiedMember)
}

// CanTransmitGroup is the account/group rule for selecting a transmit group.
// Half-duplex arbitration remains a separate runtime decision.
func CanTransmitGroup(user *gormdb.User, group *gormdb.Group, isVerifiedMember bool) bool {
	return canAccessGroup(user, group, isVerifiedMember)
}

// CanReceiveRoute is the database-free check used by realtime fan-out.
func CanReceiveRoute(disableRecv bool, rxGroupIDs []int, groupID int) bool {
	return !disableRecv && groupID > 0 && slices.Contains(rxGroupIDs, groupID)
}

// CanTransmitRoute is the database-free check used by realtime ingress.
func CanTransmitRoute(disableSend bool, txGroupID, groupID int) bool {
	return !disableSend && groupID > 0 && txGroupID == groupID
}

// SanitizeRouting removes inaccessible subscriptions, falls back from an
// inaccessible transmit channel, and restores the transmit-in-receive
// invariant. The caller is responsible for supplying an already trusted
// fallback channel.
func SanitizeRouting(db *gorm.DB, user *gormdb.User, routing ghostsession.Routing, fallbackGroupID, maxSubscriptions int) (ghostsession.Routing, bool, error) {
	originalTx := routing.TxGroupID
	originalRx := normalizePositiveGroupIDs(routing.RxGroupIDs)
	candidates := make([]int, 0, len(routing.RxGroupIDs)+2)
	candidates = append(candidates, routing.TxGroupID, fallbackGroupID)
	candidates = append(candidates, routing.RxGroupIDs...)
	receivable, err := ReceivableGroupIDs(db, user, candidates)
	if err != nil {
		return ghostsession.Routing{}, false, err
	}
	transmittable, err := TransmittableGroupIDs(db, user, []int{routing.TxGroupID, fallbackGroupID})
	if err != nil {
		return ghostsession.Routing{}, false, err
	}
	if fallbackGroupID > 0 {
		receivable[fallbackGroupID] = struct{}{}
		transmittable[fallbackGroupID] = struct{}{}
	}
	if _, ok := transmittable[routing.TxGroupID]; !ok {
		routing.TxGroupID = fallbackGroupID
	}
	filtered := make([]int, 0, len(routing.RxGroupIDs)+1)
	for _, groupID := range routing.RxGroupIDs {
		if _, ok := receivable[groupID]; ok {
			filtered = append(filtered, groupID)
		}
	}
	routing.RxGroupIDs = filtered
	routing, err = ghostsession.NormalizeRouting(routing, maxSubscriptions)
	if err != nil {
		return ghostsession.Routing{}, false, err
	}
	changed := originalTx != routing.TxGroupID || !slices.Equal(originalRx, routing.RxGroupIDs)
	return routing, changed, nil
}

func normalizePositiveGroupIDs(groupIDs []int) []int {
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
	slices.Sort(result)
	return result
}

func accessibleGroupIDs(db *gorm.DB, user *gormdb.User, groupIDs []int, authorize func(*gormdb.User, *gormdb.Group, bool) bool) (map[int]struct{}, error) {
	result := make(map[int]struct{})
	if db == nil || user == nil || len(groupIDs) == 0 || authorize == nil {
		return result, nil
	}
	unique := make(map[int]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if groupID > 0 {
			unique[groupID] = struct{}{}
		}
	}
	if len(unique) == 0 {
		return result, nil
	}
	ids := make([]int, 0, len(unique))
	for groupID := range unique {
		ids = append(ids, groupID)
	}
	var groups []*gormdb.Group
	if err := db.Where("id IN ?", ids).Find(&groups).Error; err != nil {
		return nil, err
	}
	verified := make(map[int]bool)
	if !user.HasRole("admin") {
		var members []gormdb.GroupMember
		if err := db.Where("user_id = ? AND group_id IN ? AND is_verified = ?", user.ID, ids, true).Find(&members).Error; err != nil {
			return nil, err
		}
		for _, member := range members {
			verified[member.GroupID] = true
		}
	}
	for _, group := range groups {
		if authorize(user, group, verified[group.ID]) {
			result[group.ID] = struct{}{}
		}
	}
	return result, nil
}

// ReceivableGroupIDs resolves receive authorization in a bounded batch.
func ReceivableGroupIDs(db *gorm.DB, user *gormdb.User, groupIDs []int) (map[int]struct{}, error) {
	return accessibleGroupIDs(db, user, groupIDs, CanReceiveGroup)
}

// TransmittableGroupIDs resolves transmit authorization independently from
// receive subscriptions.
func TransmittableGroupIDs(db *gorm.DB, user *gormdb.User, groupIDs []int) (map[int]struct{}, error) {
	return accessibleGroupIDs(db, user, groupIDs, CanTransmitGroup)
}
