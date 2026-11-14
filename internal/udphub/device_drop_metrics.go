package udphub

import (
	"container/list"
	"log"
	"sync"
	"time"

	"draarl/internal/models"
)

const maxDeviceDropEntries = 256

type deviceDropMetrics struct {
	Username        string `json:"username"`
	SSID            byte   `json:"ssid"`
	SessionID       string `json:"session_id,omitempty"`
	EndpointRejects uint64 `json:"endpoint_rejects"`
	VoiceEndpoint   uint64 `json:"voice_endpoint"`
	VoiceDisabled   uint64 `json:"voice_disabled"`
	VoiceHalfDuplex uint64 `json:"voice_half_duplex"`
	lastLog         time.Time
}

type deviceDropKey struct {
	username  string
	ssid      byte
	sessionID string
}
type deviceDropEntry struct {
	key     deviceDropKey
	metrics deviceDropMetrics
}

// Only rejected packets touch this bounded collector; forwarded audio does not.
var deviceDrops = struct {
	sync.Mutex
	entries map[deviceDropKey]*list.Element
	recent  list.List
}{entries: make(map[deviceDropKey]*list.Element)}

func recordDeviceDrop(state models.DeviceRuntimeSnapshot, reason string, voice bool, bound, source string, packetType byte) {
	key := deviceDropKey{state.Username, state.SSID, state.GhostSessionID}
	now := time.Now()
	deviceDrops.Lock()
	element := deviceDrops.entries[key]
	if element == nil {
		if len(deviceDrops.entries) >= maxDeviceDropEntries {
			oldest := deviceDrops.recent.Back()
			delete(deviceDrops.entries, oldest.Value.(*deviceDropEntry).key)
			deviceDrops.recent.Remove(oldest)
		}
		element = deviceDrops.recent.PushFront(&deviceDropEntry{key: key, metrics: deviceDropMetrics{
			Username: state.Username, SSID: state.SSID, SessionID: state.GhostSessionID,
		}})
		deviceDrops.entries[key] = element
	} else {
		deviceDrops.recent.MoveToFront(element)
	}
	metrics := &element.Value.(*deviceDropEntry).metrics
	switch reason {
	case "endpoint_mismatch":
		metrics.EndpointRejects++
		if voice {
			metrics.VoiceEndpoint++
		}
	case "disabled":
		metrics.VoiceDisabled++
	case "half_duplex":
		metrics.VoiceHalfDuplex++
	}
	writeLog := now.Sub(metrics.lastLog) >= 30*time.Second
	if writeLog {
		metrics.lastLog = now
	}
	snapshot := *metrics
	deviceDrops.Unlock()
	if writeLog {
		log.Printf("[UDP-DROP] device=%s-%d reason=%s bound=%s source=%s type=%d endpoint_rejects=%d voice_endpoint=%d voice_disabled=%d voice_half_duplex=%d",
			snapshot.Username, snapshot.SSID, reason, bound, source, packetType, snapshot.EndpointRejects, snapshot.VoiceEndpoint, snapshot.VoiceDisabled, snapshot.VoiceHalfDuplex)
	}
}

func GetDeviceDropMetrics() []deviceDropMetrics {
	deviceDrops.Lock()
	defer deviceDrops.Unlock()
	result := make([]deviceDropMetrics, 0, len(deviceDrops.entries))
	for element := deviceDrops.recent.Front(); element != nil; element = element.Next() {
		result = append(result, element.Value.(*deviceDropEntry).metrics)
	}
	return result
}
