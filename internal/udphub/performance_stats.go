package udphub

import "draarl/internal/ghostsession"

// GetUDPPerformanceStats 返回 UDP 热路径各层的累计指标快照。
func GetUDPPerformanceStats() map[string]interface{} {
	stats := map[string]interface{}{
		"fanout":         GetFanoutSenderStats(),
		"ingress":        GetUDPPipelineStats(),
		"receiver_cache": GetDomainReceiverCacheStats(),
		"recording":      GetCommRecordQueueStats(),
		"socket":         getUDPSocketBufferStats(),
		"ghost_sessions": ghostsession.Global.Metrics(),
		"ghost_packets":  GetGhostPacketMetrics(),
	}
	if GlobalMessageRouter != nil && GlobalMessageRouter.wsManager != nil {
		stats["websocket"] = GlobalMessageRouter.wsManager.GetDeliveryStats()
	}
	return stats
}
