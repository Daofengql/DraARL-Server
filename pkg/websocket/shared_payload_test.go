package websocket

import (
	"testing"

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
