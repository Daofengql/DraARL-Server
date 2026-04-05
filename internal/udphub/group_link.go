package udphub

import (
	"log"
	"sort"
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
	// targetGroupID -> []peerTargetGroupID (已去重，且过滤掉停用互联组)
	targetToPeers map[int][]int
}

// 全局群组互联缓存
var globalGroupLinkCache = &GroupLinkCache{
	targetToLinks: make(map[int][]int),
	linkToTargets: make(map[int][]int),
	targetToPeers: make(map[int][]int),
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

	// 预计算：每个源群组可以直达的目标群组（去重，过滤停用互联组）
	newTargetToPeers := make(map[int][]int, len(newTargetToLinks))
	for sourceGroupID, linkGroupIDs := range newTargetToLinks {
		peerSet := make(map[int]struct{})
		for _, linkGroupID := range linkGroupIDs {
			linkGroup, exists := GetGroupFromCache(linkGroupID)
			if !exists || linkGroup.Status != 1 {
				continue
			}
			for _, targetID := range newLinkToTargets[linkGroupID] {
				if targetID == sourceGroupID {
					continue
				}
				peerSet[targetID] = struct{}{}
			}
		}
		if len(peerSet) == 0 {
			continue
		}
		peers := make([]int, 0, len(peerSet))
		for targetID := range peerSet {
			peers = append(peers, targetID)
		}
		sort.Ints(peers)
		newTargetToPeers[sourceGroupID] = peers
	}

	// 加写锁，安全更新缓存
	globalGroupLinkCache.Lock()
	globalGroupLinkCache.targetToLinks = newTargetToLinks
	globalGroupLinkCache.linkToTargets = newLinkToTargets
	globalGroupLinkCache.targetToPeers = newTargetToPeers
	globalGroupLinkCache.Unlock()

	// 群组互联拓扑更新后，重置半双工域缓存，确保仲裁范围与最新转发关系一致。
	resetHalfDuplexDomainCache()

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

// GetLinkedTargetGroups 获取源群组可转发到的目标群组（已去重）
// 性能优化：直接返回预计算结果，避免热路径上重复构建去重 map
func GetLinkedTargetGroups(sourceGroupID int) []int {
	globalGroupLinkCache.RLock()
	defer globalGroupLinkCache.RUnlock()

	if peers, ok := globalGroupLinkCache.targetToPeers[sourceGroupID]; ok {
		return peers
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
