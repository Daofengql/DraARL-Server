package udphub

import (
	"net"
	"testing"

	"draarl/internal/models"
	"draarl/internal/protocol"
)

func TestEnforceNormalDeviceEndpointBinding(t *testing.T) {
	addrA := &net.UDPAddr{IP: net.IPv4(192, 168, 1, 10), Port: 40000}
	addrB := &net.UDPAddr{IP: net.IPv4(192, 168, 1, 10), Port: 40001}
	addrC := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 99), Port: 50000}

	dev := &models.Device{UDPAddr: addrA}

	textPacket := &protocol.DraARLv1Packet{Type: protocol.DraARLTypeTextMessage, UDPAddr: addrA}
	if !enforceNormalDeviceEndpointBinding(dev, textPacket, addrA) {
		t.Fatal("same endpoint text packet should pass")
	}

	voicePacket := &protocol.DraARLv1Packet{Type: protocol.DraARLTypeOpus16K, UDPAddr: addrC}
	if enforceNormalDeviceEndpointBinding(dev, voicePacket, addrC) {
		t.Fatal("different IP voice packet must be rejected")
	}

	textPacketB := &protocol.DraARLv1Packet{Type: protocol.DraARLTypeTextMessage, UDPAddr: addrB}
	if enforceNormalDeviceEndpointBinding(dev, textPacketB, addrB) {
		t.Fatal("different port text packet must be rejected")
	}

	configPacket := &protocol.DraARLv1Packet{Type: protocol.DraARLTypeConfig, UDPAddr: addrC}
	if enforceNormalDeviceEndpointBinding(dev, configPacket, addrC) {
		t.Fatal("different IP config packet must be rejected")
	}

	// PROXY v2 keeps the proxy transport address for replies but binds identity
	// to the advertised real source; two clients sharing one proxy must not
	// share the normal-device endpoint binding.
	proxyAddr := &net.UDPAddr{IP: net.IPv4(203, 0, 113, 10), Port: 60050}
	realAddrA := &net.UDPAddr{IP: net.IPv4(198, 51, 100, 10), Port: 41000}
	realAddrB := &net.UDPAddr{IP: net.IPv4(198, 51, 100, 11), Port: 41001}
	proxied := &models.Device{UDPAddr: proxyAddr, RealUDPAddr: realAddrA}
	proxiedPacket := &protocol.DraARLv1Packet{Type: protocol.DraARLTypeOpus16K, UDPAddr: proxyAddr}
	if !enforceNormalDeviceEndpointBinding(proxied, proxiedPacket, realAddrA) {
		t.Fatal("same PROXY real endpoint should pass")
	}
	if enforceNormalDeviceEndpointBinding(proxied, proxiedPacket, realAddrB) {
		t.Fatal("different PROXY real endpoint must be rejected even with shared transport")
	}
	legacy := &models.Device{UDPAddr: proxyAddr}
	if got := normalDeviceConflictAddr(legacy, proxyAddr, realAddrB); !sameUDPAddr(got, proxyAddr) {
		t.Fatalf("legacy conflict endpoint=%v, want transport %v", got, proxyAddr)
	}
	if got := normalDeviceConflictAddr(proxied, proxyAddr, realAddrB); !sameUDPAddr(got, realAddrB) {
		t.Fatalf("learned conflict endpoint=%v, want real %v", got, realAddrB)
	}

	// 心跳包不受绑定校验约束（地址变化由重认证路径处理）
	heartbeat := &protocol.DraARLv1Packet{Type: protocol.DraARLTypeHeartbeat, UDPAddr: addrC}
	if !enforceNormalDeviceEndpointBinding(dev, heartbeat, addrC) {
		t.Fatal("heartbeat must bypass endpoint binding check")
	}

	// 未上线设备（UDPAddr 为空）不允许转发业务报文
	offline := &models.Device{}
	if enforceNormalDeviceEndpointBinding(offline, &protocol.DraARLv1Packet{Type: protocol.DraARLTypeTextMessage, UDPAddr: addrA}, addrA) {
		t.Fatal("device without bound address must not forward business packets")
	}

	// 防御性：nil 入参放行（不 panic）
	if !enforceNormalDeviceEndpointBinding(nil, textPacket, addrA) {
		t.Fatal("nil device should pass defensively")
	}
	if !enforceNormalDeviceEndpointBinding(dev, nil, addrA) {
		t.Fatal("nil packet should pass defensively")
	}
}

func TestProcessPacketBindsSharedProxyDeviceToRealEndpoint(t *testing.T) {
	proxyAddr := &net.UDPAddr{IP: net.IPv4(203, 0, 113, 10), Port: 60050}
	realAddrA := &net.UDPAddr{IP: net.IPv4(198, 51, 100, 10), Port: 41000}
	realAddrB := &net.UDPAddr{IP: net.IPv4(198, 51, 100, 11), Port: 41001}
	device := &models.Device{
		ID: 1, OwnerID: 1, Username: "alice", CallSign: "BG7S1", SSID: 1,
		ISOnline: true, CurrentEntryNodeID: "center", UDPAddr: proxyAddr, RealUDPAddr: realAddrA,
	}
	runtimeIndexMu.Lock()
	oldUsernameMap := devUsernameSSIDMap
	devUsernameSSIDMap = map[string]*models.Device{usernameSSIDKey(device.Username, device.SSID): device}
	runtimeIndexMu.Unlock()
	t.Cleanup(func() {
		runtimeIndexMu.Lock()
		devUsernameSSIDMap = oldUsernameMap
		runtimeIndexMu.Unlock()
	})

	wire := protocol.EncodeDraARLv1(
		device.Username, "", device.SSID, protocol.DraARLTypeOpus16K,
		protocol.DraARLDevModelESP32NoRadio, 0, device.CallSign, []byte{1, 2, 3},
	)
	processDraARLPacket(wire, proxyAddr, realAddrB, nil)
	if got := device.RuntimeSnapshot().Traffic; got != 0 {
		t.Fatalf("forged shared-proxy packet updated traffic=%d", got)
	}
	processDraARLPacket(wire, proxyAddr, realAddrA, nil)
	if got := device.RuntimeSnapshot().Traffic; got == 0 {
		t.Fatal("packet from bound real endpoint was not processed")
	}
}
