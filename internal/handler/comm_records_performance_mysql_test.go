package handler

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"draarl/internal/gormdb"

	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

const commRecordsPerformanceE2EEnv = "DRAARL_COMM_RECORDS_PERFORMANCE_E2E"

func TestCommRecordsGlobalPaginationPerformanceMySQL(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv(commRecordsPerformanceE2EEnv)), "true") {
		t.Skip("set " + commRecordsPerformanceE2EEnv + "=true and DRAARL_TEST_MYSQL_DSN to run the 120k-row pagination test")
	}
	parsed, err := drivermysql.ParseDSN(strings.TrimSpace(os.Getenv("DRAARL_TEST_MYSQL_DSN")))
	if err != nil {
		t.Fatalf("parse MySQL DSN: %v", err)
	}
	if !strings.HasPrefix(parsed.DBName, "draarl_test_") {
		t.Fatalf("refusing non-test database %q", parsed.DBName)
	}
	parsed.ParseTime = true
	if err := gormdb.Init(&gormdb.Config{DSN: parsed.FormatDSN(), MaxOpenConns: 10, MaxIdleConns: 2, MaxLifetime: 60, LogLevel: "silent"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gormdb.Close() })
	db := gormdb.Get()
	if err := db.AutoMigrate(&gormdb.User{}, &gormdb.Group{}, &gormdb.CommRecord{}); err != nil {
		t.Fatal(err)
	}

	tx := db.Begin(&sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	user := &gormdb.User{Name: "comm-page-perf-" + suffix, Email: "comm-page-perf-" + suffix + "@example.invalid", CallSign: "CP" + suffix[len(suffix)-8:], Status: 1, ApprovalStatus: 1}
	if err := tx.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	group := &gormdb.Group{Name: "comm-page-perf-" + suffix, Type: 1, OwerID: user.ID, Status: 1}
	if err := tx.Create(group).Error; err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 6; i++ {
		table := fmt.Sprintf("comm_page_perf_digits_%d", i)
		if err := tx.Exec("CREATE TEMPORARY TABLE " + table + " (n TINYINT UNSIGNED PRIMARY KEY)").Error; err != nil {
			t.Fatal(err)
		}
		if err := tx.Exec("INSERT INTO " + table + " (n) VALUES (0),(1),(2),(3),(4),(5),(6),(7),(8),(9)").Error; err != nil {
			t.Fatal(err)
		}
	}
	insertSQL := `
		INSERT INTO comm_records (
			device_id, device_ssid, group_id, user_id, start_time, end_time, duration_ms,
			audio_path, audio_size, status, message_type, sender_username, sender_callsign,
			sender_nickname, sender_dev_model, created_at
		)
		SELECT 0, 101, ?, ?,
			TIMESTAMPADD(MICROSECOND, seq.n * 1000, '2026-08-01 00:00:00'),
			TIMESTAMPADD(MICROSECOND, seq.n * 1000, '2026-08-01 00:00:00'),
			0, '', 0, 2, 0, 'comm-page-perf', 'BG5PERF', 'Comm Page Performance', 101, NOW(3)
		FROM (
			SELECT d0.n + d1.n * 10 + d2.n * 100 + d3.n * 1000 + d4.n * 10000 + d5.n * 100000 AS n
			FROM comm_page_perf_digits_0 d0
			CROSS JOIN comm_page_perf_digits_1 d1
			CROSS JOIN comm_page_perf_digits_2 d2
			CROSS JOIN comm_page_perf_digits_3 d3
			CROSS JOIN comm_page_perf_digits_4 d4
			CROSS JOIN comm_page_perf_digits_5 d5
		) seq
		WHERE seq.n < 120000`
	if err := tx.Exec(insertSQL, group.ID, user.ID).Error; err != nil {
		t.Fatal(err)
	}

	filter := commRecordScopeFilter{CanViewGlobal: true}
	started := time.Now()
	var total int64
	if err := newCommRecordListScope(tx, filter).Count(&total).Error; err != nil {
		t.Fatal(err)
	}
	var ids []uint
	if err := newCommRecordListScope(tx, filter).
		Order("cr.start_time DESC").Order("cr.id DESC").Limit(20).Pluck("cr.id", &ids).Error; err != nil {
		t.Fatal(err)
	}
	var details []CommRecordWithDetails
	if err := newCommRecordDetailsQuery(tx).
		Where("cr.id IN ?", ids).
		Order("cr.start_time DESC").Order("cr.id DESC").
		Scan(&details).Error; err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(started)
	if total < 120000 || len(ids) != 20 || len(details) != 20 {
		t.Fatalf("pagination total=%d ids=%d details=%d", total, len(ids), len(details))
	}
	if elapsed > time.Second {
		t.Fatalf("120k-row global communication pagination took %s", elapsed)
	}
	t.Logf("global count and 20-row detail page from 120,000 records took %s", elapsed)

	plan := explainCommRecordGlobalPage(t, tx)
	if !strings.Contains(plan, "idx_comm_records_status_start_id") {
		t.Fatalf("global pagination did not use the status cursor index:\n%s", plan)
	}
	lowerPlan := strings.ToLower(plan)
	if strings.Contains(lowerPlan, "table scan") || strings.Contains(lowerPlan, "sort:") {
		t.Fatalf("global pagination used a scan or sort:\n%s", plan)
	}
}

func explainCommRecordGlobalPage(t *testing.T, db *gorm.DB) string {
	t.Helper()
	rows, err := db.Raw(`
		EXPLAIN ANALYZE
		SELECT cr.id
		FROM comm_records cr FORCE INDEX (idx_comm_records_status_start_id)
		WHERE cr.status = 2
		ORDER BY cr.start_time DESC, cr.id DESC
		LIMIT 20
	`).Rows()
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
