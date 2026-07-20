package websocket

import (
	"fmt"
	"testing"
	"time"

	"draarl/internal/interfaces"
)

func TestBroadcastSharesSinglePayloadAcrossDevices(t *testing.T) {
	wsFramesCopied.Store(0)
	wsBytesCopied.Store(0)
	wsWritesQueued.Store(0)
	wsWritesDropped.Store(0)
	wsWritesDrained.Store(0)

	manager := NewWSConnectionManager()
	first := &WSDevice{UserID: 1, SSID: 105, GroupID: 9001, DeviceType: DeviceTypeGhost, IsOnline: true, writeCh: make(chan *writeRequest, 1)}
	second := &WSDevice{UserID: 2, SSID: 105, GroupID: 9001, DeviceType: DeviceTypeGhost, IsOnline: true, writeCh: make(chan *writeRequest, 1)}
	manager.addToGlobalGroupIndex(9001, "first", first)
	manager.addToGlobalGroupIndex(9001, "second", second)
	adapter := &WSManagerAdapter{manager: manager}

	sent, dropped := adapter.BroadcastToGroups([]int{9001}, []byte{1, 2, 3, 4}, 2, interfaces.WSBroadcastFilter{})
	if sent != 2 || dropped != 0 {
		t.Fatalf("sent=%d dropped=%d", sent, dropped)
	}
	firstReq := <-first.writeCh
	secondReq := <-second.writeCh
	if firstReq.payload != secondReq.payload {
		t.Fatal("devices did not share the same immutable payload")
	}
	if wsFramesCopied.Load() != 1 || wsBytesCopied.Load() != 4 {
		t.Fatalf("frames_copied=%d bytes_copied=%d", wsFramesCopied.Load(), wsBytesCopied.Load())
	}
	firstReq.payload.release()
	secondReq.payload.release()
	wsWritesDrained.Add(2)
	if firstReq.payload.refs.Load() != 0 || firstReq.payload.data != nil {
		t.Fatalf("payload was not released: refs=%d data=%v", firstReq.payload.refs.Load(), firstReq.payload.data)
	}
}

func TestBroadcastToManySlowClientsStaysBoundedAndNonBlocking(t *testing.T) {
	wsFramesCopied.Store(0)
	wsBytesCopied.Store(0)
	wsWritesQueued.Store(0)
	wsWritesDropped.Store(0)
	wsWritesDrained.Store(0)

	const groupID = 9101
	const deviceCount = 2000
	const frameCount = writeChSize + 32
	manager := NewWSConnectionManager()
	devices := make([]*WSDevice, deviceCount)
	for i := range devices {
		device := &WSDevice{
			UserID: i + 1, SSID: 105, GroupID: groupID, DeviceType: DeviceTypeGhost,
			IsOnline: true, writeCh: make(chan *writeRequest, writeChSize),
		}
		devices[i] = device
		manager.addToGlobalGroupIndex(groupID, fmt.Sprintf("slow-%d", i), device)
	}
	adapter := &WSManagerAdapter{manager: manager}
	payload := []byte{1, 2, 3, 4, 5, 6, 7, 8}

	started := time.Now()
	var totalSent, totalDropped int
	for frame := 0; frame < frameCount; frame++ {
		sent, dropped := adapter.BroadcastToGroups([]int{groupID}, payload, 2, interfaces.WSBroadcastFilter{})
		totalSent += sent
		totalDropped += dropped
	}
	elapsed := time.Since(started)
	wantSent := deviceCount * writeChSize
	wantDropped := deviceCount * (frameCount - writeChSize)
	if totalSent != wantSent || totalDropped != wantDropped {
		t.Fatalf("sent=%d dropped=%d, want sent=%d dropped=%d", totalSent, totalDropped, wantSent, wantDropped)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("broadcast to slow clients blocked for %s", elapsed)
	}
	if wsFramesCopied.Load() != frameCount || wsBytesCopied.Load() != frameCount*int64(len(payload)) {
		t.Fatalf("frames_copied=%d bytes_copied=%d", wsFramesCopied.Load(), wsBytesCopied.Load())
	}
	sampleRequest := <-devices[0].writeCh
	devices[0].writeCh <- sampleRequest

	for _, device := range devices {
		if got := len(device.writeCh); got != writeChSize {
			t.Fatalf("slow client queue = %d, want %d", got, writeChSize)
		}
		drainWriteRequests(device.writeCh)
	}
	stats := getWSDeliveryStats()
	if stats["writes_pending"] != 0 || stats["writes_dropped"] != int64(wantDropped) {
		t.Fatalf("delivery stats after drain = %#v", stats)
	}
	if sampleRequest.payload.refs.Load() != 0 || sampleRequest.payload.data != nil {
		t.Fatalf("sample payload was not released: refs=%d data=%v", sampleRequest.payload.refs.Load(), sampleRequest.payload.data)
	}
	t.Logf("slow_clients=%d frames=%d queued=%d dropped=%d copied_frames=%d elapsed=%s", deviceCount, frameCount, totalSent, totalDropped, wsFramesCopied.Load(), elapsed)
}
