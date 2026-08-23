package cache

import (
	"container/list"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// localCache 本地内存缓存。
// 【S2 修复】淘汰机制改为真正的 LRU：Set/Get 命中都会把条目移动到链表
// 头部，容量超限时从链表尾部淘汰最久未使用的条目；Get 命中过期条目时
// 惰性删除，避免过期键长期滞留导致内存无界增长。链表位置用 map 索引，
// Get/Set 均为 O(1)。
type localCache struct {
	mu      sync.RWMutex
	items   map[string]*cacheItem
	lruList *list.List               // 元素为 *lruEntry，头部最新、尾部最旧
	lruIdx  map[string]*list.Element // key -> 链表元素，O(1) 定位
	maxSize int
}

type cacheItem struct {
	data      interface{}
	expiredAt time.Time
}

type lruEntry struct {
	key string
}

func newLocalCache(maxSize int) *localCache {
	return &localCache{
		items:   make(map[string]*cacheItem),
		lruList: list.New(),
		lruIdx:  make(map[string]*list.Element),
		maxSize: maxSize,
	}
}

func (lc *localCache) Get(key string, dest interface{}) bool {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	item, ok := lc.items[key]
	if !ok {
		return false
	}

	// 检查是否过期；过期则惰性删除并返回未命中
	if time.Now().After(item.expiredAt) {
		delete(lc.items, key)
		if elem, has := lc.lruIdx[key]; has {
			lc.lruList.Remove(elem)
			delete(lc.lruIdx, key)
		}
		return false
	}

	// 命中：刷新 LRU 顺序
	if elem, has := lc.lruIdx[key]; has {
		lc.lruList.MoveToFront(elem)
	}

	// 类型断言
	switch v := dest.(type) {
	case *[]byte:
		if data, ok := item.data.([]byte); ok {
			// 返回拷贝，避免调用方持有内部切片造成别名修改
			*v = append([]byte(nil), data...)
			return true
		}
	case *string:
		if str, ok := item.data.(string); ok {
			*v = str
			return true
		}
	default:
		// 尝试JSON反序列化
		if data, ok := item.data.([]byte); ok {
			return json.Unmarshal(data, dest) == nil
		}
	}

	return false
}

func (lc *localCache) Set(key string, value interface{}, ttl time.Duration) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	expiredAt := time.Now().Add(ttl)
	if ttl == 0 {
		expiredAt = time.Now().Add(5 * time.Minute)
	}

	// 已存在则更新并移动到头部
	if elem, has := lc.lruIdx[key]; has {
		lc.lruList.MoveToFront(elem)
	} else {
		// 容量超限时淘汰最久未使用的条目（尾部）
		if lc.maxSize > 0 && len(lc.items) >= lc.maxSize {
			lc.evict()
		}
		lc.lruIdx[key] = lc.lruList.PushFront(&lruEntry{key: key})
	}

	lc.items[key] = &cacheItem{
		data:      value,
		expiredAt: expiredAt,
	}
}

func (lc *localCache) Delete(key string) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	delete(lc.items, key)
	if elem, has := lc.lruIdx[key]; has {
		lc.lruList.Remove(elem)
		delete(lc.lruIdx, key)
	}
}

func (lc *localCache) Clear() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.items = make(map[string]*cacheItem)
	lc.lruList.Init()
	lc.lruIdx = make(map[string]*list.Element)
}

// DeletePrefix 本地缓存按前缀删除
// 遍历 Map，发现前缀匹配的直接 delete
func (lc *localCache) DeletePrefix(prefix string) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	for key := range lc.items {
		if strings.HasPrefix(key, prefix) {
			delete(lc.items, key)
		}
	}
	// 重建 LRU 链表：仅保留未被删除的键
	newList := list.New()
	newIdx := make(map[string]*list.Element, len(lc.items))
	for e := lc.lruList.Front(); e != nil; e = e.Next() {
		entry, ok := e.Value.(*lruEntry)
		if !ok {
			continue
		}
		if _, exists := lc.items[entry.key]; exists {
			newIdx[entry.key] = newList.PushBack(entry)
		}
	}
	lc.lruList = newList
	lc.lruIdx = newIdx
}

// evict 淘汰最久未使用的条目（链表尾部）。需在持锁时调用。
func (lc *localCache) evict() {
	elem := lc.lruList.Back()
	if elem == nil {
		return
	}
	entry, ok := elem.Value.(*lruEntry)
	if !ok {
		lc.lruList.Remove(elem)
		return
	}
	lc.lruList.Remove(elem)
	delete(lc.lruIdx, entry.key)
	delete(lc.items, entry.key)
}
