package udphub

import (
	"net"
	"sync/atomic"

	"draarl/internal/models"
	"draarl/internal/protocol"
)

// 普通设备业务报文（语音/文本/配置）源地址绑定校验指标。
// 心跳路径通过"地址变化触发重认证 + 重新绑定"完成地址迁移，
// 因此仅统计非心跳业务报文的伪造/漂移丢弃。
var (
	devicePacketEndpointRejects atomic.Uint64
)

// GetSecurityPacketMetrics 返回数据面安全校验指标快照。
func GetSecurityPacketMetrics() map[string]uint64 {
	return map[string]uint64{
		"normal_device_endpoint_rejects": devicePacketEndpointRejects.Load(),
	}
}

// enforceNormalDeviceEndpointBinding 校验普通设备业务报文的源地址与设备
// 当前绑定地址一致（与 UDP 幽灵设备路径 sameUDPAddr 校验一致），防止群内
// 成员仅凭报文头 username+ssid 冒名注入语音/文本。心跳包不在此校验：
// 地址变化的心跳会走 AuthenticateDevice 重认证并重新绑定地址。
func enforceNormalDeviceEndpointBinding(dev *models.Device, packet *protocol.DraARLv1Packet, realAddrs ...*net.UDPAddr) bool {
	if dev == nil || packet == nil || packet.Type == protocol.DraARLTypeHeartbeat {
		return true
	}
	state := dev.RuntimeSnapshot()
	boundAddr := state.RealUDPAddr
	if boundAddr == nil {
		boundAddr = state.UDPAddr
	}
	packetAddr := packet.UDPAddr
	if len(realAddrs) > 0 && realAddrs[0] != nil {
		packetAddr = realAddrs[0]
	}
	if sameUDPAddr(boundAddr, packetAddr) {
		return true
	}
	devicePacketEndpointRejects.Add(1)
	return false
}

// normalDeviceConflictAddr preserves the legacy transport-address comparison
// for devices loaded before RealUDPAddr existed. Once a real source has been
// learned, conflict checks use that source while UDPAddr remains the reply
// route through a proxy.
func normalDeviceConflictAddr(dev *models.Device, transportAddr, realAddr *net.UDPAddr) *net.UDPAddr {
	if dev != nil && realAddr != nil && dev.RuntimeSnapshot().RealUDPAddr != nil {
		return realAddr
	}
	return transportAddr
}
