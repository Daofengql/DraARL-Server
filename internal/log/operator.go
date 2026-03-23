package log

import (
	"log"
	"sync"
	"time"

	"draarl/internal/gormdb"
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
	repo      *gormdb.OperatorLogRepository
)

// Start 启动日志处理器
func Start() {
	logOnce.Do(func() {
		repo = gormdb.NewOperatorLogRepository()
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
func AddLogWithContext(userID int, content, operation string) {
	// TODO: 从上下文获取用户信息
	AddLog(content, operation, userID, "", "", "")
}

// AddOperatorLog 添加操作日志（兼容函数）
func AddOperatorLog(content, operation string, user interface{}) {
	// 兼容旧代码：user 可能是 *models.User 或其他类型
	// 目前简化处理
	AddLog(content, operation, 0, "", "", "")
}

// processLogBuffer 处理日志缓冲区
func processLogBuffer() {
	batch := make([]*gormdb.OperatorLog, 0, 50)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case logEntry := <-logBuffer:
			// 转换为数据库模型
			dbLog := &gormdb.OperatorLog{
				Content:    logEntry.Content,
				EventType:  logEntry.Operation,
				Operator:   logEntry.UserName + "-" + logEntry.CallSign,
				OperatorID: logEntry.UserID,
				Timestamp:  logEntry.CreateTime,
			}
			batch = append(batch, dbLog)
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
func writeBatch(logs []*gormdb.OperatorLog) {
	if len(logs) == 0 {
		return
	}

	if err := repo.BatchCreate(logs); err != nil {
		log.Printf("Write operator log batch failed: %v", err)
	}
	// 审计日志只写入数据库，不在命令行打印
}

// writeLog 写入单条日志到数据库
func writeLog(logEntry *OperatorLog) {
	dbLog := &gormdb.OperatorLog{
		Content:    logEntry.Content,
		EventType:  logEntry.Operation,
		Operator:   logEntry.UserName + "-" + logEntry.CallSign,
		OperatorID: logEntry.UserID,
		Timestamp:  logEntry.CreateTime,
	}

	if err := repo.AddOperatorLog(dbLog.Content, dbLog.EventType, dbLog.Operator, dbLog.OperatorID); err != nil {
		log.Printf("Write operator log failed: %v", err)
	}
	// 审计日志只写入数据库，不在命令行打印
}

// QueryLogs 查询操作日志
func QueryLogs(userID int, page, limit int, operation string) ([]*OperatorLog, int64, error) {
	dbLogs, total, err := repo.Query(userID, page, limit, operation)
	if err != nil {
		return nil, 0, err
	}

	logs := make([]*OperatorLog, 0, len(dbLogs))
	for _, dbLog := range dbLogs {
		log := &OperatorLog{
			ID:         dbLog.ID,
			Content:    dbLog.Content,
			Operation:  dbLog.EventType,
			UserID:     dbLog.OperatorID,
			CreateTime: dbLog.Timestamp,
		}
		logs = append(logs, log)
	}

	return logs, total, nil
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
	batch := make([]*gormdb.OperatorLog, 0, 100)

	for {
		select {
		case logEntry := <-logBuffer:
			dbLog := &gormdb.OperatorLog{
				Content:    logEntry.Content,
				EventType:  logEntry.Operation,
				Operator:   logEntry.UserName + "-" + logEntry.CallSign,
				OperatorID: logEntry.UserID,
				Timestamp:  logEntry.CreateTime,
			}
			batch = append(batch, dbLog)
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
	return repo.GetStats()
}
