package gormdb

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// GroupMemberRepository 群组成员仓库
type GroupMemberRepository struct {
	db *gorm.DB
}

// NewGroupMemberRepository 创建群组成员仓库
func NewGroupMemberRepository() *GroupMemberRepository {
	return &GroupMemberRepository{db: Get()}
}

// GetMemberByGroupAndUser 获取群组成员记录
func (r *GroupMemberRepository) GetMemberByGroupAndUser(groupID, userID int) (*GroupMember, error) {
	var member GroupMember
	err := r.db.Where("group_id = ? AND user_id = ?", groupID, userID).First(&member).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &member, nil
}

// GetVerifiedMemberByGroupAndUser 获取已验证的群组成员记录
func (r *GroupMemberRepository) GetVerifiedMemberByGroupAndUser(groupID, userID int) (*GroupMember, error) {
	var member GroupMember
	err := r.db.Where("group_id = ? AND user_id = ? AND is_verified = ?", groupID, userID, true).First(&member).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &member, nil
}

// ListMembersByGroup 获取群组成员列表
func (r *GroupMemberRepository) ListMembersByGroup(groupID int) ([]*GroupMember, error) {
	var members []*GroupMember
	err := r.db.Where("group_id = ?", groupID).Find(&members).Error
	return members, err
}

// ListVerifiedMembersByGroup 获取群组已验证成员列表
func (r *GroupMemberRepository) ListVerifiedMembersByGroup(groupID int) ([]*GroupMember, error) {
	var members []*GroupMember
	err := r.db.Where("group_id = ? AND is_verified = ?", groupID, true).Find(&members).Error
	return members, err
}

// ListGroupsByUser 获取用户已加入的群组列表
func (r *GroupMemberRepository) ListGroupsByUser(userID int) ([]*GroupMember, error) {
	var members []*GroupMember
	err := r.db.Where("user_id = ? AND is_verified = ?", userID, true).Find(&members).Error
	return members, err
}

// ListVerifiedGroupIDsByUser 只返回给定候选中的已验证成员关系，供分页
// 列表避免为计算当前页 is_joined 而加载用户的全部群组成员记录。
func (r *GroupMemberRepository) ListVerifiedGroupIDsByUser(userID int, groupIDs []int) ([]int, error) {
	if userID <= 0 || len(groupIDs) == 0 {
		return []int{}, nil
	}
	var ids []int
	err := r.db.Model(&GroupMember{}).
		Where("user_id = ? AND group_id IN ? AND is_verified = ?", userID, groupIDs, true).
		Distinct("group_id").
		Pluck("group_id", &ids).Error
	return ids, err
}

// CreateMember 创建群组成员记录
func (r *GroupMemberRepository) CreateMember(member *GroupMember) error {
	return r.db.Create(member).Error
}

// UpdateMember 更新群组成员记录
func (r *GroupMemberRepository) UpdateMember(member *GroupMember) error {
	return r.db.Save(member).Error
}

// UpdateMemberVerification 更新成员验证状态
func (r *GroupMemberRepository) UpdateMemberVerification(groupID, userID int, isVerified bool) error {
	updates := map[string]interface{}{
		"is_verified": isVerified,
		"last_verify": time.Now(),
	}
	return r.db.Model(&GroupMember{}).
		Where("group_id = ? AND user_id = ?", groupID, userID).
		Updates(updates).Error
}

// DeleteMember 删除群组成员记录
func (r *GroupMemberRepository) DeleteMember(groupID, userID int) error {
	return r.db.Where("group_id = ? AND user_id = ?", groupID, userID).Delete(&GroupMember{}).Error
}

// DeleteMembersByGroup 删除群组所有成员记录
func (r *GroupMemberRepository) DeleteMembersByGroup(groupID int) error {
	return r.db.Where("group_id = ?", groupID).Delete(&GroupMember{}).Error
}

// DeleteMembersByUser 删除用户所有群组成员记录
func (r *GroupMemberRepository) DeleteMembersByUser(userID int) error {
	return r.db.Where("user_id = ?", userID).Delete(&GroupMember{}).Error
}

// IsVerifiedMember 检查用户是否已验证加入群组
func (r *GroupMemberRepository) IsVerifiedMember(groupID, userID int) bool {
	member, _ := r.GetVerifiedMemberByGroupAndUser(groupID, userID)
	return member != nil
}

// CountMembersByGroup 统计群组成员数
func (r *GroupMemberRepository) CountMembersByGroup(groupID int) (int64, error) {
	var count int64
	err := r.db.Model(&GroupMember{}).Where("group_id = ? AND is_verified = ?", groupID, true).Count(&count).Error
	return count, err
}
