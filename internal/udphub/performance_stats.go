package udphub

// GetUDPPerformanceStats 返回 UDP 热路径各层的累计指标快照。
func GetUDPPerformanceStats() map[string]interface{} {
	stats := map[string]interface{}{
		"fanout":         GetFanoutSenderStats(),
		"ingress":        GetUDPPipelineStats(),
		"receiver_cache": GetDomainReceiverCacheStats(),
		"recording":      GetCommRecordQueueStats(),
		"socket":         getUDPSocketBufferStats(),
	}
	if GlobalMessageRouter != nil && GlobalMessageRouter.wsManager != nil {
		stats["websocket"] = GlobalMessageRouter.wsManager.GetDeliveryStats()
	}
	return stats
}
