package websocket

import (
	"fmt"
	"testing"
	"time"

	"draarl/internal/interfaces"
	"draarl/internal/protocol"
	"github.com/gorilla/websocket"
)

func testVoiceFrame(marker byte) []byte {
	return protocol.EncodeDraARLv1("voice", "", protocol.SSIDGhostWeb, protocol.DraARLTypeOpus16K,
		protocol.DraARLDevModelBrowser, 0, "CALL", []byte{marker})
}

func TestVoiceQueueEvictsOldestVoiceAndKeepsControls(t *testing.T) {
	device := &WSDevice{writeCh: newWriteQueue(3)}
	if !device.AsyncWrite(2, []byte("control-1")) || !device.AsyncWrite(2, testVoiceFrame(1)) ||
		!device.AsyncWrite(2, []byte("control-2")) {
		t.Fatal("failed to fill test queue")
	}
	if !device.AsyncWrite(2, testVoiceFrame(2)) {
		t.Fatal("latest voice frame was rejected from a full queue")
	}
	if got := device.writeCh.length(); got != 3 {
		t.Fatalf("queue length=%d, want 3", got)
	}
	first, ok := device.writeCh.pop()
	if !ok || string(first.payload.data) != "control-1" {
		t.Fatal("first control frame was not preserved")
	}
	second, ok := device.writeCh.pop()
	if !ok || string(second.payload.data) != "control-2" {
		t.Fatal("second control frame was not preserved")
	}
	third, ok := device.writeCh.pop()
	if !ok || len(third.payload.data) <= 48 || third.payload.data[48] != protocol.DraARLTypeOpus16K || third.payload.data[len(third.payload.data)-1] != 2 {
		t.Fatal("latest voice frame was not retained")
	}
	for _, request := range []*writeRequest{first, second, third} {
		request.payload.release()
	}
	if wsWritesEvicted.Load() < 1 {
		t.Fatal("voice eviction was not recorded")
	}
}

func TestVoiceQueueDoesNotEvictControlFrames(t *testing.T) {
	device := &WSDevice{writeCh: newWriteQueue(2)}
	if !device.AsyncWrite(2, []byte("control-1")) || !device.AsyncWrite(2, []byte("control-2")) {
		t.Fatal("failed to fill control queue")
	}
	if device.AsyncWrite(2, testVoiceFrame(3)) {
		t.Fatal("voice frame unexpectedly displaced a full control queue")
	}
	if got := device.writeCh.length(); got != 2 {
		t.Fatalf("queue length=%d, want 2", got)
	}
	drainWriteRequests(device.writeCh)
}

func TestVoiceQueueDoesNotTreatTextPayloadAsVoice(t *testing.T) {
	device := &WSDevice{writeCh: newWriteQueue(2)}
	textPayload := make([]byte, protocol.DraARLv1HeaderSize)
	textPayload[48] = protocol.DraARLTypeOpus16K
	if !device.AsyncWrite(websocket.TextMessage, textPayload) ||
		!device.AsyncWrite(websocket.BinaryMessage, []byte("control")) {
		t.Fatal("failed to fill queue with control frames")
	}
	if device.AsyncWrite(websocket.BinaryMessage, testVoiceFrame(4)) {
		t.Fatal("voice frame displaced a text control payload")
	}
	if got := device.writeCh.length(); got != 2 {
		t.Fatalf("queue length=%d, want 2", got)
	}
	drainWriteRequests(device.writeCh)
}

func TestBroadcastSharesSinglePayloadAcrossDevices(t *testing.T) {
	wsFramesCopied.Store(0)
	wsBytesCopied.Store(0)
	wsWritesQueued.Store(0)
	wsWritesDropped.Store(0)
	wsWritesDrained.Store(0)
	wsWritesEvicted.Store(0)

	manager := NewWSConnectionManager()
	first := &WSDevice{UserID: 1, SSID: 105, GroupID: 9001, DeviceType: DeviceTypeGhost, IsOnline: true, writeCh: newWriteQueue(1)}
	second := &WSDevice{UserID: 2, SSID: 105, GroupID: 9001, DeviceType: DeviceTypeGhost, IsOnline: true, writeCh: newWriteQueue(1)}
	manager.addToGlobalGroupIndex(9001, "first", first)
	manager.addToGlobalGroupIndex(9001, "second", second)
	adapter := &WSManagerAdapter{manager: manager}

	sent, dropped := adapter.BroadcastToGroups([]int{9001}, []byte{1, 2, 3, 4}, 2, interfaces.WSBroadcastFilter{})
	if sent != 2 || dropped != 0 {
		t.Fatalf("sent=%d dropped=%d", sent, dropped)
	}
	firstReq, firstOK := first.writeCh.pop()
	secondReq, secondOK := second.writeCh.pop()
	if !firstOK || !secondOK {
		t.Fatal("expected shared payload requests")
	}
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
	wsWritesEvicted.Store(0)

	const groupID = 9101
	const deviceCount = 2000
	const frameCount = writeChSize + 32
	manager := NewWSConnectionManager()
	devices := make([]*WSDevice, deviceCount)
	for i := range devices {
		device := &WSDevice{
			UserID: i + 1, SSID: 105, GroupID: groupID, DeviceType: DeviceTypeGhost,
			IsOnline: true, writeCh: newWriteQueue(writeChSize),
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
	sampleRequest, ok := devices[0].writeCh.pop()
	if !ok {
		t.Fatal("expected a sample request")
	}
	if accepted, _ := devices[0].writeCh.tryPush(sampleRequest, false); !accepted {
		t.Fatal("failed to restore sample request")
	}

	for _, device := range devices {
		if got := device.writeCh.length(); got != writeChSize {
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
