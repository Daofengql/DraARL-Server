package log

import (
	"context"
	"log"
	"sync"
	"time"
)

// OperatorLog 操作日志
type OperatorLog struct {
	ID        int       `json:"id"`
	UserID    int       `json:"user_id"`
	UserName  string    `json:"user_name"`
	CallSign  string    `json:"callsign"`
	Content   string    `json:"content"`
	Operation string    `json:"operation"`
	IPAddress string    `json:"ip_address"`
	CreateTime time.Time `json:"create_time"`
}

var (
	logBuffer = make(chan *OperatorLog, 100)
	logOnce   sync.Once
)

// Start 启动日志处理器
func Start() {
	logOnce.Do(func() {
		go processLogBuffer()
		log.Println("Operator log processor started")
	})
}

// AddLog 添加操作日志
func AddLog(content, operation string, userID int, userName, callSign, ipAddress string) {
	logEntry := &OperatorLog{
		UserID:    userID,
		UserName:  userName,
		CallSign:  callSign,
		Content:   content,
		Operation: operation,
		IPAddress: ipAddress,
		CreateTime: time.Now(),
	}

	select {
	case logBuffer <- logEntry:
	default:
		// 缓冲区满时直接写入数据库
		writeLog(logEntry)
	}
}

// AddLogWithContext 添加操作日志（带上下文）
func AddLogWithContext(ctx context.Context, content, operation string, userID int) {
	// TODO: 从上下文获取用户信息
	AddLog(content, operation, userID, "", "", "")
}

// AddOperatorLog 添加操作日志（兼容函数）
func AddOperatorLog(content, operation string, user interface{}) {
	// TODO: 解析用户信息
	// 目前简化处理
	AddLog(content, operation, 0, "", "", "")
}

// processLogBuffer 处理日志缓冲区
func processLogBuffer() {
	batch := make([]*OperatorLog, 0, 50)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case logEntry := <-logBuffer:
			batch = append(batch, logEntry)
			if len(batch) >= 50 {
				writeBatch(batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			if len(batch) > 0 {
				writeBatch(batch)
				batch = batch[:0]
			}
		}
	}
}

// writeBatch 批量写入日志到数据库
func writeBatch(logs []*OperatorLog) {
	if len(logs) == 0 {
		return
	}

	// TODO: 批量写入数据库
	// repo := db.NewOperatorLogRepository()
	// if err := repo.BatchCreate(logs); err != nil {
	// 	log.Printf("Write operator log batch failed: %v", err)
	// }

	for _, logEntry := range logs {
		log.Printf("[OPERATOR] %s - %s: %s", logEntry.CreateTime.Format("2006-01-02 15:04:05"), logEntry.Operation, logEntry.Content)
	}
}

// writeLog 写入单条日志到数据库
func writeLog(logEntry *OperatorLog) {
	// TODO: 写入数据库
	// repo := db.NewOperatorLogRepository()
	// if err := repo.Create(logEntry); err != nil {
	// 	log.Printf("Write operator log failed: %v", err)
	// }

	log.Printf("[OPERATOR] %s - %s: %s", logEntry.CreateTime.Format("2006-01-02 15:04:05"), logEntry.Operation, logEntry.Content)
}

// QueryLogs 查询操作日志
func QueryLogs(userID int, page, limit int, operation string) ([]*OperatorLog, int, error) {
	// TODO: 从数据库查询
	// repo := db.NewOperatorLogRepository()
	// return repo.Query(userID, page, limit, operation)

	return []*OperatorLog{}, 0, nil
}

// GetUserLogs 获取用户操作日志
func GetUserLogs(userID int, limit int) ([]*OperatorLog, error) {
	logs, _, err := QueryLogs(userID, 1, limit, "")
	return logs, err
}

// GetRecentLogs 获取最近的操作日志
func GetRecentLogs(limit int) ([]*OperatorLog, error) {
	logs, _, err := QueryLogs(0, 1, limit, "")
	return logs, err
}

// Flush 刷新日志缓冲区
func Flush() {
	batch := make([]*OperatorLog, 0, 100)

	for {
		select {
		case logEntry := <-logBuffer:
			batch = append(batch, logEntry)
		default:
			if len(batch) > 0 {
				writeBatch(batch)
			}
			return
		}
	}
}

// GetStats 获取日志统计信息
func GetStats() (map[string]int64, error) {
	// TODO: 从数据库统计
	stats := map[string]int64{
		"total":      0,
		"today":      0,
		"this_week":  0,
		"this_month": 0,
	}
	return stats, nil
}
