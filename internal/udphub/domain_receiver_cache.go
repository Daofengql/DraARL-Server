package udphub

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"draarl/internal/models"
)

// domainReceiverCache：缓存连通域内在线 UDP 接收者（含普通设备与 ghost），降低每帧全表扫描。
// 失效策略：拓扑变更 / 连接池真实成员变化；TTL 仅作兜底。

const domainReceiverTTL = 2 * time.Second

type domainReceiverEntry struct {
	addr     *net.UDPAddr
	deviceID int
	username string
	ssid     byte
}

type domainReceiverSnap struct {
	entries   []domainReceiverEntry
	updatedAt time.Time
	gen       uint64
}

var (
	domainReceiverCache       sync.Map // domainKey -> *domainReceiverSnap
	domainReceiverLifecycleMu sync.Mutex
	domainReceiverStopCh      chan struct{}
	domainReceiverWg          sync.WaitGroup
	domainReceiverRunning     bool
	domainReceiverHits        int64
	domainReceiverMisses      int64
	domainReceiverGen         uint64 // 全局代数：Invalidate 时递增，快照比对
)

var domainAddrSlicePool = sync.Pool{
	New: func() interface{} {
		s := make([]*net.UDPAddr, 0, 64)
		return &s
	},
}

// InitDomainReceiverCache 启动过期清理。
func InitDomainReceiverCache() {
	domainReceiverLifecycleMu.Lock()
	defer domainReceiverLifecycleMu.Unlock()
	if domainReceiverRunning {
		return
	}
	stopCh := make(chan struct{})
	domainReceiverStopCh = stopCh
	domainReceiverRunning = true
	domainReceiverWg.Add(1)
	go func() {
		defer domainReceiverWg.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				now := time.Now()
				gen := atomic.LoadUint64(&domainReceiverGen)
				domainReceiverCache.Range(func(key, value any) bool {
					if snap, ok := value.(*domainReceiverSnap); ok {
						if snap.gen != gen || now.Sub(snap.updatedAt) > 3*domainReceiverTTL {
							domainReceiverCache.Delete(key)
						}
					}
					return true
				})
			}
		}
	}()
}

// StopDomainReceiverCache 停止维护。
func StopDomainReceiverCache() {
	domainReceiverLifecycleMu.Lock()
	if domainReceiverRunning {
		close(domainReceiverStopCh)
		domainReceiverWg.Wait()
		domainReceiverRunning = false
		domainReceiverStopCh = nil
	}
	domainReceiverLifecycleMu.Unlock()
	domainReceiverCache.Range(func(key, _ any) bool {
		domainReceiverCache.Delete(key)
		return true
	})
}

// InvalidateDomainReceiverCache 拓扑/成员变更后失效（代数递增，旧快照自然 miss）。
func InvalidateDomainReceiverCache() {
	atomic.AddUint64(&domainReceiverGen, 1)
	// 不立即清空 map，避免热路径全局 rebuild；下次 get 时因 gen 不匹配重建。
}

func buildDomainReceiverSnap(sourceGroupID int, gen uint64) *domainReceiverSnap {
	groupIDs := GetHalfDuplexDomainGroupIDs(sourceGroupID)
	if len(groupIDs) == 0 {
		groupIDs = []int{sourceGroupID}
	}
	entries := make([]domainReceiverEntry, 0, 64)
	seen := make(map[string]struct{}, 64)

	addDev := func(dev *models.Device, expectedGroupID int) {
		if dev == nil || dev.GroupID != expectedGroupID || !dev.ISOnline || dev.UDPAddr == nil || dev.DisableRecv {
			return
		}
		key := dev.UDPAddr.String()
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		entries = append(entries, domainReceiverEntry{
			addr:     cloneUDPAddr(dev.UDPAddr),
			deviceID: dev.ID,
			username: dev.Username,
			ssid:     dev.SSID,
		})
	}

	for _, gid := range groupIDs {
		gp, ok := GetGroupFromCache(gid)
		if !ok || gp == nil || gp.Status != 1 {
			continue
		}
		if pool, ok := gp.ConnPool.(*CurrentConnPool); ok && pool != nil {
			for _, dev := range pool.snapshotConnList() {
				addDev(dev, gid)
			}
		}
		GlobalUDPGhostManager.ForEachOnlineByGroup(gid, func(dev *models.Device) {
			addDev(dev, gid)
		})
	}
	return &domainReceiverSnap{entries: entries, updatedAt: time.Now(), gen: gen}
}

func getDomainReceiverEntries(sourceGroupID int) []domainReceiverEntry {
	domainKey := getHalfDuplexDomainKey(sourceGroupID)
	gen := atomic.LoadUint64(&domainReceiverGen)
	if domainKey == "" {
		return buildDomainReceiverSnap(sourceGroupID, gen).entries
	}
	if v, ok := domainReceiverCache.Load(domainKey); ok {
		if snap, ok := v.(*domainReceiverSnap); ok &&
			snap.gen == gen &&
			time.Since(snap.updatedAt) < domainReceiverTTL {
			atomic.AddInt64(&domainReceiverHits, 1)
			return snap.entries
		}
	}
	atomic.AddInt64(&domainReceiverMisses, 1)
	snap := buildDomainReceiverSnap(sourceGroupID, gen)
	domainReceiverCache.Store(domainKey, snap)
	return snap.entries
}

// collectDomainUDPAddrs 返回连通域内排除源设备后的目标地址。
// 返回切片可归还 pool（releaseDomainAddrSlice）。
func collectDomainUDPAddrs(sourceGroupID int, sourceID int, sourceUsername string, sourceSSID byte) []*net.UDPAddr {
	entries := getDomainReceiverEntries(sourceGroupID)
	sp := domainAddrSlicePool.Get().(*[]*net.UDPAddr)
	out := (*sp)[:0]
	if cap(out) < len(entries) {
		out = make([]*net.UDPAddr, 0, len(entries))
	}
	for _, e := range entries {
		if sourceID > 0 && e.deviceID == sourceID {
			continue
		}
		if sourceUsername != "" && e.username == sourceUsername && e.ssid == sourceSSID {
			continue
		}
		if e.addr != nil {
			out = append(out, e.addr)
		}
	}
	return out
}

func releaseDomainAddrSlice(addrs []*net.UDPAddr) {
	if addrs == nil {
		return
	}
	if cap(addrs) == 0 || cap(addrs) > 4096 {
		return
	}
	for i := range addrs {
		addrs[i] = nil
	}
	s := addrs[:0]
	domainAddrSlicePool.Put(&s)
}

// forwardVoiceDomain 将已编码语音帧 fan-out 到连通域全部 UDP 目标（本群+互联+ghost）。
func forwardVoiceDomain(source *models.Device, data []byte, sourceGroupID int) {
	if source == nil || len(data) == 0 {
		return
	}
	addrs := collectDomainUDPAddrs(sourceGroupID, source.ID, source.Username, source.SSID)
	writeUDPFanout(data, addrs)
	releaseDomainAddrSlice(addrs)
}

// GetDomainReceiverCacheStats 监控。
func GetDomainReceiverCacheStats() map[string]int64 {
	return map[string]int64{
		"hits":   atomic.LoadInt64(&domainReceiverHits),
		"misses": atomic.LoadInt64(&domainReceiverMisses),
		"gen":    int64(atomic.LoadUint64(&domainReceiverGen)),
	}
}
