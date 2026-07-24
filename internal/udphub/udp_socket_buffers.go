package udphub

import (
	"log"
	"net"
	"sync/atomic"

	"draarl/internal/config"
)

var (
	udpRequestedReadBuffer  int64
	udpRequestedWriteBuffer int64
	udpActualReadBuffer     int64
	udpActualWriteBuffer    int64
)

func configureUDPSocketBuffers(conn *net.UDPConn) {
	if conn == nil {
		return
	}
	readSize := 4 * 1024 * 1024
	writeSize := 4 * 1024 * 1024
	if cfg := config.TryGet(); cfg != nil {
		if cfg.UDP.ReadBufferBytes > 0 {
			readSize = cfg.UDP.ReadBufferBytes
		}
		if cfg.UDP.WriteBufferBytes > 0 {
			writeSize = cfg.UDP.WriteBufferBytes
		}
	}
	atomic.StoreInt64(&udpRequestedReadBuffer, int64(readSize))
	atomic.StoreInt64(&udpRequestedWriteBuffer, int64(writeSize))
	if err := conn.SetReadBuffer(readSize); err != nil {
		log.Printf("[UDP] set read buffer to %d failed: %v", readSize, err)
	}
	if err := conn.SetWriteBuffer(writeSize); err != nil {
		log.Printf("[UDP] set write buffer to %d failed: %v", writeSize, err)
	}

	actualRead, actualWrite, err := readUDPSocketBufferSizes(conn)
	if err != nil {
		log.Printf("[UDP] read actual socket buffer sizes failed: %v", err)
		return
	}
	atomic.StoreInt64(&udpActualReadBuffer, int64(actualRead))
	atomic.StoreInt64(&udpActualWriteBuffer, int64(actualWrite))
	log.Printf("[UDP] socket buffers: read=%d/%d write=%d/%d (actual/requested)",
		actualRead, readSize, actualWrite, writeSize)
}

func getUDPSocketBufferStats() map[string]int64 {
	return map[string]int64{
		"read_requested":  atomic.LoadInt64(&udpRequestedReadBuffer),
		"read_actual":     atomic.LoadInt64(&udpActualReadBuffer),
		"write_requested": atomic.LoadInt64(&udpRequestedWriteBuffer),
		"write_actual":    atomic.LoadInt64(&udpActualWriteBuffer),
	}
}
