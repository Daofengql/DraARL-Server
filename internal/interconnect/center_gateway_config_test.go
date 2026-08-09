package interconnect

import (
	"errors"
	"testing"
	"time"
)

func TestCenterDeviceConfigRetryAndTimeout(t *testing.T) {
	now := time.Now()
	owner := deviceSessionOwner{
		NodeID:           "edge-a",
		ControlSessionID: 11,
		SessionID:        21,
		SessionEpoch:     31,
		DeviceID:         41,
	}
	retry := &pendingDeviceConfigDelivery{
		owner:      owner,
		envelope:   Envelope{MessageID: 1},
		createdAt:  now.Add(-deviceConfigRetryAfter),
		lastSentAt: now.Add(-deviceConfigRetryAfter),
		attempts:   1,
		result:     make(chan error, 1),
	}
	expired := &pendingDeviceConfigDelivery{
		owner:      owner,
		envelope:   Envelope{MessageID: 2},
		createdAt:  now.Add(-deviceConfigTimeout),
		lastSentAt: now.Add(-deviceConfigRetryAfter),
		attempts:   1,
		result:     make(chan error, 1),
	}
	gateway := &CenterGateway{
		deviceSessions: map[uint64]deviceSessionOwner{owner.SessionID: owner},
		activeByID:     map[int]uint64{owner.DeviceID: owner.SessionID},
		configPending:  map[uint64]*pendingDeviceConfigDelivery{1: retry, 2: expired},
		configUpCache:  make(map[deviceConfigCacheKey]cachedDeviceConfigResult),
	}

	gateway.retryPendingDeviceConfigs(now)

	if retry.attempts != 2 || !retry.lastSentAt.Equal(now) {
		t.Fatalf("retry state = attempts %d last_sent %v, want attempts 2 at %v", retry.attempts, retry.lastSentAt, now)
	}
	if _, ok := gateway.configPending[1]; !ok {
		t.Fatal("retryable delivery was removed from the pending queue")
	}
	if _, ok := gateway.configPending[2]; ok {
		t.Fatal("expired delivery remained in the pending queue")
	}
	select {
	case err := <-expired.result:
		if !errors.Is(err, errDeviceConfigTimeout) {
			t.Fatalf("expired delivery result = %v, want %v", err, errDeviceConfigTimeout)
		}
	default:
		t.Fatal("expired delivery did not report its timeout")
	}
}
