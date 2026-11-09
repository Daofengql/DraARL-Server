package udphub

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"draarl/internal/gormdb"
	"draarl/internal/models"
	"draarl/pkg/cache"
)

const onlineEventQueueSize = 512

var (
	onlineEvents        = make(chan *models.Device, onlineEventQueueSize)
	onlineEventsDropped atomic.Uint64
	onlineEventsSynced  atomic.Uint64
)

func GetOnlineEventMetrics() map[string]uint64 {
	return map[string]uint64{"synced": onlineEventsSynced.Load(), "dropped": onlineEventsDropped.Load()}
}

func GetWebSocketOnlineCount() int {
	_, normal, ghost := onlineTransportOnlineCounts()
	return normal + ghost
}

// Notifications are best effort and never block packet processing. The full
// reconciliation remains responsible for queue overflow and transient DB errors.
func notifyDeviceOnlineChange(dev *models.Device) {
	if dev == nil || dev.RuntimeSnapshot().ID <= 0 {
		return
	}
	select {
	case onlineEvents <- dev:
	default:
		onlineEventsDropped.Add(1)
	}
}

func syncDeviceOnlineEvent(dev *models.Device) error {
	if dev == nil {
		return nil
	}
	deviceCacheSyncMu.Lock()
	defer deviceCacheSyncMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), groupCacheSyncTimeout)
	defer cancel()
	state := dev.RuntimeSnapshot()
	// A newer session can have replaced the queued device before this write.
	if latest := findDeviceByOwnerSSIDFromMemory(state.OwnerID, state.SSID); latest != nil {
		state = latest.RuntimeSnapshot()
	}
	onlineTime := ""
	if state.ISOnline && !state.OnlineTime.IsZero() {
		onlineTime = state.OnlineTime.Format(time.DateTime)
	}
	if err := gormdb.NewDeviceRepository().WithContext(ctx).UpdateDeviceOnlineStatus(state.OwnerID, state.SSID, state.ISOnline, onlineTime, state.LastOnlineIP); err != nil {
		return err
	}
	if dc := cache.GetDeviceCache(); dc != nil {
		if err := dc.InvalidateDevice(ctx, state.ID, state.OwnerID, state.SSID); err != nil {
			return err
		}
	}
	onlineEventsSynced.Add(1)
	return nil
}

func runOnlineEventSync() {
	for {
		select {
		case <-udpShutdown:
			return
		case dev := <-onlineEvents:
			if err := syncDeviceOnlineEvent(dev); err != nil {
				slog.Warn("device online event sync failed", "event", "device_online_sync", "device_id", dev.RuntimeSnapshot().ID, "error", err)
			}
		}
	}
}
