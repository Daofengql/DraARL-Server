package udphub

import (
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"draarl/internal/models"
)

// 成员变化会主动失效快照；较长 TTL 只负责兜底，避免稳定大组每两秒重建。
const domainReceiverTTL = 30 * time.Second

type domainReceiverEntry struct {
	addr          netip.AddrPort
	deviceID      int
	username      string
	ssid          byte
	sessionID     string
	sourceGroupV1 bool
}

type domainReceiverSnap struct {
	entries    []domainReceiverEntry
	partitions [][]domainReceiverEntry
	workers    int
	updatedAt  time.Time
	gen        uint64
}

var (
	domainReceiverCache       sync.Map // domainKey -> *domainReceiverSnap
	domainReceiverLifecycleMu sync.Mutex
	domainReceiverBuildMu     sync.Mutex
	domainReceiverStopCh      chan struct{}
	domainReceiverWg          sync.WaitGroup
	domainReceiverRunning     bool
	domainReceiverHits        int64
	domainReceiverMisses      int64
	domainReceiverRebuilds    int64
	domainReceiverBuildNanos  int64
	domainReceiverMaxEntries  int64
	domainReceiverCandidates  int64
	domainReceiverDeduped     int64
	domainReceiverEntries     int64
	domainReceiverGen         uint64
)

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
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				now := time.Now()
				gen := atomic.LoadUint64(&domainReceiverGen)
				domainReceiverCache.Range(func(key, value any) bool {
					if snap, ok := value.(*domainReceiverSnap); ok &&
						(snap.gen != gen || now.Sub(snap.updatedAt) > 3*domainReceiverTTL) {
						domainReceiverCache.Delete(key)
					}
					return true
				})
			}
		}
	}()
}

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

func InvalidateDomainReceiverCache() {
	atomic.AddUint64(&domainReceiverGen, 1)
}

func buildDomainReceiverSnap(sourceGroupID int, gen uint64, workers int) *domainReceiverSnap {
	started := time.Now()
	groupIDs := GetHalfDuplexDomainGroupIDs(sourceGroupID)
	if len(groupIDs) == 0 {
		groupIDs = []int{sourceGroupID}
	}
	entries := make([]domainReceiverEntry, 0, 64)
	seen := make(map[netip.AddrPort]struct{}, 64)
	candidates := int64(0)
	deduplicated := int64(0)

	addDev := func(dev *models.Device, expectedGroupID int) {
		if dev == nil || !dev.ISOnline || dev.UDPAddr == nil || dev.DisableRecv {
			return
		}
		// Physical and legacy devices are single-channel. Modern UDP ghost
		// sessions are already selected through their multi-group RX index.
		if dev.GhostSessionID == "" && dev.GroupID != expectedGroupID {
			return
		}
		addr, ok := udpAddrPort(dev.UDPAddr)
		if !ok {
			return
		}
		candidates++
		if _, ok := seen[addr]; ok {
			deduplicated++
			return
		}
		seen[addr] = struct{}{}
		entries = append(entries, domainReceiverEntry{
			addr: addr, deviceID: dev.ID, username: dev.Username, ssid: dev.SSID,
			sessionID:     dev.GhostSessionID,
			sourceGroupV1: dev.GhostProtocolVersion > 0 && ghostCapability(dev.GhostCapabilities, "source_group_v1"),
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

	if workers < 1 {
		workers = 1
	}
	partitions := make([][]domainReceiverEntry, workers)
	for i := range entries {
		index := addrPortShard(entries[i].addr, workers)
		partitions[index] = append(partitions[index], entries[i])
	}

	atomic.AddInt64(&domainReceiverRebuilds, 1)
	atomic.AddInt64(&domainReceiverBuildNanos, time.Since(started).Nanoseconds())
	updateMaxInt64(&domainReceiverMaxEntries, int64(len(entries)))
	atomic.AddInt64(&domainReceiverCandidates, candidates)
	atomic.AddInt64(&domainReceiverDeduped, deduplicated)
	atomic.AddInt64(&domainReceiverEntries, int64(len(entries)))
	return &domainReceiverSnap{
		entries: entries, partitions: partitions, workers: workers,
		updatedAt: time.Now(), gen: gen,
	}
}

func validDomainReceiverSnap(value any, gen uint64, workers int) (*domainReceiverSnap, bool) {
	snap, ok := value.(*domainReceiverSnap)
	return snap, ok && snap.gen == gen && snap.workers == workers && time.Since(snap.updatedAt) < domainReceiverTTL
}

func getDomainReceiverSnap(sourceGroupID int) *domainReceiverSnap {
	domainKey := getHalfDuplexDomainKey(sourceGroupID)
	gen := atomic.LoadUint64(&domainReceiverGen)
	workers := currentFanoutWorkerCount()
	if domainKey == "" {
		return buildDomainReceiverSnap(sourceGroupID, gen, workers)
	}
	if value, ok := domainReceiverCache.Load(domainKey); ok {
		if snap, valid := validDomainReceiverSnap(value, gen, workers); valid {
			atomic.AddInt64(&domainReceiverHits, 1)
			return snap
		}
	}

	atomic.AddInt64(&domainReceiverMisses, 1)
	domainReceiverBuildMu.Lock()
	defer domainReceiverBuildMu.Unlock()
	gen = atomic.LoadUint64(&domainReceiverGen)
	workers = currentFanoutWorkerCount()
	if value, ok := domainReceiverCache.Load(domainKey); ok {
		if snap, valid := validDomainReceiverSnap(value, gen, workers); valid {
			atomic.AddInt64(&domainReceiverHits, 1)
			return snap
		}
	}
	snap := buildDomainReceiverSnap(sourceGroupID, gen, workers)
	domainReceiverCache.Store(domainKey, snap)
	return snap
}

func getDomainReceiverEntries(sourceGroupID int) []domainReceiverEntry {
	return getDomainReceiverSnap(sourceGroupID).entries
}

func forwardVoiceDomain(source *models.Device, data []byte, sourceGroupID int) {
	if source == nil || len(data) == 0 {
		return
	}
	writeUDPDomain(data, getDomainReceiverSnap(sourceGroupID), source.ID, source.Username, source.SSID, source.GhostSessionID, sourceGroupID)
}

func GetDomainReceiverCacheStats() map[string]int64 {
	return map[string]int64{
		"hits":          atomic.LoadInt64(&domainReceiverHits),
		"misses":        atomic.LoadInt64(&domainReceiverMisses),
		"rebuilds":      atomic.LoadInt64(&domainReceiverRebuilds),
		"build_ns":      atomic.LoadInt64(&domainReceiverBuildNanos),
		"max_entries":   atomic.LoadInt64(&domainReceiverMaxEntries),
		"candidates":    atomic.LoadInt64(&domainReceiverCandidates),
		"deduplicated":  atomic.LoadInt64(&domainReceiverDeduped),
		"entries_built": atomic.LoadInt64(&domainReceiverEntries),
		"gen":           int64(atomic.LoadUint64(&domainReceiverGen)),
	}
}
