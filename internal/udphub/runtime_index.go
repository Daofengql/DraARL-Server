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

	if dev.OwnerID > 0 {
		devOwnerSSIDMap[getOwnerSSIDKey(dev.OwnerID, dev.SSID)] = dev
	}
	if dev.Username != "" {
		devUsernameSSIDMap[protocol.GetUsernameSSID(dev.Username, dev.SSID)] = dev
	}
	if dev.CallSign != "" {
		dev.CallSignSSID = protocol.GetCallSignSSID(dev.CallSign, dev.SSID)
		devCallsignSSIDMap[dev.CallSignSSID] = dev
	}
	syncRuntimeDeviceMAC(dev)
}

func removeRuntimeUsernameKey(dev *models.Device, username string) {
	if dev == nil || username == "" {
		return
	}
	delete(devUsernameSSIDMap, protocol.GetUsernameSSID(username, dev.SSID))
}

func removeRuntimeCallSignKey(dev *models.Device, callsign string) {
	if dev == nil || callsign == "" {
		return
	}
	delete(devCallsignSSIDMap, protocol.GetCallSignSSID(callsign, dev.SSID))
}

func findDeviceByOwnerSSIDFromMemory(ownerID int, ssid byte) *models.Device {
	if ownerID <= 0 {
		return nil
	}
	return devOwnerSSIDMap[getOwnerSSIDKey(ownerID, ssid)]
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
	if pool.DevConnMap == nil {
		pool.DevConnMap = make(map[string]*models.Device)
	}

	addrKey := addr.String()
	for key, existing := range pool.DevConnMap {
		if existing == nil {
			delete(pool.DevConnMap, key)
			continue
		}
		if key == addrKey || isSameRuntimeDevice(existing, dev) {
			delete(pool.DevConnMap, key)
		}
	}

	pool.DevConnMap[addrKey] = dev
	pool.DevConnList = make([]*models.Device, 0, len(pool.DevConnMap))
	for _, existing := range pool.DevConnMap {
		pool.DevConnList = append(pool.DevConnList, existing)
	}
}

func rebuildDeviceConnList(pool *CurrentConnPool) {
	if pool == nil {
		return
	}
	pool.DevConnList = make([]*models.Device, 0, len(pool.DevConnMap))
	for _, existing := range pool.DevConnMap {
		if existing == nil {
			continue
		}
		pool.DevConnList = append(pool.DevConnList, existing)
	}
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
	for key, existing := range pool.DevConnMap {
		if existing == nil || isSameRuntimeDevice(existing, dev) {
			delete(pool.DevConnMap, key)
		}
	}
	rebuildDeviceConnList(pool)
}

func RemoveRuntimeDevice(ownerID int, ssid byte) bool {
	dev := findDeviceByOwnerSSIDFromMemory(ownerID, ssid)
	if dev == nil {
		runtimeDeviceMACStore.Delete(ownerID, ssid)
		return false
	}

	delete(devOwnerSSIDMap, getOwnerSSIDKey(ownerID, ssid))
	removeRuntimeUsernameKey(dev, dev.Username)
	removeRuntimeCallSignKey(dev, dev.CallSign)
	delete(onlineDevMap, dev.ID)
	delete(onlineDevMapDraARL, dev.ID)
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

	for _, dev := range devOwnerSSIDMap {
		collect(dev)
	}
	for _, dev := range devUsernameSSIDMap {
		collect(dev)
	}
	for _, dev := range devCallsignSSIDMap {
		collect(dev)
	}
	for _, dev := range onlineDevMap {
		collect(dev)
	}
	for _, dev := range onlineDevMapDraARL {
		collect(dev)
	}
	for _, gp := range publicGroupMap {
		if gp == nil {
			continue
		}
		if rewritten, changed := rewriteRuntimeAllowCallSignSSID(gp.AllowCallSignSSID, oldCallSign, newCallSign); changed {
			gp.AllowCallSignSSID = rewritten
		}
		for _, dev := range gp.DevMap {
			collect(dev)
		}
		pool := getGroupConnPool(gp)
		if pool == nil {
			continue
		}
		for _, dev := range pool.DevConnMap {
			collect(dev)
		}
		for _, dev := range pool.DevConnList {
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
		removeRuntimeCallSignKey(dev, oldCallSign)
		dev.CallSign = newCallSign
		dev.CallSignSSID = protocol.GetCallSignSSID(newCallSign, dev.SSID)
		devCallsignSSIDMap[dev.CallSignSSID] = dev
	}

	GlobalUDPGhostManager.UpdateUserCallSign(ownerID, username, newCallSign)
}
