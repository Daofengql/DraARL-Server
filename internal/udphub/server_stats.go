package udphub

import (
	"net"
	"sync"
	"sync/atomic"

	"draarl/internal/models"
)

// GetGlobalConn 获取全局 UDP 连接
func GetGlobalConn() *net.UDPConn {
	return globalConn
}

// GetTotalStats 获取服务器统计信息（返回快照副本，避免并发读写）
func GetTotalStats() *models.ServerStats {
	return &models.ServerStats{
		PacketNumber:    atomic.LoadInt64(&totalStats.PacketNumber),
		VoiceTime:       atomic.LoadInt64(&totalStats.VoiceTime),
		Traffic:         atomic.LoadInt64(&totalStats.Traffic),
		OnlineDevNumber: int(atomic.LoadInt64(&totalStatsOnline)),
	}
}

func atomicAddPacketNumber(n int64) {
	atomic.AddInt64(&totalStats.PacketNumber, n)
}

func atomicAddTraffic(n int64) {
	atomic.AddInt64(&totalStats.Traffic, n)
}

func atomicAddVoiceTime(n int64) {
	atomic.AddInt64(&totalStats.VoiceTime, n)
}

func setOnlineDevNumber(n int) {
	atomic.StoreInt64(&totalStatsOnline, int64(n))
}

func getOnlineDevNumber() int {
	return int(atomic.LoadInt64(&totalStatsOnline))
}

// GetUserList 获取用户列表
func GetUserList() *sync.Map {
	return &userList
}

// GetPublicGroupMap 获取公共群组映射
func GetPublicGroupMap() map[int]*models.Group {
	return GetAllGroupsFromCache()
}
