package cache

import (
	"context"
	"fmt"
	"time"

	gormdb "nrllink/internal/gormdb"
)

// GroupCache 群组信息缓存管理器
type GroupCache struct {
	cache *ThreeLevelCache
}

// GroupCacheConfig 群组缓存配置
type GroupCacheConfig struct {
	// L1 本地缓存配置
	LocalTTL time.Duration // 默认 1 分钟
	MaxSize  int           // 默认 10000

	// L2 Redis 缓存配置
	RedisTTL time.Duration // 默认 5 分钟
}

// NewGroupCache 创建群组缓存管理器
func NewGroupCache(config GroupCacheConfig) (*GroupCache, error) {
	// 设置默认值
	if config.LocalTTL == 0 {
		config.LocalTTL = time.Minute
	}
	if config.RedisTTL == 0 {
		config.RedisTTL = 5 * time.Minute
	}
	if config.MaxSize == 0 {
		config.MaxSize = 10000
	}

	cache, err := NewThreeLevelCache(CacheConfig{
		LocalTTL: config.LocalTTL,
		MaxSize:  config.MaxSize,
		RedisTTL: config.RedisTTL,
	})
	if err != nil {
		return nil, err
	}

	return &GroupCache{cache: cache}, nil
}

// 缓存键生成函数

// groupKey 群组详情缓存键
func groupKey(groupID int) string {
	return fmt.Sprintf("group:info:%d", groupID)
}

// groupListKey 群组列表缓存键（分页）
func groupListKey(page, pageSize int) string {
	return fmt.Sprintf("group:list:page:%d:size:%d", page, pageSize)
}

// groupListTotalKey 群组总数缓存键
func groupListTotalKey() string {
	return "group:list:total"
}

// groupMembersKey 群组成员列表缓存键
func groupMembersKey(groupID int) string {
	return fmt.Sprintf("group:members:%d", groupID)
}

// groupPublicListKey 公开群组列表缓存键
func groupPublicListKey() string {
	return "group:list:public"
}

// groupByUserKey 用户群组列表缓存键
func groupByUserKey(userID int) string {
	return fmt.Sprintf("group:user:%d", userID)
}

// GetGroupByID 通过ID获取群组详情（带缓存）
func (c *GroupCache) GetGroupByID(ctx context.Context, id int) (*gormdb.Group, error) {
	key := groupKey(id)

	var group gormdb.Group
	if err := c.cache.Get(ctx, key, &group); err == nil {
		return &group, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.NewGroupRepository()
	dbGroup, err := repo.GetGroupByID(id)
	if err != nil {
		return nil, err
	}
	if dbGroup == nil {
		return nil, nil
	}

	// 写入缓存（详情缓存 5 分钟）
	_ = c.cache.Set(ctx, key, dbGroup, 5*time.Minute)

	return dbGroup, nil
}

// GetGroupList 获取群组列表（带缓存，列表使用短TTL被动过期）
func (c *GroupCache) GetGroupList(ctx context.Context, page, pageSize int) ([]*gormdb.Group, int64, error) {
	itemsKey := groupListKey(page, pageSize)
	totalKey := groupListTotalKey()

	var groups []*gormdb.Group
	var total int64

	// 尝试从缓存获取列表和总数
	itemsHit := c.cache.Get(ctx, itemsKey, &groups) == nil
	totalHit := c.cache.Get(ctx, totalKey, &total) == nil

	// 完全命中缓存
	if itemsHit && totalHit {
		return groups, total, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.NewGroupRepository()
	dbGroups, err := repo.ListGroups()
	if err != nil {
		return nil, 0, err
	}

	// 计算总数和分页
	total = int64(len(dbGroups))
	offset := (page - 1) * pageSize
	if int64(offset) >= total {
		dbGroups = []*gormdb.Group{}
	} else if offset+pageSize > int(total) {
		dbGroups = dbGroups[offset:]
	} else {
		dbGroups = dbGroups[offset : offset+pageSize]
	}

	// 缓存穿透保护
	if dbGroups == nil {
		dbGroups = make([]*gormdb.Group, 0)
	}

	// 写入缓存（列表 1 分钟，总数 2 分钟）
	_ = c.cache.Set(ctx, itemsKey, dbGroups, time.Minute)
	_ = c.cache.Set(ctx, totalKey, total, 2*time.Minute)

	return dbGroups, total, nil
}

// GetPublicGroups 获取公开群组列表（带缓存）
func (c *GroupCache) GetPublicGroups(ctx context.Context) ([]*gormdb.Group, error) {
	key := groupPublicListKey()

	var groups []*gormdb.Group
	if err := c.cache.Get(ctx, key, &groups); err == nil {
		return groups, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.NewGroupRepository()
	dbGroups, err := repo.ListPublicGroups()
	if err != nil {
		return nil, err
	}

	// 缓存穿透保护
	if dbGroups == nil {
		dbGroups = make([]*gormdb.Group, 0)
	}

	// 写入缓存（公开群组列表 1 分钟）
	_ = c.cache.Set(ctx, key, dbGroups, time.Minute)

	return dbGroups, nil
}

// GetGroupMembers 获取群组成员列表（带缓存）
func (c *GroupCache) GetGroupMembers(ctx context.Context, groupID int) ([]*gormdb.GroupMember, error) {
	key := groupMembersKey(groupID)

	var members []*gormdb.GroupMember
	if err := c.cache.Get(ctx, key, &members); err == nil {
		return members, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.NewGroupMemberRepository()
	dbMembers, err := repo.ListMembersByGroup(groupID)
	if err != nil {
		return nil, err
	}

	// 缓存穿透保护
	if dbMembers == nil {
		dbMembers = make([]*gormdb.GroupMember, 0)
	}

	// 写入缓存（成员列表 30 秒）
	_ = c.cache.Set(ctx, key, dbMembers, 30*time.Second)

	return dbMembers, nil
}

// InvalidateGroup 使群组详情缓存失效（更新/删除群组时调用）
// 注意：列表缓存不主动失效，依赖TTL自然过期
func (c *GroupCache) InvalidateGroup(ctx context.Context, groupID int) error {
	keys := []string{
		groupKey(groupID),
		groupMembersKey(groupID),
	}
	return c.cache.Delete(ctx, keys...)
}

// InvalidateGroupMembers 使群组成员缓存失效
func (c *GroupCache) InvalidateGroupMembers(ctx context.Context, groupID int) error {
	return c.cache.Delete(ctx, groupMembersKey(groupID))
}

// InvalidateGroupList 使群组列表缓存失效（批量操作、新增、删除时调用）
// 不仅清理总数和公开列表Key，还要前缀清理全部分页列表
func (c *GroupCache) InvalidateGroupList(ctx context.Context) error {
	// 1. 删除固定名称的列表Key
	if err := c.cache.Delete(ctx, groupListTotalKey(), groupPublicListKey()); err != nil {
		return err
	}
	// 2. 主动删除所有分页相关的缓存 (形如 group:list:page:*)
	return c.cache.DeletePrefix(ctx, "group:list:page:")
}

// GetGroup 获取底层缓存接口（用于特殊操作）
func (c *GroupCache) GetGroup() *ThreeLevelCache {
	return c.cache
}

// GetGroupsByUserID 获取用户所属群组列表（带缓存）
func (c *GroupCache) GetGroupsByUserID(ctx context.Context, userID int) ([]*gormdb.GroupMember, error) {
	key := groupByUserKey(userID)

	var members []*gormdb.GroupMember
	if err := c.cache.Get(ctx, key, &members); err == nil {
		return members, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.NewGroupMemberRepository()
	dbMembers, err := repo.ListGroupsByUser(userID)
	if err != nil {
		return nil, err
	}

	// 缓存穿透保护
	if dbMembers == nil {
		dbMembers = make([]*gormdb.GroupMember, 0)
	}

	// 写入缓存（用户群组列表 1 分钟）
	_ = c.cache.Set(ctx, key, dbMembers, time.Minute)

	return dbMembers, nil
}

// InvalidateUserGroups 使用户群组列表缓存失效
func (c *GroupCache) InvalidateUserGroups(ctx context.Context, userID int) error {
	return c.cache.Delete(ctx, groupByUserKey(userID))
}
