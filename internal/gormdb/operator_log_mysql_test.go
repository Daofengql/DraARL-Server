package gormdb

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
)

func TestOperatorLogIndexesAndQueriesMySQL(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("DRAARL_OPERATOR_LOG_E2E")), "true") {
		t.Skip("set DRAARL_OPERATOR_LOG_E2E=true and DRAARL_TEST_MYSQL_DSN to run operator log E2E")
	}
	parsed, err := drivermysql.ParseDSN(strings.TrimSpace(os.Getenv("DRAARL_TEST_MYSQL_DSN")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(parsed.DBName, "draarl_test_") {
		t.Fatalf("refusing non-test database %q", parsed.DBName)
	}
	parsed.ParseTime = true
	if err := Init(&Config{DSN: parsed.FormatDSN(), MaxOpenConns: 10, MaxIdleConns: 2, MaxLifetime: 60, LogLevel: "silent"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })
	db := Get()
	if err := db.AutoMigrate(&OperatorLog{}); err != nil {
		t.Fatal(err)
	}

	currentDB := ""
	if err := db.Raw("SELECT DATABASE()").Scan(&currentDB).Error; err != nil {
		t.Fatal(err)
	}
	assertIndexColumns := func(name string, want []string) {
		t.Helper()
		var got []string
		if err := db.Raw(`
			SELECT column_name
			FROM information_schema.statistics
			WHERE table_schema = ? AND table_name = 'operator_log' AND index_name = ?
			ORDER BY seq_in_index
		`, currentDB, name).Scan(&got).Error; err != nil {
			t.Fatal(err)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("index %s columns=%v want=%v", name, got, want)
		}
	}
	assertIndexColumns("idx_operator_log_event_id", []string{"event_type", "id"})
	assertIndexColumns("idx_operator_log_operator_id", []string{"operator_id", "id"})

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	eventType := "operator-log-e2e-" + suffix
	operatorID := int(time.Now().UnixNano() % 1000000)
	base := time.Now().Add(-time.Minute)
	logs := []*OperatorLog{
		{Timestamp: base, Content: "first", EventType: eventType, Operator: "e2e", OperatorID: operatorID},
		{Timestamp: base.Add(time.Second), Content: "second", EventType: eventType, Operator: "e2e", OperatorID: operatorID},
		{Timestamp: base.Add(2 * time.Second), Content: "other", EventType: "other-" + suffix, Operator: "e2e", OperatorID: operatorID + 1},
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Delete(&OperatorLog{}, []int{logs[0].ID, logs[1].ID, logs[2].ID}).Error })

	repo := NewOperatorLogRepository()
	got, total, err := repo.ListLogsByEventType(eventType, 10, 1)
	if err != nil || total != 2 || len(got) != 2 || got[0].ID != logs[1].ID || got[1].ID != logs[0].ID {
		t.Fatalf("event query len=%d total=%d ids=%v err=%v", len(got), total, idsOfOperatorLogs(got), err)
	}
	got, total, err = repo.ListLogsByOperator(operatorID, 10, 1)
	if err != nil || total != 2 || len(got) != 2 {
		t.Fatalf("operator query len=%d total=%d err=%v", len(got), total, err)
	}

	stats, err := repo.GetLogStats()
	if err != nil || stats[eventType] != 2 || stats["total"] < 3 {
		t.Fatalf("stats=%v err=%v", stats, err)
	}

	var planRows []struct {
		Key string `gorm:"column:key"`
	}
	if err := db.Raw("EXPLAIN SELECT id FROM operator_log WHERE event_type = ? ORDER BY id DESC LIMIT 20", eventType).Scan(&planRows).Error; err != nil {
		t.Fatal(err)
	}
	if len(planRows) == 0 || !strings.Contains(planRows[0].Key, "idx_operator_log_event_id") {
		t.Fatalf("event query plan=%v", planRows)
	}
	if err := db.Raw("EXPLAIN SELECT id FROM operator_log WHERE operator_id = ? ORDER BY id DESC LIMIT 20", operatorID).Scan(&planRows).Error; err != nil {
		t.Fatal(err)
	}
	if len(planRows) == 0 || !strings.Contains(planRows[0].Key, "idx_operator_log_operator_id") {
		t.Fatalf("operator query plan=%v", planRows)
	}

	if _, _, _, err := NormalizeOperatorLogPagination(100, int(^uint(0)>>1)); err == nil {
		t.Fatal("repository pagination overflow was accepted")
	}
}

func idsOfOperatorLogs(logs []*OperatorLog) []int {
	ids := make([]int, 0, len(logs))
	for _, log := range logs {
		if log != nil {
			ids = append(ids, log.ID)
		}
	}
	return ids
}
