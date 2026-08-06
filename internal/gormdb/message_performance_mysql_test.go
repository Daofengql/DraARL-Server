package gormdb

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

const messagePerformanceE2EEnv = "DRAARL_MESSAGE_PERFORMANCE_E2E"

func TestMillionRowMessageCursorPlanMySQL(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv(messagePerformanceE2EEnv)), "true") {
		t.Skip("set " + messagePerformanceE2EEnv + "=true and DRAARL_TEST_MYSQL_DSN to run the million-row message query test")
	}
	parsed, err := drivermysql.ParseDSN(strings.TrimSpace(os.Getenv("DRAARL_TEST_MYSQL_DSN")))
	if err != nil {
		t.Fatalf("parse MySQL DSN: %v", err)
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
	if err := db.AutoMigrate(&User{}, &Group{}, &Device{}, &CommRecord{}); err != nil {
		t.Fatal(err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	user := &User{
		Name: "message-perf-" + suffix, Email: "message-perf-" + suffix + "@example.invalid",
		CallSign: "MP" + suffix[len(suffix)-8:], Status: 1, ApprovalStatus: 1,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	groups := make([]*Group, 4)
	for i := range groups {
		groups[i] = &Group{Name: fmt.Sprintf("message-perf-%d-%s", i, suffix), Type: 1, OwerID: user.ID, Status: 1}
		if err := db.Create(groups[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		ids := make([]int, len(groups))
		for i := range groups {
			ids[i] = groups[i].ID
		}
		_ = db.Delete(&Group{}, ids).Error
		_ = db.Delete(&User{}, user.ID).Error
	})

	tx := db.Begin(&sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	for i := 0; i < 6; i++ {
		table := fmt.Sprintf("message_perf_digits_%d", i)
		if err := tx.Exec("CREATE TEMPORARY TABLE " + table + " (n TINYINT UNSIGNED PRIMARY KEY)").Error; err != nil {
			t.Fatal(err)
		}
		if err := tx.Exec("INSERT INTO " + table + " (n) VALUES (0),(1),(2),(3),(4),(5),(6),(7),(8),(9)").Error; err != nil {
			t.Fatal(err)
		}
	}
	groupIDs := []int{groups[0].ID, groups[1].ID, groups[2].ID, groups[3].ID}
	insertStarted := time.Now()
	insertSQL := `
		INSERT INTO comm_records (
			device_id, device_ssid, group_id, user_id, start_time, end_time, duration_ms,
			audio_path, audio_size, status, message_type, text_content,
			sender_username, sender_callsign, sender_nickname, sender_dev_model, created_at
		)
		SELECT 0, 101,
			CASE MOD(seq.n, 4) WHEN 0 THEN ? WHEN 1 THEN ? WHEN 2 THEN ? ELSE ? END,
			?, TIMESTAMPADD(MICROSECOND, seq.n * 1000, '2026-01-01 00:00:00'),
			TIMESTAMPADD(MICROSECOND, seq.n * 1000, '2026-01-01 00:00:00'), 0,
			'', 0, 2, MOD(seq.n, 2), IF(MOD(seq.n, 2) = 1, CONCAT('message-', seq.n), ''),
			'message-perf', 'BG5PERF', 'Message Performance', 101, NOW(3)
		FROM (
			SELECT d0.n + d1.n * 10 + d2.n * 100 + d3.n * 1000 + d4.n * 10000 + d5.n * 100000 AS n
			FROM message_perf_digits_0 d0
			CROSS JOIN message_perf_digits_1 d1
			CROSS JOIN message_perf_digits_2 d2
			CROSS JOIN message_perf_digits_3 d3
			CROSS JOIN message_perf_digits_4 d4
			CROSS JOIN message_perf_digits_5 d5
		) seq`
	if err := tx.Exec(insertSQL, groupIDs[0], groupIDs[1], groupIDs[2], groupIDs[3], user.ID).Error; err != nil {
		t.Fatal(err)
	}
	var rowCount int64
	if err := tx.Model(&CommRecord{}).Where("user_id = ?", user.ID).Count(&rowCount).Error; err != nil || rowCount != 1_000_000 {
		t.Fatalf("performance fixture rows=%d err=%v", rowCount, err)
	}
	t.Logf("inserted %d communication records in %s", rowCount, time.Since(insertStarted))

	before := time.Date(2026, time.January, 1, 0, 15, 0, 0, time.UTC)
	plan := explainMessageCursorPlan(t, tx, groupIDs[0], before)
	if !strings.Contains(plan, "idx_comm_records_group_status_start_id") {
		t.Fatalf("message cursor query did not use the compound index:\n%s", plan)
	}
	if strings.Contains(strings.ToLower(plan), "table scan on cr") {
		t.Fatalf("message cursor query used a table scan:\n%s", plan)
	}
	t.Logf("single-group EXPLAIN ANALYZE:\n%s", plan)

	queryStarted := time.Now()
	records, hasMore, err := (&MessageRepository{db: tx}).List(MessageQuery{
		GroupIDs: groupIDs, BeforeTime: &before, BeforeID: ^uint(0), Limit: 50,
	})
	queryElapsed := time.Since(queryStarted)
	if err != nil || len(records) != 50 || !hasMore {
		t.Fatalf("linked message cursor result len=%d has_more=%v elapsed=%s err=%v", len(records), hasMore, queryElapsed, err)
	}
	for i := 1; i < len(records); i++ {
		if records[i-1].StartTime.Before(records[i].StartTime) ||
			(records[i-1].StartTime.Equal(records[i].StartTime) && records[i-1].ID < records[i].ID) {
			t.Fatalf("linked message results are not cursor ordered at %d", i)
		}
	}
	if queryElapsed > 5*time.Second {
		t.Fatalf("linked message cursor query took %s for four groups", queryElapsed)
	}
	t.Logf("four-group cursor fetched 50 rows from 1,000,000 in %s", queryElapsed)

	typePlan := explainMessageTypeCursorPlan(t, tx, groupIDs[0], before, CommMessageTypeText)
	if !strings.Contains(typePlan, "idx_comm_records_group_status_type_start_id") {
		t.Fatalf("typed message cursor query did not use the compound index:\n%s", typePlan)
	}
	if strings.Contains(strings.ToLower(typePlan), "table scan on cr") {
		t.Fatalf("typed message cursor query used a table scan:\n%s", typePlan)
	}
	t.Logf("typed single-group EXPLAIN ANALYZE:\n%s", typePlan)

	messageType := CommMessageTypeText
	typedQueryStarted := time.Now()
	typedRecords, typedHasMore, err := (&MessageRepository{db: tx}).List(MessageQuery{
		GroupIDs: groupIDs, Type: &messageType, BeforeTime: &before, BeforeID: ^uint(0), Limit: 50,
	})
	typedQueryElapsed := time.Since(typedQueryStarted)
	if err != nil || len(typedRecords) != 50 || !typedHasMore {
		t.Fatalf("typed linked message cursor result len=%d has_more=%v elapsed=%s err=%v", len(typedRecords), typedHasMore, typedQueryElapsed, err)
	}
	for _, record := range typedRecords {
		if record.MessageType != messageType {
			t.Fatalf("typed linked message cursor returned message type %d", record.MessageType)
		}
	}
	if typedQueryElapsed > 5*time.Second {
		t.Fatalf("typed linked message cursor query took %s for four groups", typedQueryElapsed)
	}
	t.Logf("typed four-group cursor fetched 50 rows from 1,000,000 in %s", typedQueryElapsed)
}

func explainMessageCursorPlan(t *testing.T, db *gorm.DB, groupID int, before time.Time) string {
	t.Helper()
	rows, err := db.Raw(`
		EXPLAIN ANALYZE
		SELECT cr.id, cr.start_time
		FROM comm_records cr FORCE INDEX (idx_comm_records_group_status_start_id)
		WHERE cr.status = 2 AND cr.group_id = ?
			AND cr.start_time < ?
		ORDER BY cr.start_time DESC, cr.id DESC
		LIMIT 51
	`, groupID, before).Rows()
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}

func explainMessageTypeCursorPlan(t *testing.T, db *gorm.DB, groupID int, before time.Time, messageType uint8) string {
	t.Helper()
	rows, err := db.Raw(`
		EXPLAIN ANALYZE
		SELECT cr.id, cr.start_time, cr.message_type
		FROM comm_records cr FORCE INDEX (idx_comm_records_group_status_type_start_id)
		WHERE cr.status = 2 AND cr.group_id = ? AND cr.message_type = ?
			AND cr.start_time < ?
		ORDER BY cr.start_time DESC, cr.id DESC
		LIMIT 51
	`, groupID, messageType, before).Rows()
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}
