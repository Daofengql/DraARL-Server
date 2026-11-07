package udphub

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"draarl/internal/models"
	"draarl/internal/protocol"
	"draarl/pkg/cache"
)

const runtimeDeviceActiveTimeout = 20 * time.Second

var deviceGroupCleanupFallbacks atomic.Uint64

// Device.GroupID is maintained by the same detach/attach path as membership.
// Only a missing group requires the compatibility scan across cached groups.
func visitRuntimeDeviceGroups(dev *models.Device, visit func(*models.Group)) {
	state := dev.RuntimeSnapshot()
	if gp, ok := GetGroupFromCache(state.GroupID); ok && gp != nil {
		visit(gp)
	} else if state.GroupID > 0 {
		count := deviceGroupCleanupFallbacks.Add(1)
		if count == 1 || count%100 == 0 {
			log.Printf("[WARN] device group cleanup index missing: device=%d group=%d fallbacks=%d", state.ID, state.GroupID, count)
		}
		for _, gp := range GetAllGroupsFromCache() {
			visit(gp)
		}
	}
}

func getOwnerSSIDKey(ownerID int, ssid byte) string {
	return fmt.Sprintf("%d-%d", ownerID, ssid)
}

func usernameSSIDKey(username string, ssid byte) string {
	return protocol.GetUsernameSSID(username, ssid)
}

func callsignSSIDKey(callsign string, ssid byte) string {
	return protocol.GetCallSignSSID(callsign, ssid)
}

func sameUDPAddr(a, b *net.UDPAddr) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Port == b.Port && a.IP.Equal(b.IP)
}

func isRecentlyActiveDevice(dev *models.Device) bool {
	if dev == nil {
		return false
	}
	state := dev.RuntimeSnapshot()
	if !state.ISOnline || state.LastPacketTime.IsZero() {
		return false
	}
	return time.Since(state.LastPacketTime) <= runtimeDeviceActiveTimeout
}

func shouldRejectNormalDeviceConflict(dev *models.Device, addr *net.UDPAddr, incomingMAC string) bool {
	if dev == nil {
		return false
	}
	return shouldRejectNormalDeviceConflictForModel(dev, addr, incomingMAC, dev.RuntimeSnapshot().DevModel)
}

func shouldRejectNormalDeviceConflictForModel(dev *models.Device, addr *net.UDPAddr, incomingMAC string, incomingDevModel byte) bool {
	if dev == nil || !isRecentlyActiveDevice(dev) {
		return false
	}
	state := dev.RuntimeSnapshot()
	boundAddr := state.RealUDPAddr
	if boundAddr == nil {
		boundAddr = state.UDPAddr
	}
	if boundAddr == nil {
		return false
	}
	if sameUDPAddr(boundAddr, addr) {
		return false
	}

	// Modern Android releases may not expose a stable hardware MAC. A
	// password-authenticated heartbeat is sufficient to take over a new NAT
	// binding after a network switch. A supplied MAC remains checked below.
	incomingMAC = protocol.NormalizeMAC(incomingMAC)
	if incomingMAC == "" && (state.DevModel == protocol.DraARLDevModelAndroid || incomingDevModel == protocol.DraARLDevModelAndroid) {
		return false
	}
	if incomingMAC != "" {
		existingMAC := protocol.NormalizeMAC(state.MAC)
		if existingMAC == "" {
			existingMAC = runtimeDeviceMACStore.Get(state.OwnerID, state.SSID)
		}
		if existingMAC != "" && existingMAC == incomingMAC {
			return false
		}
	}
	return true
}

func indexRuntimeDevice(dev *models.Device) {
	if dev == nil {
		return
	}
	// Device state always precedes the index lock. Readers copy map entries
	// before taking a device snapshot, so neither lock waits in reverse order.
	dev.UpdateRuntime(func(current *models.Device) {
		if current.CallSign != "" {
			current.CallSignSSID = callsignSSIDKey(current.CallSign, current.SSID)
		}
		runtimeIndexMu.Lock()
		defer runtimeIndexMu.Unlock()
		if current.OwnerID > 0 {
			devOwnerSSIDMap[getOwnerSSIDKey(current.OwnerID, current.SSID)] = dev
		}
		if current.Username != "" {
			devUsernameSSIDMap[usernameSSIDKey(current.Username, current.SSID)] = dev
		}
		if current.CallSign != "" {
			devCallsignSSIDMap[current.CallSignSSID] = dev
		}
	})
	// Redis is an external dependency and may stall during a failover. Keep
	// that I/O outside the global runtime-index lock so one device cannot block
	// lookups or indexing for every other device.
	syncRuntimeDeviceMAC(dev)
}

func removeRuntimeUsernameKey(dev *models.Device, username string) {
	if dev == nil || username == "" {
		return
	}
	state := dev.RuntimeSnapshot()
	runtimeIndexMu.Lock()
	key := usernameSSIDKey(username, state.SSID)
	if devUsernameSSIDMap[key] == dev {
		delete(devUsernameSSIDMap, key)
	}
	runtimeIndexMu.Unlock()
}

func removeRuntimeCallSignKey(dev *models.Device, callsign string) {
	if dev == nil || callsign == "" {
		return
	}
	state := dev.RuntimeSnapshot()
	runtimeIndexMu.Lock()
	key := callsignSSIDKey(callsign, state.SSID)
	if devCallsignSSIDMap[key] == dev {
		delete(devCallsignSSIDMap, key)
	}
	runtimeIndexMu.Unlock()
}

func findDeviceByOwnerSSIDFromMemory(ownerID int, ssid byte) *models.Device {
	if ownerID <= 0 {
		return nil
	}
	runtimeIndexMu.RLock()
	dev := devOwnerSSIDMap[getOwnerSSIDKey(ownerID, ssid)]
	runtimeIndexMu.RUnlock()
	return dev
}

func IsRuntimeNormalDeviceActive(ownerID int, ssid byte) bool {
	return isRecentlyActiveDevice(findDeviceByOwnerSSIDFromMemory(ownerID, ssid))
}

func isSameRuntimeDevice(a, b *models.Device) bool {
	if a == nil || b == nil {
		return false
	}
	if a == b {
		return true
	}
	aState, bState := a.RuntimeSnapshot(), b.RuntimeSnapshot()
	if aState.OwnerID > 0 && bState.OwnerID > 0 {
		return aState.OwnerID == bState.OwnerID && aState.SSID == bState.SSID
	}
	return aState.Username != "" && aState.Username == bState.Username && aState.SSID == bState.SSID
}

func syncDeviceConnPool(pool *CurrentConnPool, dev *models.Device, addr *net.UDPAddr) {
	if pool == nil || dev == nil || addr == nil {
		return
	}

	pool.mu.Lock()
	defer pool.mu.Unlock()

	if pool.DevConnMap == nil {
		pool.DevConnMap = make(map[string]*models.Device)
	}

	addrKey := addr.String()
	// 热路径：同设备同地址已在池中时直接返回，避免心跳反复 rebuild/invalidate。
	if existing, ok := pool.DevConnMap[addrKey]; ok && existing == dev {
		return
	}

	membershipChanged := false
	// 增量清理：仅移除同地址或同一设备的旧条目。
	for key, existing := range pool.DevConnMap {
		if existing == nil {
			delete(pool.DevConnMap, key)
			membershipChanged = true
			continue
		}
		if key == addrKey {
			if existing != dev {
				membershipChanged = true
			}
			continue
		}
		if isSameRuntimeDevice(existing, dev) {
			delete(pool.DevConnMap, key)
			membershipChanged = true
		}
	}

	if prev, ok := pool.DevConnMap[addrKey]; !ok || prev != dev {
		membershipChanged = true
	}
	pool.DevConnMap[addrKey] = dev

	if !membershipChanged {
		return
	}
	rebuildDeviceConnListLocked(pool)
	// 仅真实成员/地址变化时失效域接收者缓存
	InvalidateDomainReceiverCache()
}

func rebuildDeviceConnList(pool *CurrentConnPool) {
	if pool == nil {
		return
	}
	pool.mu.Lock()
	defer pool.mu.Unlock()
	rebuildDeviceConnListLocked(pool)
}

func rebuildDeviceConnListLocked(pool *CurrentConnPool) {
	if pool == nil {
		return
	}
	list := make([]*models.Device, 0, len(pool.DevConnMap))
	for _, existing := range pool.DevConnMap {
		if existing == nil {
			continue
		}
		list = append(list, existing)
	}
	pool.storeConnList(list)
}

func removeDeviceFromGroupRuntime(gp *models.Group, dev *models.Device) {
	if gp == nil || dev == nil {
		return
	}

	groupRuntimeMu.Lock()
	if gp.DevMap != nil {
		for id, existing := range gp.DevMap {
			if existing == nil || isSameRuntimeDevice(existing, dev) {
				delete(gp.DevMap, id)
			}
		}
	}

	if len(gp.DevList) > 0 {
		filtered := gp.DevList[:0]
		for _, id := range gp.DevList {
			if id != dev.ID {
				filtered = append(filtered, id)
			}
		}
		gp.DevList = filtered
	}
	groupRuntimeMu.Unlock()

	pool := getGroupConnPool(gp)
	if pool == nil {
		return
	}
	pool.mu.Lock()
	for key, existing := range pool.DevConnMap {
		if existing == nil || isSameRuntimeDevice(existing, dev) {
			delete(pool.DevConnMap, key)
		}
	}
	rebuildDeviceConnListLocked(pool)
	pool.mu.Unlock()
	InvalidateDomainReceiverCache()
}

func removeDeviceConnectionFromGroup(gp *models.Group, dev *models.Device) {
	if gp == nil || dev == nil {
		return
	}
	pool := getGroupConnPool(gp)
	if pool == nil {
		return
	}
	pool.mu.Lock()
	changed := false
	for key, existing := range pool.DevConnMap {
		if existing == nil || isSameRuntimeDevice(existing, dev) {
			delete(pool.DevConnMap, key)
			changed = true
		}
	}
	if changed {
		rebuildDeviceConnListLocked(pool)
	}
	pool.mu.Unlock()
	if changed {
		InvalidateDomainReceiverCache()
	}
}

func invalidateDeviceEntryCache(dev *models.Device) {
	if dev == nil {
		return
	}
	deviceCache := cache.GetDeviceCache()
	if deviceCache == nil {
		return
	}
	ctx := context.Background()
	_ = deviceCache.InvalidateDevice(ctx, dev.ID, dev.OwnerID, dev.SSID)
	_ = deviceCache.InvalidateDeviceList(ctx)
	state := dev.RuntimeSnapshot()
	if state.GroupID > 0 {
		_ = deviceCache.InvalidateDevicesByGroup(ctx, state.GroupID)
	}
}

// SyncRuntimeDeviceEntry keeps the centre's in-memory device view aligned
// with the committed entry ownership. Remote devices stay in their persistent
// group membership, but are never left in a centre-local UDP fan-out pool.
func SyncRuntimeDeviceEntry(deviceID int, nodeID, mode string, sessionID uint64, online bool, seenAt time.Time) {
	dev := GetDeviceByID(deviceID)
	if dev == nil {
		return
	}
	remote := nodeID != "center"
	dev.UpdateRuntime(func(current *models.Device) {
		current.CurrentEntryNodeID = nodeID
		current.CurrentEntrySessionID = sessionID
		if nodeID != "" {
			copyTime := seenAt
			current.LastEntryNodeID = nodeID
			current.LastEntryAt = &copyTime
		}
		current.EntryMode = mode
		current.ISOnline = online
		if online {
			current.OnlineTime = seenAt
		}
		if remote || !online {
			current.UDPAddr = nil
			current.RealUDPAddr = nil
		}
		runtimeIndexMu.Lock()
		if online {
			onlineDevMap[current.ID] = dev
			onlineDevMapDraARL[current.ID] = dev
		} else {
			delete(onlineDevMap, current.ID)
			delete(onlineDevMapDraARL, current.ID)
		}
		runtimeIndexMu.Unlock()
	})

	if remote || !online {
		visitRuntimeDeviceGroups(dev, func(gp *models.Group) {
			removeDeviceConnectionFromGroup(gp, dev)
		})
	}
	invalidateDeviceEntryCache(dev)
}

// ClearRuntimeDeviceEntryIfNode prevents a stale disconnect from overwriting
// a newer entry assignment made during a reconnect or node-to-node roam.
func ClearRuntimeDeviceEntryIfSession(deviceID int, nodeID string, sessionID uint64) {
	dev := GetDeviceByID(deviceID)
	if dev == nil {
		return
	}
	state := dev.RuntimeSnapshot()
	matches := state.CurrentEntryNodeID == nodeID && state.CurrentEntrySessionID == sessionID
	if !matches {
		invalidateDeviceEntryCache(dev)
		return
	}
	SyncRuntimeDeviceEntry(deviceID, "", state.EntryMode, 0, false, time.Now())
}

func ClearRuntimeDeviceEntryIfNode(deviceID int, nodeID string) {
	dev := GetDeviceByID(deviceID)
	if dev == nil {
		return
	}
	state := dev.RuntimeSnapshot()
	matches := state.CurrentEntryNodeID == nodeID
	mode := state.EntryMode
	if !matches {
		invalidateDeviceEntryCache(dev)
		return
	}
	SyncRuntimeDeviceEntry(deviceID, "", mode, 0, false, time.Now())
}

func RemoveRuntimeDevice(ownerID int, ssid byte) bool {
	dev := findDeviceByOwnerSSIDFromMemory(ownerID, ssid)
	if dev == nil {
		runtimeDeviceMACStore.Delete(ownerID, ssid)
		return false
	}

	state := dev.RuntimeSnapshot()
	runtimeIndexMu.Lock()
	delete(devOwnerSSIDMap, getOwnerSSIDKey(ownerID, ssid))
	if state.Username != "" {
		delete(devUsernameSSIDMap, usernameSSIDKey(state.Username, state.SSID))
	}
	if state.CallSign != "" {
		delete(devCallsignSSIDMap, callsignSSIDKey(state.CallSign, state.SSID))
	}
	delete(onlineDevMap, dev.ID)
	delete(onlineDevMapDraARL, dev.ID)
	runtimeIndexMu.Unlock()

	removeRuntimeDeviceMAC(dev)

	dev.UpdateRuntime(func(current *models.Device) {
		current.ISOnline = false
		current.UDPAddr = nil
		current.RealUDPAddr = nil
	})

	visitRuntimeDeviceGroups(dev, func(gp *models.Group) {
		removeDeviceFromGroupRuntime(gp, dev)
	})

	userList.Range(func(_, value any) bool {
		info, ok := value.(*UserInfo)
		if !ok {
			return true
		}
		for _, gp := range info.Groups {
			removeDeviceFromGroupRuntime(gp, dev)
		}
		return true
	})

	return true
}

func sendHeartbeatReject(conn *net.UDPConn, packet *protocol.DraARLv1Packet, code byte, message string) {
	if conn == nil || packet == nil || packet.UDPAddr == nil {
		return
	}
	conn.WriteToUDP(protocol.EncodeHeartbeatRejectResponse(packet, code, message), packet.UDPAddr)
}

// SyncUserCallSignChange 在呼号审批真正落库后，同步 UDP 运行时索引与展示字段。
func SyncUserCallSignChange(ownerID int, username, oldCallSign, newCallSign string) {
	oldCallSign = strings.ToUpper(strings.TrimSpace(oldCallSign))
	newCallSign = strings.ToUpper(strings.TrimSpace(newCallSign))
	if ownerID <= 0 || newCallSign == "" {
		return
	}

	seen := make(map[*models.Device]struct{}, 16)
	collect := func(dev *models.Device) {
		if dev == nil || dev.OwnerID != ownerID {
			return
		}
		seen[dev] = struct{}{}
	}

	rangeOwnerDevices(collect)
	rangeCallsignDevices(collect)
	runtimeIndexMu.RLock()
	for _, dev := range onlineDevMap {
		collect(dev)
	}
	for _, dev := range onlineDevMapDraARL {
		collect(dev)
	}
	runtimeIndexMu.RUnlock()

	for _, gp := range GetAllGroupsFromCache() {
		if gp == nil {
			continue
		}
		pool := getGroupConnPool(gp)
		if pool == nil {
			continue
		}
		for _, dev := range pool.snapshotConnList() {
			collect(dev)
		}
	}

	if oldCallSign != "" {
		if value, ok := userList.Load(oldCallSign); ok {
			if info, ok := value.(*UserInfo); ok {
				info.CallSign = newCallSign
				userList.Store(newCallSign, info)
				userList.Delete(oldCallSign)
			}
		}
	}
	if username != "" {
		userList.Range(func(_, value any) bool {
			info, ok := value.(*UserInfo)
			if !ok || info.Name != username {
				return true
			}
			info.CallSign = newCallSign
			return true
		})
	}

	for dev := range seen {
		dev.UpdateRuntime(func(current *models.Device) {
			runtimeIndexMu.Lock()
			defer runtimeIndexMu.Unlock()
			oldKey := callsignSSIDKey(oldCallSign, current.SSID)
			if oldCallSign != "" && devCallsignSSIDMap[oldKey] == dev {
				delete(devCallsignSSIDMap, oldKey)
			}
			current.CallSign = newCallSign
			current.CallSignSSID = callsignSSIDKey(newCallSign, current.SSID)
			devCallsignSSIDMap[current.CallSignSSID] = dev
		})
	}

	GlobalUDPGhostManager.UpdateUserCallSign(ownerID, username, newCallSign)
}
