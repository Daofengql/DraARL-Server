// Integration runs each database fixture in its own disposable database.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"draarl/internal/config"
	drivermysql "github.com/go-sql-driver/mysql"
	"gopkg.in/yaml.v3"
)

type suite struct{ name, pkg, pattern, gate string }

var suites = []suite{
	{"single-node-upgrade", "./internal/gormdb", "TestSingleNodeAccessUpgradeMySQL", ""},
	{"device-capability-switch", "./internal/udphub", "TestDeviceCapabilitySwitchPreservesConfigurationMySQL", "DRAARL_DEVICE_CONFIG_E2E"},
	{"device-config-api", "./internal/handler", "TestDeviceConfigCapabilitiesAndStaleSaveMySQL", "DRAARL_DEVICE_CONFIG_E2E"},
	{"device-binding-retry", "./internal/handler", "TestDynamicBindingRetriesMySQL", "DRAARL_DEVICE_CONFIG_E2E"},
	{"handler-redis", "./internal/handler", "", ""},
	{"auth-redis", "./internal/auth", "", ""},
	{"device-list", "./internal/handler", "TestDeviceListBatchOwnersAndRequestCancellationMySQL", "DRAARL_TODO_E2E"},
	{"authentication", "./internal/handler", "TestRemediationAuthenticationAndLogbookMySQL", "DRAARL_REMEDIATION_E2E"},
	{"group-password", "./internal/handler", "TestGroupPasswordHTTPFlowMySQL", "DRAARL_GROUP_PASSWORD_E2E"},
	{"group-query", "./internal/handler", "TestGroupQueryPaginationAndDeviceStatsMySQL", "DRAARL_GROUP_QUERY_E2E"},
	{"radio-routing", "./internal/handler", "TestRadioSessionRoutingMySQL", "DRAARL_RADIO_SESSIONS_E2E"},
	{"group-membership", "./internal/handler", "TestRemoveGroupMemberHTTPE2E", "DRAARL_RADIO_SESSIONS_E2E"},
	{"messages", "./internal/handler", "TestGroupMessagesHTTPE2E", "DRAARL_MESSAGES_E2E"},
	{"recordings", "./internal/handler", "TestCommRecordsAuthorizationHTTPE2E", "DRAARL_COMM_RECORDS_E2E"},
	{"client-resources", "./internal/handler", "TestClientResourceHTTPE2E", "DRAARL_CLIENT_RESOURCE_E2E"},
	{"broadcast-api", "./internal/handler", "TestBroadcastManagementHTTPE2E", "DRAARL_BROADCAST_API_E2E"},
	{"broadcast-policy", "./internal/handler", "TestVirtualGroupBroadcastPolicyHTTPE2E", "DRAARL_BROADCAST_API_E2E"},
	{"resource-storage", "./internal/gormdb", "Test(ClientResourceLifecycle|LegacyClientReleaseMigrationGuard)MySQL", ""},
	{"db-online-filter", "./internal/gormdb", "TestDeviceListOnlineFilterMySQL", "DRAARL_DEVICE_LIST_E2E"},
	{"message-migration", "./internal/gormdb", "TestCommRecordMessageMigrationMySQL", "DRAARL_MESSAGE_MIGRATION_E2E"},
	{"migration-recovery", "./internal/gormdb", "Test(RecordMigrationVersionRecovery|AppliedMigrationVersionIgnoresRunningState)MySQL", "DRAARL_MIGRATION_RECORD_E2E"},
	{"ghost-preferences", "./internal/gormdb", "TestGhostClientPreferenceMySQL", "DRAARL_GHOST_PREFERENCE_E2E"},
	{"operator-log", "./internal/gormdb", "TestOperatorLogIndexesAndQueriesMySQL", "DRAARL_OPERATOR_LOG_E2E"},
	{"site-secrets", "./internal/gormdb", "TestSiteConfigLegacyPlaintextLazyEncryptionMySQL", "DRAARL_SITECONFIG_CRYPTO_E2E"},
	{"database-tracing", "./internal/gormdb", "TestDatabaseSpansFollowRequestAndRestoreContextMySQL", "DRAARL_OBSERVABILITY_E2E"},
	{"online-events", "./internal/udphub", "TestOnlineEventsPersistLatestSessionMySQL", "DRAARL_ONLINE_EVENTS_E2E"},
	{"device-cache", "./pkg/cache", "TestFilteredDeviceCacheMySQL", "DRAARL_CACHE_E2E"},
	{"websocket-routing", "./pkg/websocket", "Test.*MySQL", "DRAARL_RADIO_SESSIONS_E2E"},
	{"broadcast-schema", "./internal/broadcast/model", "TestBroadcastSchemaMigrationMySQL", "DRAARL_BROADCAST_SCHEMA_E2E"},
	{"broadcast-repository", "./internal/broadcast/repository", "", "DRAARL_BROADCAST_REPOSITORY_E2E"},
	{"ffmpeg", "./internal/broadcast/media", "TestRealFFmpegPipeline", "DRAARL_BROADCAST_FFMPEG_E2E"},
}

type outcome struct {
	Suite   string   `json:"suite"`
	Passed  int      `json:"passed"`
	Skipped int      `json:"skipped"`
	Failed  []string `json:"failed"`
	Success bool     `json:"success"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	out := flag.String("out", "output/integration", "JSON test logs and coverage output")
	only := flag.String("only", "", "comma-separated suite names (empty runs all)")
	race := flag.Bool("race", false, "enable the race detector")
	flag.Parse()
	if os.Getenv("DRAARL_REDIS_ADDR") == "" {
		return fmt.Errorf("DRAARL_REDIS_ADDR is required for Redis integration coverage")
	}
	parsed, err := drivermysql.ParseDSN(os.Getenv("DRAARL_TEST_MYSQL_DSN"))
	if err != nil || !strings.HasPrefix(parsed.DBName, "draarl_test_") {
		return fmt.Errorf("DRAARL_TEST_MYSQL_DSN must name a disposable draarl_test_ database; credentials need CREATE/DROP DATABASE")
	}
	if parsed.Net != "tcp" {
		return fmt.Errorf("integration runner requires a TCP MySQL DSN")
	}
	parsed.DBName, parsed.ParseTime = "", true
	parsed.Timeout, parsed.ReadTimeout, parsed.WriteTimeout = 5*time.Second, 15*time.Second, 15*time.Second
	db, err := sql.Open("mysql", parsed.FormatDSN())
	if err != nil {
		return fmt.Errorf("open integration database: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := os.MkdirAll(*out, 0700); err != nil {
		return err
	}
	selected := map[string]bool{}
	if *only != "" {
		for _, name := range strings.Split(*only, ",") {
			selected[name] = true
		}
	}
	results := []outcome{}
	failed := false
	for _, job := range suites {
		if *only != "" && !selected[job.name] {
			continue
		}
		delete(selected, job.name)
		fmt.Println("running", job.name)
		result, err := runSuite(db, parsed, job, *out, *race)
		if err != nil {
			return fmt.Errorf("%s: %w", job.name, err)
		}
		results = append(results, result)
		data, err := json.Marshal(result)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		failed = failed || !result.Success
	}
	if len(selected) > 0 {
		return fmt.Errorf("unknown suite names: %v", selected)
	}
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	// #nosec G703 -- Test report location is explicitly selected by the local operator.
	if err := os.WriteFile(filepath.Join(*out, "summary.json"), data, 0600); err != nil {
		return err
	}
	if failed {
		return fmt.Errorf("integration failures; see %s", *out)
	}
	return nil
}

func runSuite(db *sql.DB, base *drivermysql.Config, job suite, out string, race bool) (outcome, error) {
	result := outcome{Suite: job.name, Failed: []string{}}
	var token [8]byte
	if _, err := rand.Read(token[:]); err != nil {
		return result, err
	}
	name := "draarl_test_" + hex.EncodeToString(token[:])
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// name is generated exclusively from the fixed prefix and hexadecimal bytes.
	if _, err := db.ExecContext(ctx, "CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		return result, err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if _, err := db.ExecContext(cleanup, "DROP DATABASE `"+name+"`"); err != nil {
			fmt.Fprintln(os.Stderr, "cleanup failed for", name, err)
		}
	}()
	conn := base.Clone()
	conn.DBName = name
	env := os.Environ()
	// Isolate from other suites even if a developer has enabled an E2E flag.
	for _, other := range suites {
		if other.gate != "" {
			env = append(env, other.gate+"=false")
		}
	}
	if job.gate != "" {
		env = append(env, job.gate+"=true")
	}
	env = append(env, "DRAARL_TEST_MYSQL_DSN="+conn.FormatDSN())
	cfg, err := integrationConfig(conn, name)
	if err != nil {
		return result, err
	}
	configPath := filepath.Join(out, name+".yaml")
	// #nosec G703 -- Local test configuration in the operator-selected output directory.
	if err := os.WriteFile(configPath, cfg, 0600); err != nil {
		return result, err
	}
	defer func() { _ = os.Remove(configPath) }()
	absolute, err := filepath.Abs(configPath)
	if err != nil {
		return result, err
	}
	env = append(env, "DRAARL_TEST_CONFIG="+absolute)
	logPath := filepath.Join(out, job.name+".jsonl")
	// #nosec G304 G703 -- Local report path uses a code-defined suite name.
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return result, err
	}
	args := []string{"test", "-count=1", "-timeout=8m", "-json", "-coverprofile=" + filepath.Join(out, job.name+".cover")}
	if race {
		args = append(args, "-race")
	}
	if job.pattern != "" {
		args = append(args, "-run=^("+job.pattern+")$")
	}
	args = append(args, job.pkg)
	testCtx, stop := context.WithTimeout(context.Background(), 10*time.Minute)
	defer stop()
	cmd := exec.CommandContext(testCtx, "go", args...) // #nosec G204 -- Fixed tool and a code-defined package/test manifest; no shell.
	cmd.Env, cmd.Stdout, cmd.Stderr = env, log, log
	err = cmd.Run()
	if closeErr := log.Close(); closeErr != nil {
		return result, closeErr
	}
	result.Success = err == nil
	input, err := os.Open(logPath) // #nosec G304 G703 -- Operator-selected local test output directory.
	if err != nil {
		return result, err
	}
	defer func() { _ = input.Close() }()
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 65536), 4*1024*1024)
	for scanner.Scan() {
		var event struct{ Action, Test, Package string }
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		if event.Test != "" {
			switch event.Action {
			case "pass":
				result.Passed++
			case "skip":
				result.Skipped++
			case "fail":
				result.Failed = append(result.Failed, event.Test)
			}
		} else if event.Action == "fail" {
			result.Success = false
		}
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	// A misspelled -run expression or a skipped dependency must not look green.
	if result.Passed == 0 {
		result.Success = false
	}
	return result, nil
}

func integrationConfig(conn *drivermysql.Config, name string) ([]byte, error) {
	cfg := &config.Configuration{}
	host, port, err := net.SplitHostPort(conn.Addr)
	if err != nil {
		return nil, err
	}
	cfg.Database.Host, cfg.Database.User, cfg.Database.Password, cfg.Database.DBName = host, conn.User, conn.Passwd, name
	cfg.Database.Port, err = strconv.Atoi(port)
	if err != nil {
		return nil, err
	}
	cfg.Database.Timezone = "UTC"
	cfg.JWT.Secret = strings.Repeat("integration", 4)
	cfg.DeviceAuth.AESKey = "0123456789abcdef0123456789abcdef"
	if addr := os.Getenv("DRAARL_REDIS_ADDR"); addr != "" {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		cfg.Redis.Host, cfg.Redis.Port = host, 0
		cfg.Redis.Port, err = strconv.Atoi(port)
		if err != nil {
			return nil, err
		}
	}
	cfg.Redis.Password = os.Getenv("DRAARL_REDIS_PASSWORD")
	cfg.Redis.DB, err = strconv.Atoi("0" + os.Getenv("DRAARL_REDIS_DB"))
	if err != nil {
		return nil, err
	}
	cfg.Redis.Prefix = name
	if err := cfg.SetDefaults(); err != nil {
		return nil, err
	}
	return yaml.Marshal(cfg)
}
