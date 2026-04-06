package gormdb

import (
	"errors"

	"gorm.io/gorm"
)

var (
	// ErrTargetGroupAlreadyLinked 目标群组已被其他虚拟互联组占用
	ErrTargetGroupAlreadyLinked = errors.New("target group already linked by another virtual group")
)

// GroupLinkRepository 群组互联仓库
type GroupLinkRepository struct {
	db *gorm.DB
}

// NewGroupLinkRepository 创建群组互联仓库
func NewGroupLinkRepository() *GroupLinkRepository {
	return &GroupLinkRepository{db: Get()}
}

// AddLink 添加互联关系
func (r *GroupLinkRepository) AddLink(linkGroupID, targetGroupID int) error {
	// 业务约束：一个实体群组只能归属于一个虚拟互联组，避免互联拓扑重叠导致不可控扩散。
	var count int64
	if err := r.db.Model(&GroupLink{}).
		Where("target_group_id = ? AND link_group_id <> ?", targetGroupID, linkGroupID).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrTargetGroupAlreadyLinked
	}

	link := &GroupLink{
		LinkGroupID:   linkGroupID,
		TargetGroupID: targetGroupID,
	}
	return r.db.Create(link).Error
}

// RemoveLink 移除互联关系
func (r *GroupLinkRepository) RemoveLink(linkGroupID, targetGroupID int) error {
	return r.db.Where("link_group_id = ? AND target_group_id = ?", linkGroupID, targetGroupID).
		Delete(&GroupLink{}).Error
}

// RemoveAllLinks 移除互联组的所有关联关系
func (r *GroupLinkRepository) RemoveAllLinks(linkGroupID int) error {
	return r.db.Where("link_group_id = ?", linkGroupID).Delete(&GroupLink{}).Error
}

// GetLinksByLinkGroup 获取互联组关联的所有目标群组
func (r *GroupLinkRepository) GetLinksByLinkGroup(linkGroupID int) ([]*GroupLink, error) {
	var links []*GroupLink
	err := r.db.Where("link_group_id = ?", linkGroupID).Find(&links).Error
	return links, err
}

// GetLinksByTargetGroup 获取目标群组所属的所有互联组
func (r *GroupLinkRepository) GetLinksByTargetGroup(targetGroupID int) ([]*GroupLink, error) {
	var links []*GroupLink
	err := r.db.Where("target_group_id = ?", targetGroupID).Find(&links).Error
	return links, err
}

// GetAllLinks 获取所有互联关系（用于内存缓存）
func (r *GroupLinkRepository) GetAllLinks() ([]*GroupLink, error) {
	var links []*GroupLink
	err := r.db.Find(&links).Error
	return links, err
}

// GetTargetGroupIDs 获取互联组关联的所有目标群组ID
func (r *GroupLinkRepository) GetTargetGroupIDs(linkGroupID int) ([]int, error) {
	var groupIDs []int
	err := r.db.Model(&GroupLink{}).
		Where("link_group_id = ?", linkGroupID).
		Pluck("target_group_id", &groupIDs).Error
	return groupIDs, err
}

// GetLinkGroupIDsByTarget 获取目标群组所属的所有互联组ID
func (r *GroupLinkRepository) GetLinkGroupIDsByTarget(targetGroupID int) ([]int, error) {
	var groupIDs []int
	err := r.db.Model(&GroupLink{}).
		Where("target_group_id = ?", targetGroupID).
		Pluck("link_group_id", &groupIDs).Error
	return groupIDs, err
}

// LinkExists 检查互联关系是否存在
func (r *GroupLinkRepository) LinkExists(linkGroupID, targetGroupID int) (bool, error) {
	var count int64
	err := r.db.Model(&GroupLink{}).
		Where("link_group_id = ? AND target_group_id = ?", linkGroupID, targetGroupID).
		Count(&count).Error
	return count > 0, err
}

// GetLinkCount 获取互联组关联的目标群组数量
func (r *GroupLinkRepository) GetLinkCount(linkGroupID int) (int64, error) {
	var count int64
	err := r.db.Model(&GroupLink{}).
		Where("link_group_id = ?", linkGroupID).
		Count(&count).Error
	return count, err
}

// GetLinkWithGroupInfo 获取互联关系及目标群组信息
func (r *GroupLinkRepository) GetLinkWithGroupInfo(linkGroupID int) ([]map[string]interface{}, error) {
	var results []map[string]interface{}
	err := r.db.Table("group_links").
		Select("group_links.id, group_links.link_group_id, group_links.target_group_id, group_links.created_at, public_groups.name as target_group_name, public_groups.status as target_group_status").
		Joins("LEFT JOIN public_groups ON group_links.target_group_id = public_groups.id").
		Where("group_links.link_group_id = ?", linkGroupID).
		Find(&results).Error
	return results, err
}

// IsTargetGroupLinked 检查目标群组是否已被任何互联组关联
func (r *GroupLinkRepository) IsTargetGroupLinked(targetGroupID int) (bool, error) {
	var count int64
	err := r.db.Model(&GroupLink{}).
		Where("target_group_id = ?", targetGroupID).
		Count(&count).Error
	return count > 0, err
}

// GetLinkedTargetGroupIDs 获取所有已被互联的目标群组ID（去重）
func (r *GroupLinkRepository) GetLinkedTargetGroupIDs() ([]int, error) {
	var groupIDs []int
	err := r.db.Model(&GroupLink{}).
		Distinct("target_group_id").
		Pluck("target_group_id", &groupIDs).Error
	return groupIDs, err
}

// DeleteLinksByTargetGroup 删除与目标群组相关的所有互联关系（群组删除时调用）
func (r *GroupLinkRepository) DeleteLinksByTargetGroup(targetGroupID int) error {
	return r.db.Where("target_group_id = ?", targetGroupID).Delete(&GroupLink{}).Error
}

// DeleteLinksByLinkGroup 删除互联组的所有关联关系（互联组删除时调用）
func (r *GroupLinkRepository) DeleteLinksByLinkGroup(linkGroupID int) error {
	return r.db.Where("link_group_id = ?", linkGroupID).Delete(&GroupLink{}).Error
}

// BatchAddLinks 批量添加互联关系
func (r *GroupLinkRepository) BatchAddLinks(linkGroupID int, targetGroupIDs []int) error {
	if len(targetGroupIDs) == 0 {
		return nil
	}
	links := make([]*GroupLink, len(targetGroupIDs))
	for i, targetID := range targetGroupIDs {
		links[i] = &GroupLink{
			LinkGroupID:   linkGroupID,
			TargetGroupID: targetID,
		}
	}
	return r.db.CreateInBatches(links, 100).Error
}

// GetLinkGroupsForTarget 获取目标群组所属的所有互联组详情
func (r *GroupLinkRepository) GetLinkGroupsForTarget(targetGroupID int) ([]*Group, error) {
	var groups []*Group
	err := r.db.Table("public_groups").
		Select("public_groups.*").
		Joins("INNER JOIN group_links ON public_groups.id = group_links.link_group_id").
		Where("group_links.target_group_id = ? AND public_groups.is_virtual = ? AND public_groups.status = ?", targetGroupID, true, 1).
		Find(&groups).Error
	return groups, err
}

// GetTargetGroupsForLink 获取互联组关联的所有目标群组详情
func (r *GroupLinkRepository) GetTargetGroupsForLink(linkGroupID int) ([]*Group, error) {
	var groups []*Group
	err := r.db.Table("public_groups").
		Select("public_groups.*").
		Joins("INNER JOIN group_links ON public_groups.id = group_links.target_group_id").
		Where("group_links.link_group_id = ? AND public_groups.is_virtual = ? AND public_groups.status = ?", linkGroupID, false, 1).
		Find(&groups).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return []*Group{}, nil
	}
	return groups, err
}
