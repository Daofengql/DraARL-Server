package db

import (
	"database/sql"
	"log"
	"time"
)

// OperatorLogRepository 操作日志数据访问层
type OperatorLogRepository struct {
	db *sql.DB
}

// NewOperatorLogRepository 创建操作日志仓库
func NewOperatorLogRepository() *OperatorLogRepository {
	return &OperatorLogRepository{db: Get()}
}

// OperatorLog 操作日志模型
type OperatorLog struct {
	ID         int       `json:"id" db:"id"`
	Timestamp  time.Time `json:"timestamp" db:"timestamp"`
	Content    string    `json:"content" db:"content"`
	EventType  string    `json:"event_type" db:"event_type"`
	Operator   string    `json:"operator" db:"operator"`
	OperatorID int       `json:"operator_id" db:"operator_id"`
	Note       string    `json:"note" db:"note"`
}

// AddOperatorLog 添加操作日志
func (r *OperatorLogRepository) AddOperatorLog(content string, eventType string, operator string, operatorID int) error {
	query := "INSERT INTO operator_log (timestamp, content, event_type, operator, operator_id) VALUES (NOW(), ?, ?, ?, ?)"

	_, err := r.db.Exec(query, content, eventType, operator, operatorID)
	if err != nil {
		log.Println("记录日志错误: ", err, "\n", query)
		return err
	}

	return nil
}

// BatchCreate 批量创建操作日志
func (r *OperatorLogRepository) BatchCreate(logs []*OperatorLog) error {
	if len(logs) == 0 {
		return nil
	}

	query := "INSERT INTO operator_log (timestamp, content, event_type, operator, operator_id) VALUES "

	values := make([]interface{}, 0, len(logs)*5)
	for i, log := range logs {
		if i > 0 {
			query += ", "
		}
		query += "(?, ?, ?, ?, ?)"
		values = append(values, log.Timestamp, log.Content, log.EventType, log.Operator, log.OperatorID)
	}

	_, err := r.db.Exec(query, values...)
	if err != nil {
		log.Println("批量插入操作日志错误: ", err)
		return err
	}

	return nil
}

// Query 查询操作日志（分页）
func (r *OperatorLogRepository) Query(userID int, page, limit int, operation string) ([]*OperatorLog, int, error) {
	offset := (page - 1) * limit

	where := "WHERE 1=1"
	args := make([]interface{}, 0, 2)
	if userID > 0 {
		where += " AND operator_id = ?"
		args = append(args, userID)
	}
	if operation != "" {
		where += " AND event_type = ?"
		args = append(args, operation)
	}

	logs := make([]*OperatorLog, 0)
	queryArgs := append(append([]interface{}{}, args...), limit, offset)
	rows, err := r.db.Query(`SELECT id, timestamp, content, event_type, operator, operator_id
		FROM operator_log `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		l := &OperatorLog{}
		if err := rows.Scan(&l.ID, &l.Timestamp, &l.Content, &l.EventType, &l.Operator, &l.OperatorID); err != nil {
			log.Println("select operator_log err:", err)
			continue
		}
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var total int
	if err := r.db.QueryRow("SELECT count(*) as total FROM operator_log "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// GetStats 获取日志统计信息
func (r *OperatorLogRepository) GetStats() (map[string]int64, error) {
	stats := make(map[string]int64)

	// 总数
	var total int64
	err := r.db.QueryRow("SELECT COUNT(*) FROM operator_log").Scan(&total)
	if err != nil {
		return nil, err
	}
	stats["total"] = total

	// 今日统计
	var today int64
	err = r.db.QueryRow("SELECT COUNT(*) FROM operator_log WHERE DATE(timestamp) = CURDATE()").Scan(&today)
	if err != nil {
		return nil, err
	}
	stats["today"] = today

	// 本周统计
	var week int64
	err = r.db.QueryRow("SELECT COUNT(*) FROM operator_log WHERE YEARWEEK(timestamp, 1) = YEARWEEK(NOW(), 1)").Scan(&week)
	if err != nil {
		return nil, err
	}
	stats["this_week"] = week

	// 本月统计
	var month int64
	err = r.db.QueryRow("SELECT COUNT(*) FROM operator_log WHERE YEAR(timestamp) = YEAR(NOW()) AND MONTH(timestamp) = MONTH(NOW())").Scan(&month)
	if err != nil {
		return nil, err
	}
	stats["this_month"] = month

	return stats, nil
}
