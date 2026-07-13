package udphub

import (
	"fmt"
	"net"
	"strings"
	"time"

	"draarl/internal/models"
	"draarl/internal/protocol"
)

const runtimeDeviceActiveTimeout = 20 * time.Second

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
	if dev == nil || !dev.ISOnline || dev.LastPacketTime.IsZero() {
		return false
	}
	return time.Since(dev.LastPacketTime) <= runtimeDeviceActiveTimeout
}

func shouldRejectNormalDeviceConflict(dev *models.Device, addr *net.UDPAddr, incomingMAC string) bool {
	if dev == nil || !isRecentlyActiveDevice(dev) || dev.UDPAddr == nil {
		return false
	}
	incomingMAC = protocol.NormalizeMAC(incomingMAC)
	if incomingMAC != "" {
		existingMAC := protocol.NormalizeMAC(dev.MAC)
		if existingMAC == "" {
			existingMAC = runtimeDeviceMACStore.Get(dev.OwnerID, dev.SSID)
		}
		if existingMAC != "" && existingMAC == incomingMAC {
			return false
		}
	}
	return !sameUDPAddr(dev.UDPAddr, addr)
}

func indexRuntimeDevice(dev *models.Device) {
	if dev == nil {
		return
	}
	runtimeIndexMu.Lock()
	defer runtimeIndexMu.Unlock()
	indexRuntimeDeviceLocked(dev)
}

func indexRuntimeDeviceLocked(dev *models.Device) {
	if dev == nil {
		return
	}
	if dev.OwnerID > 0 {
		devOwnerSSIDMap[getOwnerSSIDKey(dev.OwnerID, dev.SSID)] = dev
	}
	if dev.Username != "" {
		devUsernameSSIDMap[usernameSSIDKey(dev.Username, dev.SSID)] = dev
	}
	if dev.CallSign != "" {
		dev.CallSignSSID = callsignSSIDKey(dev.CallSign, dev.SSID)
		devCallsignSSIDMap[dev.CallSignSSID] = dev
	}
	syncRuntimeDeviceMAC(dev)
}

func removeRuntimeUsernameKey(dev *models.Device, username string) {
	if dev == nil || username == "" {
		return
	}
	runtimeIndexMu.Lock()
	delete(devUsernameSSIDMap, usernameSSIDKey(username, dev.SSID))
	runtimeIndexMu.Unlock()
}

func removeRuntimeCallSignKey(dev *models.Device, callsign string) {
	if dev == nil || callsign == "" {
		return
	}
	runtimeIndexMu.Lock()
	delete(devCallsignSSIDMap, callsignSSIDKey(callsign, dev.SSID))
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
	if a.OwnerID > 0 && b.OwnerID > 0 {
		return a.OwnerID == b.OwnerID && a.SSID == b.SSID
	}
	return a.Username != "" && a.Username == b.Username && a.SSID == b.SSID
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

func RemoveRuntimeDevice(ownerID int, ssid byte) bool {
	dev := findDeviceByOwnerSSIDFromMemory(ownerID, ssid)
	if dev == nil {
		runtimeDeviceMACStore.Delete(ownerID, ssid)
		return false
	}

	runtimeIndexMu.Lock()
	delete(devOwnerSSIDMap, getOwnerSSIDKey(ownerID, ssid))
	if dev.Username != "" {
		delete(devUsernameSSIDMap, usernameSSIDKey(dev.Username, dev.SSID))
	}
	if dev.CallSign != "" {
		delete(devCallsignSSIDMap, callsignSSIDKey(dev.CallSign, dev.SSID))
	}
	delete(onlineDevMap, dev.ID)
	delete(onlineDevMapDraARL, dev.ID)
	runtimeIndexMu.Unlock()

	removeRuntimeDeviceMAC(dev)

	dev.ISOnline = false
	dev.UDPAddr = nil

	for _, gp := range publicGroupMap {
		removeDeviceFromGroupRuntime(gp, dev)
	}

	if cache := globalGroupCacheAtomic.Load(); cache != nil {
		if groupCache, ok := cache.(map[int]*models.Group); ok {
			for _, gp := range groupCache {
				removeDeviceFromGroupRuntime(gp, dev)
			}
		}
	}

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

func rewriteRuntimeAllowCallSignSSID(raw, oldCallSign, newCallSign string) (string, bool) {
	if raw == "" {
		return raw, false
	}

	oldPrefix := oldCallSign + "-"
	parts := strings.Split(raw, ",")
	changed := false
	for i, part := range parts {
		trimmed := strings.TrimSpace(part)
		if oldCallSign != "" && strings.HasPrefix(trimmed, oldPrefix) {
			parts[i] = newCallSign + trimmed[len(oldCallSign):]
			changed = true
			continue
		}
		parts[i] = trimmed
	}
	return strings.Join(parts, ","), changed
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

	for _, gp := range publicGroupMap {
		if gp == nil {
			continue
		}
		if rewritten, changed := rewriteRuntimeAllowCallSignSSID(gp.AllowCallSignSSID, oldCallSign, newCallSign); changed {
			gp.AllowCallSignSSID = rewritten
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

	runtimeIndexMu.Lock()
	for dev := range seen {
		if oldCallSign != "" {
			delete(devCallsignSSIDMap, callsignSSIDKey(oldCallSign, dev.SSID))
		}
		dev.CallSign = newCallSign
		dev.CallSignSSID = callsignSSIDKey(newCallSign, dev.SSID)
		devCallsignSSIDMap[dev.CallSignSSID] = dev
	}
	runtimeIndexMu.Unlock()

	GlobalUDPGhostManager.UpdateUserCallSign(ownerID, username, newCallSign)
}
