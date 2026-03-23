package udphub

import (
	"log"
	"sync"

	gormdb "draarl/internal/gormdb"
)

// ==========================================
// 群组互联缓存管理
// ==========================================

// GroupLinkCache 群组互联缓存结构
type GroupLinkCache struct {
	sync.RWMutex
	// targetGroupID -> []linkGroupID (一个实体组可能属于多个互联组)
	targetToLinks map[int][]int
	// linkGroupID -> []targetGroupID (一个互联组包含哪些实体组)
	linkToTargets map[int][]int
}

// 全局群组互联缓存
var globalGroupLinkCache = &GroupLinkCache{
	targetToLinks: make(map[int][]int),
	linkToTargets: make(map[int][]int),
}

// RefreshGroupCache 刷新群组缓存（导出函数，供外部调用）
func RefreshGroupCache() {
	refreshGroupCache()
	log.Println("[CACHE] 群组缓存已手动刷新")
}

// RefreshGroupLinkCache 刷新群组互联缓存（导出函数，供外部调用）
func RefreshGroupLinkCache() {
	refreshGroupLinkCache()
	log.Println("[CACHE] 群组互联缓存已刷新")
}

// refreshGroupLinkCache 刷新群组互联缓存的内部实现
func refreshGroupLinkCache() {
	repo := gormdb.NewGroupLinkRepository()
	links, err := repo.GetAllLinks()
	if err != nil {
		log.Printf("[CACHE] 从数据库加载群组互联关系失败: %v", err)
		return
	}

	// 构建新的缓存
	newTargetToLinks := make(map[int][]int)
	newLinkToTargets := make(map[int][]int)

	for _, link := range links {
		// target -> links 映射
		if _, ok := newTargetToLinks[link.TargetGroupID]; !ok {
			newTargetToLinks[link.TargetGroupID] = []int{}
		}
		newTargetToLinks[link.TargetGroupID] = append(newTargetToLinks[link.TargetGroupID], link.LinkGroupID)

		// link -> targets 映射
		if _, ok := newLinkToTargets[link.LinkGroupID]; !ok {
			newLinkToTargets[link.LinkGroupID] = []int{}
		}
		newLinkToTargets[link.LinkGroupID] = append(newLinkToTargets[link.LinkGroupID], link.TargetGroupID)
	}

	// 加写锁，安全更新缓存
	globalGroupLinkCache.Lock()
	globalGroupLinkCache.targetToLinks = newTargetToLinks
	globalGroupLinkCache.linkToTargets = newLinkToTargets
	globalGroupLinkCache.Unlock()

	log.Printf("[CACHE] 群组互联关系同步完成，共 %d 个关联", len(links))
}

// GetLinkGroupsForTarget 获取目标群组所属的所有互联组ID
// 性能优化：直接返回内部切片引用，避免拷贝
// 注意：调用方不应该修改返回的切片
func GetLinkGroupsForTarget(targetGroupID int) []int {
	globalGroupLinkCache.RLock()
	defer globalGroupLinkCache.RUnlock()

	if linkGroupIDs, ok := globalGroupLinkCache.targetToLinks[targetGroupID]; ok {
		return linkGroupIDs // 直接返回，不拷贝
	}
	return nil // 返回 nil 而不是空切片，便于 len() 判断
}

// GetTargetGroupsForLink 获取互联组关联的所有目标群组ID
// 性能优化：直接返回内部切片引用，避免拷贝
// 注意：调用方不应该修改返回的切片
func GetTargetGroupsForLink(linkGroupID int) []int {
	globalGroupLinkCache.RLock()
	defer globalGroupLinkCache.RUnlock()

	if targetGroupIDs, ok := globalGroupLinkCache.linkToTargets[linkGroupID]; ok {
		return targetGroupIDs // 直接返回，不拷贝
	}
	return nil
}

// HasGroupLinks 检查群组是否属于某个互联组
func HasGroupLinks(groupID int) bool {
	globalGroupLinkCache.RLock()
	defer globalGroupLinkCache.RUnlock()

	_, hasLinks := globalGroupLinkCache.targetToLinks[groupID]
	return hasLinks
}

// InitGroupLinkCache 初始化群组互联缓存（在服务器启动时调用）
func InitGroupLinkCache() {
	refreshGroupLinkCache()
}
