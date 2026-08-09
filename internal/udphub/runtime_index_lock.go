package udphub

import (
	"sync"

	"draarl/internal/models"
)

// runtimeIndexMu 保护设备运行时索引 map 的并发读写。
// 热路径以 RLock 查找为主；写路径（上线/下线/加载）持写锁。
var runtimeIndexMu sync.RWMutex

func replaceRuntimeDeviceMaps(
	owner map[string]*models.Device,
	username map[string]*models.Device,
	callsign map[string]*models.Device,
) {
	runtimeIndexMu.Lock()
	devOwnerSSIDMap = owner
	devUsernameSSIDMap = username
	devCallsignSSIDMap = callsign
	runtimeIndexMu.Unlock()
}

func setOnlineDevMap(online map[int]*models.Device) {
	runtimeIndexMu.Lock()
	onlineDevMap = online
	runtimeIndexMu.Unlock()
}

func runtimeOwnerDeviceCount() int {
	runtimeIndexMu.RLock()
	n := len(devOwnerSSIDMap)
	runtimeIndexMu.RUnlock()
	return n
}

func getOwnerDeviceMapSnapshot() map[string]*models.Device {
	runtimeIndexMu.RLock()
	defer runtimeIndexMu.RUnlock()
	out := make(map[string]*models.Device, len(devOwnerSSIDMap))
	for k, v := range devOwnerSSIDMap {
		out[k] = v
	}
	return out
}

func rangeOwnerDevices(fn func(*models.Device)) {
	runtimeIndexMu.RLock()
	devices := make([]*models.Device, 0, len(devOwnerSSIDMap))
	for _, d := range devOwnerSSIDMap {
		if d != nil {
			devices = append(devices, d)
		}
	}
	runtimeIndexMu.RUnlock()
	for _, d := range devices {
		fn(d)
	}
}

func rangeCallsignDevices(fn func(*models.Device)) {
	runtimeIndexMu.RLock()
	devices := make([]*models.Device, 0, len(devCallsignSSIDMap))
	for _, d := range devCallsignSSIDMap {
		if d != nil {
			devices = append(devices, d)
		}
	}
	runtimeIndexMu.RUnlock()
	for _, d := range devices {
		fn(d)
	}
}

func lookupDeviceByUsernameSSID(username string, ssid byte) *models.Device {
	if username == "" {
		return nil
	}
	key := usernameSSIDKey(username, ssid)
	runtimeIndexMu.RLock()
	dev := devUsernameSSIDMap[key]
	runtimeIndexMu.RUnlock()
	return dev
}

func lookupDeviceByCallsignSSID(callsign string, ssid byte) *models.Device {
	if callsign == "" {
		return nil
	}
	key := callsignSSIDKey(callsign, ssid)
	runtimeIndexMu.RLock()
	dev := devCallsignSSIDMap[key]
	runtimeIndexMu.RUnlock()
	return dev
}
