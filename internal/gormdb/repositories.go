package gormdb

import (
	"errors"
	"gorm.io/gorm"
)

// GroupRepository 群组仓库
type GroupRepository struct {
	db *gorm.DB
}

// NewGroupRepository 创建群组仓库
func NewGroupRepository() *GroupRepository {
	return &GroupRepository{db: Get()}
}

// ListGroups 获取群组列表
func (r *GroupRepository) ListGroups() ([]*Group, error) {
	var groups []*Group
	err := r.db.Order("id DESC").Find(&groups).Error
	return groups, err
}

// ListGroupsPaginated 分页获取群组列表
func (r *GroupRepository) ListGroupsPaginated(limit, page int) ([]*Group, int64, error) {
	var groups []*Group
	var total int64

	offset := (page - 1) * limit

	// 获取总数
	if err := r.db.Model(&Group{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	if err := r.db.Order("id DESC").Limit(limit).Offset(offset).Find(&groups).Error; err != nil {
		return nil, 0, err
	}

	return groups, total, nil
}

// GetGroupByID 通过ID获取群组
func (r *GroupRepository) GetGroupByID(id int) (*Group, error) {
	var group Group
	err := r.db.First(&group, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &group, nil
}

// CreateGroup 创建群组
func (r *GroupRepository) CreateGroup(group *Group) error {
	return r.db.Create(group).Error
}

// UpdateGroup 更新群组
func (r *GroupRepository) UpdateGroup(group *Group) error {
	return r.db.Save(group).Error
}

// UpdateGroupFields 更新群组指定字段
func (r *GroupRepository) UpdateGroupFields(id int, fields map[string]interface{}) error {
	return r.db.Model(&Group{}).Where("id = ?", id).Updates(fields).Error
}

// DeleteGroup 删除群组
func (r *GroupRepository) DeleteGroup(id int) error {
	return r.db.Delete(&Group{}, id).Error
}

// GroupCount 获取群组总数
func (r *GroupRepository) GroupCount() (int64, error) {
	var count int64
	err := r.db.Model(&Group{}).Count(&count).Error
	return count, err
}

// SearchGroups 搜索群组
func (r *GroupRepository) SearchGroups(keyword string) ([]*Group, error) {
	var groups []*Group
	like := "%" + keyword + "%"
	err := r.db.Where("CAST(id AS CHAR) LIKE ? OR name LIKE ?", like, like).Find(&groups).Error
	return groups, err
}

// ListPublicGroups 获取公开群组列表（Type=1）
func (r *GroupRepository) ListPublicGroups() ([]*Group, error) {
	var groups []*Group
	err := r.db.Where("type = ?", 1).Order("id DESC").Find(&groups).Error
	return groups, err
}

// ListPublicGroupsExcludeVirtual 获取公开群组列表（排除虚拟互联组）
func (r *GroupRepository) ListPublicGroupsExcludeVirtual() ([]*Group, error) {
	var groups []*Group
	err := r.db.Where("type = ? AND (is_virtual = ? OR is_virtual IS NULL)", 1, false).Order("id DESC").Find(&groups).Error
	return groups, err
}

// ListVirtualGroups 获取所有虚拟互联组
func (r *GroupRepository) ListVirtualGroups() ([]*Group, error) {
	var groups []*Group
	err := r.db.Where("is_virtual = ?", true).Order("id DESC").Find(&groups).Error
	return groups, err
}

// ListGroupsExcludeVirtual 获取所有群组（排除虚拟互联组）
func (r *GroupRepository) ListGroupsExcludeVirtual() ([]*Group, error) {
	var groups []*Group
	err := r.db.Where("is_virtual = ? OR is_virtual IS NULL", false).Order("id DESC").Find(&groups).Error
	return groups, err
}

// ListPublicGroupsPaginated 分页获取公开群组列表
func (r *GroupRepository) ListPublicGroupsPaginated(limit, page int, keyword string) ([]*Group, int64, error) {
	var groups []*Group
	var total int64

	offset := (page - 1) * limit
	query := r.db.Model(&Group{}).Where("type = ?", 1)

	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("CAST(id AS CHAR) LIKE ? OR name LIKE ?", like, like)
	}

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	if err := query.Order("id DESC").Limit(limit).Offset(offset).Find(&groups).Error; err != nil {
		return nil, 0, err
	}

	return groups, total, nil
}

// ListGroupsByType 按类型获取群组列表
func (r *GroupRepository) ListGroupsByType(groupType int) ([]*Group, error) {
	var groups []*Group
	err := r.db.Where("type = ?", groupType).Order("id DESC").Find(&groups).Error
	return groups, err
}

// GetGroupsByIDs 批量获取群组
func (r *GroupRepository) GetGroupsByIDs(ids []int) ([]*Group, error) {
	var groups []*Group
	err := r.db.Where("id IN ?", ids).Find(&groups).Error
	return groups, err
}

// GetUserVisibleGroups 获取用户可见的所有群组（一次查询解决）
// 包括：公开群组（type=1）+ 用户已验证的私有群组
func (r *GroupRepository) GetUserVisibleGroups(userID int) ([]*Group, error) {
	var groups []*Group

	// 使用 GORM 子查询：获取用户已验证的群组ID
	subQuery := r.db.Table("group_members").
		Select("group_id").
		Where("user_id = ? AND is_verified = ?", userID, true)

	// 主查询：公开群组（非虚拟）或者用户已验证的群组
	err := r.db.Where("type = ? AND (is_virtual = ? OR is_virtual IS NULL)", 1, false).
		Or("id IN (?)", subQuery).
		Order("id DESC").
		Find(&groups).Error

	if err != nil {
		return nil, err
	}
	return groups, nil
}

// AddPublicGroup 添加公共群组（兼容旧接口）
func (r *GroupRepository) AddPublicGroup(group *Group) error {
	return r.db.Create(group).Error
}

// RelayRepository 中继台仓库
type RelayRepository struct {
	db *gorm.DB
}

// NewRelayRepository 创建中继台仓库
func NewRelayRepository() *RelayRepository {
	return &RelayRepository{db: Get()}
}

// ListRelays 获取中继台列表
func (r *RelayRepository) ListRelays() ([]*Relay, error) {
	var relays []*Relay
	err := r.db.Order("id DESC").Find(&relays).Error
	return relays, err
}

// ListRelaysPaginated 分页获取中继台列表
func (r *RelayRepository) ListRelaysPaginated(limit, page int) ([]*Relay, int64, error) {
	var relays []*Relay
	var total int64

	offset := (page - 1) * limit

	// 获取总数
	if err := r.db.Model(&Relay{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	if err := r.db.Order("id DESC").Limit(limit).Offset(offset).Find(&relays).Error; err != nil {
		return nil, 0, err
	}

	return relays, total, nil
}

// GetRelayByID 通过ID获取中继台
func (r *RelayRepository) GetRelayByID(id int) (*Relay, error) {
	var relay Relay
	err := r.db.First(&relay, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &relay, nil
}

// CreateRelay 创建中继台
func (r *RelayRepository) CreateRelay(relay *Relay) error {
	return r.db.Create(relay).Error
}

// UpdateRelay 更新中继台
func (r *RelayRepository) UpdateRelay(relay *Relay) error {
	return r.db.Save(relay).Error
}

// DeleteRelay 删除中继台
func (r *RelayRepository) DeleteRelay(id int) error {
	return r.db.Delete(&Relay{}, id).Error
}

// SearchRelaysByLocation 按位置搜索中继台（公开接口）
// location 可以是省份、城市或区县名称
func (r *RelayRepository) SearchRelaysByLocation(location string) ([]*Relay, error) {
	var relays []*Relay
	query := r.db.Where("status = ?", 1) // 只返回启用的中继台
	if location != "" {
		query = query.Where("location LIKE ?", "%"+location+"%")
	}
	err := query.Order("id DESC").Find(&relays).Error
	return relays, err
}

// ServerRepository 服务器仓库
type ServerRepository struct {
	db *gorm.DB
}

// NewServerRepository 创建服务器仓库
func NewServerRepository() *ServerRepository {
	return &ServerRepository{db: Get()}
}

// ListServers 获取服务器列表
func (r *ServerRepository) ListServers() ([]*Server, error) {
	var servers []*Server
	err := r.db.Order("id DESC").Find(&servers).Error
	return servers, err
}

// ListServersPaginated 分页获取服务器列表
func (r *ServerRepository) ListServersPaginated(limit, page int) ([]*Server, int64, error) {
	var servers []*Server
	var total int64

	offset := (page - 1) * limit

	// 获取总数
	if err := r.db.Model(&Server{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	if err := r.db.Order("id DESC").Limit(limit).Offset(offset).Find(&servers).Error; err != nil {
		return nil, 0, err
	}

	return servers, total, nil
}

// GetServerByID 通过ID获取服务器
func (r *ServerRepository) GetServerByID(id int) (*Server, error) {
	var server Server
	err := r.db.First(&server, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &server, nil
}

// CreateServer 创建服务器
func (r *ServerRepository) CreateServer(server *Server) error {
	return r.db.Create(server).Error
}

// UpdateServer 更新服务器
func (r *ServerRepository) UpdateServer(server *Server) error {
	return r.db.Save(server).Error
}

// UpdateServerFields 更新服务器指定字段
func (r *ServerRepository) UpdateServerFields(id int, fields map[string]interface{}) error {
	return r.db.Model(&Server{}).Where("id = ?", id).Updates(fields).Error
}

// DeleteServer 删除服务器
func (r *ServerRepository) DeleteServer(id int) error {
	return r.db.Delete(&Server{}, id).Error
}

// ServerCount 获取服务器总数
func (r *ServerRepository) ServerCount() (int64, error) {
	var count int64
	err := r.db.Model(&Server{}).Count(&count).Error
	return count, err
}

// OperatorLogRepository 操作日志仓库
type OperatorLogRepository struct {
	db *gorm.DB
}

// NewOperatorLogRepository 创建操作日志仓库
func NewOperatorLogRepository() *OperatorLogRepository {
	return &OperatorLogRepository{db: Get()}
}

// ListLogs 获取操作日志列表
func (r *OperatorLogRepository) ListLogs(limit, page int) ([]*OperatorLog, int64, error) {
	var logs []*OperatorLog
	var total int64

	offset := (page - 1) * limit

	// 获取总数
	if err := r.db.Model(&OperatorLog{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	if err := r.db.Order("id DESC").Limit(limit).Offset(offset).Find(&logs).Error; err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// ListLogsByEventType 按事件类型获取操作日志
func (r *OperatorLogRepository) ListLogsByEventType(eventType string, limit, page int) ([]*OperatorLog, int64, error) {
	var logs []*OperatorLog
	var total int64

	offset := (page - 1) * limit
	query := r.db.Model(&OperatorLog{})
	if eventType != "" {
		query = query.Where("event_type = ?", eventType)
	}

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	if err := query.Order("id DESC").Limit(limit).Offset(offset).Find(&logs).Error; err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// ListLogsByOperator 按操作人获取操作日志
func (r *OperatorLogRepository) ListLogsByOperator(operatorID int, limit, page int) ([]*OperatorLog, int64, error) {
	var logs []*OperatorLog
	var total int64

	offset := (page - 1) * limit
	query := r.db.Model(&OperatorLog{}).Where("operator_id = ?", operatorID)

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	if err := query.Order("id DESC").Limit(limit).Offset(offset).Find(&logs).Error; err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// CreateLog 创建操作日志
func (r *OperatorLogRepository) CreateLog(log *OperatorLog) error {
	return r.db.Create(log).Error
}

// GetLogStats 获取操作日志统计信息
func (r *OperatorLogRepository) GetLogStats() (map[string]int64, error) {
	stats := make(map[string]int64)

	// 按事件类型统计
	var results []struct {
		EventType string
		Count     int64
	}

	err := r.db.Model(&OperatorLog{}).
		Select("event_type, count(*) as count").
		Group("event_type").
		Scan(&results).Error

	if err != nil {
		return nil, err
	}

	for _, r := range results {
		stats[r.EventType] = r.Count
	}

	// 总数
	var total int64
	r.db.Model(&OperatorLog{}).Count(&total)
	stats["total"] = total

	return stats, nil
}

// BatchCreate 批量创建操作日志
func (r *OperatorLogRepository) BatchCreate(logs []*OperatorLog) error {
	if len(logs) == 0 {
		return nil
	}
	return r.db.CreateInBatches(logs, 100).Error
}

// AddOperatorLog 添加操作日志
func (r *OperatorLogRepository) AddOperatorLog(content, eventType, operator string, operatorID int) error {
	log := &OperatorLog{
		Content:    content,
		EventType:  eventType,
		Operator:   operator,
		OperatorID: operatorID,
	}
	return r.db.Create(log).Error
}

// Query 查询操作日志（分页）
func (r *OperatorLogRepository) Query(userID int, page, limit int, eventType string) ([]*OperatorLog, int64, error) {
	var logs []*OperatorLog
	var total int64

	offset := (page - 1) * limit
	query := r.db.Model(&OperatorLog{})

	if userID > 0 {
		query = query.Where("operator_id = ?", userID)
	}
	if eventType != "" {
		query = query.Where("event_type = ?", eventType)
	}

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	if err := query.Order("id DESC").Limit(limit).Offset(offset).Find(&logs).Error; err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// GetStats 获取日志统计信息（兼容旧接口）
func (r *OperatorLogRepository) GetStats() (map[string]int64, error) {
	stats := make(map[string]int64)

	// 总数
	var total int64
	if err := r.db.Model(&OperatorLog{}).Count(&total).Error; err != nil {
		return nil, err
	}
	stats["total"] = total

	// 今日统计
	var today int64
	if err := r.db.Model(&OperatorLog{}).Where("DATE(timestamp) = CURDATE()").Count(&today).Error; err != nil {
		return nil, err
	}
	stats["today"] = today

	// 本周统计
	var week int64
	if err := r.db.Model(&OperatorLog{}).Where("YEARWEEK(timestamp, 1) = YEARWEEK(NOW(), 1)").Count(&week).Error; err != nil {
		return nil, err
	}
	stats["this_week"] = week

	// 本月统计
	var month int64
	if err := r.db.Model(&OperatorLog{}).Where("YEAR(timestamp) = YEAR(NOW()) AND MONTH(timestamp) = MONTH(NOW())").Count(&month).Error; err != nil {
		return nil, err
	}
	stats["this_month"] = month

	return stats, nil
}
