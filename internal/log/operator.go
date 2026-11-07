package log

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"draarl/internal/gormdb"
)

const (
	operatorLogQueueSize     = 100
	operatorLogBatchSize     = 50
	operatorLogFlushInterval = 5 * time.Second
	operatorLogWriteTimeout  = 5 * time.Second
)

// OperatorLog 操作日志
type OperatorLog struct {
	ID         int       `json:"id"`
	UserID     int       `json:"user_id"`
	UserName   string    `json:"user_name"`
	CallSign   string    `json:"callsign"`
	Content    string    `json:"content"`
	Operation  string    `json:"operation"`
	IPAddress  string    `json:"ip_address"`
	CreateTime time.Time `json:"create_time"`
}

var (
	logBuffer = make(chan *OperatorLog, operatorLogQueueSize)
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
		UserID:     userID,
		UserName:   userName,
		CallSign:   callSign,
		Content:    content,
		Operation:  operation,
		IPAddress:  ipAddress,
		CreateTime: time.Now(),
	}

	select {
	case logBuffer <- logEntry:
	default:
		// 缓冲区满时直接写入数据库
		writeLog(logEntry)
	}
}

// processLogBuffer 处理日志缓冲区
func processLogBuffer() {
	batch := make([]*gormdb.OperatorLog, 0, operatorLogBatchSize)
	ticker := time.NewTicker(operatorLogFlushInterval)
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
				IPAddress:  logEntry.IPAddress,
			}
			batch = append(batch, dbLog)
			if len(batch) >= operatorLogBatchSize {
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
	if len(logs) == 0 || repo == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), operatorLogWriteTimeout)
	defer cancel()
	if err := repo.WithContext(ctx).BatchCreate(logs); err != nil {
		log.Printf("Write operator log batch failed: %v", err)
	}
	// 审计日志只写入数据库，不在命令行打印
}

// writeLog 写入单条日志到数据库
func writeLog(logEntry *OperatorLog) {
	if repo == nil || logEntry == nil {
		return
	}
	dbLog := &gormdb.OperatorLog{
		Content:    logEntry.Content,
		EventType:  logEntry.Operation,
		Operator:   logEntry.UserName + "-" + logEntry.CallSign,
		OperatorID: logEntry.UserID,
		Timestamp:  logEntry.CreateTime,
		IPAddress:  logEntry.IPAddress,
	}

	ctx, cancel := context.WithTimeout(context.Background(), operatorLogWriteTimeout)
	defer cancel()
	if err := repo.WithContext(ctx).CreateLog(dbLog); err != nil {
		log.Printf("Write operator log failed: %v", err)
	}
	// 审计日志只写入数据库，不在命令行打印
}

// QueryLogs 查询操作日志
func QueryLogs(userID int, page, limit int, operation string) ([]*OperatorLog, int64, error) {
	if repo == nil {
		return nil, 0, errors.New("operator log processor is not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), operatorLogWriteTimeout)
	defer cancel()
	dbLogs, total, err := repo.WithContext(ctx).Query(userID, page, limit, operation)
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
			IPAddress:  dbLog.IPAddress,
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
				IPAddress:  logEntry.IPAddress,
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
	if repo == nil {
		return nil, errors.New("operator log processor is not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), operatorLogWriteTimeout)
	defer cancel()
	return repo.WithContext(ctx).GetStats()
}
