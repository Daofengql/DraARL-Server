// Package routesync refreshes in-process sender state after committed device or group changes.
package routesync

import (
	"log"

	"draarl/internal/gormdb"
	"draarl/internal/interconnect"
	"draarl/internal/udphub"
)

func PublishDevice(deviceID int) bool {
	runtime := interconnect.ActiveCenterRuntime()
	if runtime == nil || runtime.Local == nil || deviceID <= 0 {
		return false
	}
	device, err := gormdb.NewDeviceRepository().GetDeviceByID(deviceID)
	if err != nil {
		log.Printf("[ROUTE_SYNC] load device %d failed: %v", deviceID, err)
		return false
	}
	if device == nil {
		return RevokeDevice(deviceID, "device_deleted")
	}
	groupID := device.GroupIDValue()
	updated, err := runtime.Local.UpdateActiveDeviceRoute(device.ID, groupID, udphub.GetActiveCommunicationDomainID(groupID), device.DisableSend, device.DisableRecv)
	if err != nil {
		log.Printf("[ROUTE_SYNC] publish device %d failed: %v", deviceID, err)
	}
	return updated
}

func PublishDevices(deviceIDs []int) {
	seen := make(map[int]struct{}, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		if deviceID <= 0 {
			continue
		}
		if _, ok := seen[deviceID]; ok {
			continue
		}
		seen[deviceID] = struct{}{}
		PublishDevice(deviceID)
	}
}

func RevokeDevice(deviceID int, reason string) bool {
	runtime := interconnect.ActiveCenterRuntime()
	if runtime == nil || runtime.Local == nil || deviceID <= 0 {
		return false
	}
	revoked, err := runtime.Local.RevokeActiveDevice(deviceID, reason)
	if err != nil {
		log.Printf("[ROUTE_SYNC] revoke device %d failed: %v", deviceID, err)
	}
	return revoked
}

func PublishIdentity(ownerID int, ssid byte, groupID int, disableSend, disableRecv bool) bool {
	runtime := interconnect.ActiveCenterRuntime()
	if runtime == nil || runtime.Local == nil {
		return false
	}
	updated, err := runtime.Local.UpdateActiveIdentityRoute(ownerID, ssid, groupID, udphub.GetActiveCommunicationDomainID(groupID), disableSend, disableRecv)
	if err != nil {
		log.Printf("[ROUTE_SYNC] publish owner=%d ssid=%d failed: %v", ownerID, ssid, err)
	}
	return updated
}

func RevokeOwner(ownerID int, reason string) int {
	runtime := interconnect.ActiveCenterRuntime()
	if runtime == nil || runtime.Local == nil {
		return 0
	}
	count, err := runtime.Local.RevokeActiveOwner(ownerID, reason)
	if err != nil {
		log.Printf("[ROUTE_SYNC] revoke owner %d failed: %v", ownerID, err)
	}
	return count
}

func RefreshTopology() {
	runtime := interconnect.ActiveCenterRuntime()
	if runtime == nil || runtime.Local == nil {
		return
	}
	if err := runtime.Local.RefreshActiveDeviceDomains(udphub.GetActiveCommunicationDomainID); err != nil {
		log.Printf("[ROUTE_SYNC] refresh local communication domains failed: %v", err)
	}
}
