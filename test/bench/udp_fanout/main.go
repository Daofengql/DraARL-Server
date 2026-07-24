package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"draarl/internal/config"
	"draarl/internal/protocol"
	draarlcrypto "draarl/pkg/crypto"
	"draarl/pkg/storage"

	_ "github.com/go-sql-driver/mysql"
)

const (
	benchPrefix     = "__draarl_udp_bench_"
	benchGroupName  = "__draarl_udp_fanout_bench__"
	benchPassword   = "Bench123"
	devicesPerUser  = 248
	maxBenchClients = 20000
	heartbeatEvery  = 2 * time.Second
	benchLockName   = "draarl_udp_fanout_bench"
)

var (
	packetMarker = [8]byte{'D', 'R', 'A', 'B', 'E', 'N', 'C', 'H'}
	activeStage  atomic.Pointer[stageMetrics]
)

type options struct {
	configPath     string
	serverAddr     string
	serverAddrs    string
	apiBase        string
	serverPID      int
	processPIDs    string
	levels         []int
	duration       time.Duration
	interval       time.Duration
	intervals      []time.Duration
	payload        int
	settle         time.Duration
	groups         int
	placement      string
	churn          bool
	churnEvery     time.Duration
	edgeResetEvery time.Duration
	confirm        bool
	cleanupOnly    bool
}

type processTarget struct {
	name string
	pid  int
}

type benchIdentity struct {
	username string
	ssid     byte
	dmrid    uint32
	mac      string
	ip       net.IP
}

type benchClient struct {
	identity      benchIdentity
	conn          *net.UDPConn
	server        atomic.Pointer[net.UDPAddr]
	authCh        chan struct{}
	authOnce      sync.Once
	heartbeatAcks chan string
	receivedTypes [6]atomic.Uint64
	stopCh        chan struct{}
	stopOnce      sync.Once
	wg            sync.WaitGroup
}

type benchData struct {
	groupIDs  []int64
	deviceIDs []int64
}

type stageMetrics struct {
	id        uint64
	received  atomic.Uint64
	latencyUS atomic.Uint64
	maxUS     atomic.Uint64
	buckets   [12]atomic.Uint64
}

type processSample struct {
	cpu time.Duration
	rss uint64
}

type processResult struct {
	corePercent float64
	machinePct  float64
	rssMB       float64
	peakRSSMB   float64
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() (runErr error) {
	opts, err := parseOptions()
	if err != nil {
		return err
	}
	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	db, err := sql.Open("mysql", cfg.GetDSN())
	if err != nil {
		return fmt.Errorf("open mysql: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(15 * time.Second)
	db.SetConnMaxLifetime(30 * time.Second)
	if err := db.Ping(); err != nil {
		return fmt.Errorf("ping mysql: %w", err)
	}
	lockConn, err := db.Conn(context.Background())
	if err != nil {
		return fmt.Errorf("reserve MySQL benchmark connection: %w", err)
	}
	defer lockConn.Close()
	var lockAcquired int
	if err := lockConn.QueryRowContext(context.Background(), "SELECT GET_LOCK(?, 0)", benchLockName).Scan(&lockAcquired); err != nil {
		return fmt.Errorf("acquire MySQL benchmark lock: %w", err)
	}
	if lockAcquired != 1 {
		return errors.New("another UDP fan-out benchmark is already using this database")
	}
	defer func() {
		var released sql.NullInt64
		_ = lockConn.QueryRowContext(context.Background(), "SELECT RELEASE_LOCK(?)", benchLockName).Scan(&released)
	}()

	localStorage := storage.ResolveDriver(cfg) == storage.DriverLocal
	localRoot := storage.LocalRootPath(cfg)
	if !localStorage {
		return errors.New("UDP fan-out benchmark requires local storage so Type 5 recordings can be cleaned")
	}
	cleanupStale := func() error {
		var cleanupErr error
		if localStorage {
			if _, err := cleanupBenchAudioFiles(db, localRoot); err != nil {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("cleanup benchmark audio: %w", err))
			}
		}
		if err := cleanupBenchData(db); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("cleanup benchmark rows: %w", err))
		}
		return cleanupErr
	}
	if opts.cleanupOnly {
		if err := cleanupStale(); err != nil {
			return err
		}
		fmt.Println("CLEANUP stale benchmark recordings and database rows removed")
		return nil
	}

	serverUDPs, err := resolveServerAddrs(opts.serverAddr, opts.serverAddrs)
	if err != nil {
		return err
	}
	if opts.churn && len(serverUDPs) < 2 {
		return errors.New("-churn requires at least two UDP servers")
	}
	processTargets, err := parseProcessTargets(opts.serverPID, opts.processPIDs)
	if err != nil {
		return err
	}
	serverMeters := make([]*processMeter, 0, len(processTargets))
	for _, target := range processTargets {
		meter, meterErr := newProcessMeter(target.name, target.pid)
		if meterErr != nil {
			return fmt.Errorf("open %s process %d: %w", target.name, target.pid, meterErr)
		}
		serverMeters = append(serverMeters, meter)
		defer meter.close()
	}
	loadMeter, err := newProcessMeter("loadgen", os.Getpid())
	if err != nil {
		return fmt.Errorf("open load generator process: %w", err)
	}
	defer loadMeter.close()

	maxClients := opts.levels[len(opts.levels)-1]
	if err := cleanupStale(); err != nil {
		return fmt.Errorf("cleanup stale benchmark data: %w", err)
	}
	benchRows, err := setupBenchData(db, cfg.DeviceAuth.AESKey, maxClients, opts.groups, opts.churn)
	if err != nil {
		return fmt.Errorf("setup benchmark data: %w", err)
	}
	fmt.Printf("SETUP group_ids=%v groups=%d clients=%d users=%d\n", benchRows.groupIDs, opts.groups, maxClients, usersForClients(maxClients))

	clients := make([]*benchClient, 0, maxClients)
	defer func() {
		activeStage.Store(nil)
		closeClients(clients)
		var cleanupErr error
		fmt.Println("WAIT recording pipeline flush (42s)")
		time.Sleep(42 * time.Second)
		removed, err := cleanupBenchAudioFiles(db, localRoot)
		if err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("cleanup benchmark audio files: %w", err))
		} else {
			fmt.Printf("CLEANUP recording files removed=%d\n", removed)
		}
		if err := cleanupBenchData(db); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("cleanup benchmark data: %w", err))
		} else {
			fmt.Println("CLEANUP database benchmark rows removed")
		}
		if cleanupErr != nil {
			if runErr == nil {
				runErr = cleanupErr
			} else {
				log.Printf("cleanup after benchmark failure: %v", cleanupErr)
			}
		}
	}()

	// The group cache refreshes every 10 seconds. Wait for the newly inserted
	// group before sending device heartbeats.
	time.Sleep(11 * time.Second)

	for levelIndex, level := range opts.levels {
		beforeServers := sampleProcesses(serverMeters)
		connectStart := time.Now()
		newClients, err := connectRange(serverUDPs, opts.placement, opts.groups, len(clients), level)
		if err != nil {
			return fmt.Errorf("connect clients up to %d: %w", level, err)
		}
		clients = append(clients, newClients...)
		connectElapsed := time.Since(connectStart)
		afterServers := sampleProcesses(serverMeters)
		connectCPU := aggregateCPUCorePercent(beforeServers, afterServers, connectElapsed)
		fmt.Printf("CONNECTED clients=%d added=%d elapsed=%s rate=%.1f_clients_s server_cpu=%.1f%%_of_one_core\n",
			level, len(newClients), connectElapsed.Round(time.Millisecond), float64(len(newClients))/connectElapsed.Seconds(), connectCPU)

		// Allow runtime membership and receiver snapshots to settle. Later tiers
		// use the same source device, preserving half-duplex ownership.
		settle := opts.settle
		if levelIndex == 0 && settle < 3*time.Second {
			settle = 3 * time.Second
		}
		time.Sleep(settle)
		warmUp(clients[:opts.groups], opts.payload)
		if opts.churn {
			result, err := runChurnSoak(clients, benchRows, opts, cfg.JWT.Secret, serverMeters, loadMeter)
			if err != nil {
				return fmt.Errorf("mixed churn soak: %w", err)
			}
			fmt.Println(result)
			break
		}

		stopLevels := false
		for intervalIndex, interval := range opts.intervals {
			opts.interval = interval
			stageID := uint64(levelIndex+1)<<32 | uint64(intervalIndex+1)
			result, loss := runStage(clients, opts, serverMeters, loadMeter, stageID)
			fmt.Println(result)
			if loss > 5 {
				fmt.Printf("STOP loss_pct=%.3f exceeded 5%%; faster intervals and higher levels would not be representative\n", loss)
				stopLevels = true
				break
			}
		}
		if stopLevels {
			break
		}
	}

	closeClients(clients)
	clients = nil
	// Let the server observe the closed sockets only through its normal timeout;
	// database cleanup below removes the benchmark runtime entries on cache sync.
	time.Sleep(time.Second)
	return nil
}

func cleanupBenchAudioFiles(db *sql.DB, root string) (int, error) {
	if strings.TrimSpace(root) == "" {
		root = "./data/storage"
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return 0, err
	}
	rows, err := db.Query(`SELECT cr.audio_path
		FROM comm_records cr
		JOIN devices d ON d.id=cr.device_id
		JOIN users u ON u.id=d.owner_id
		WHERE LEFT(u.name, ?) = ? AND cr.audio_path LIKE 'comm-records/%'`, len(benchPrefix), benchPrefix)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	removed := 0
	for rows.Next() {
		var objectKey string
		if err := rows.Scan(&objectKey); err != nil {
			return removed, err
		}
		candidate, err := filepath.Abs(filepath.Join(rootAbs, filepath.FromSlash(objectKey)))
		if err != nil {
			return removed, err
		}
		if !strings.HasPrefix(candidate, rootAbs+string(os.PathSeparator)) {
			return removed, fmt.Errorf("recording path escaped storage root: %s", objectKey)
		}
		if err := os.Remove(candidate); err != nil && !os.IsNotExist(err) {
			return removed, err
		}
		removed++
	}
	return removed, rows.Err()
}

func parseOptions() (options, error) {
	var rawLevels, rawIntervals string
	opts := options{}
	flag.StringVar(&opts.configPath, "config", "config.yaml", "DraARL config path")
	flag.StringVar(&opts.serverAddr, "server", "127.0.0.1:60050", "UDP server address")
	flag.StringVar(&opts.serverAddrs, "servers", "", "comma-separated UDP server addresses; overrides -server")
	flag.StringVar(&opts.apiBase, "api-base", "", "HTTP API base including /api; required by -churn")
	flag.IntVar(&opts.serverPID, "server-pid", 0, "DraARL server process ID")
	flag.StringVar(&opts.processPIDs, "process-pids", "", "comma-separated measured processes, for example center=100,edge-a=101")
	flag.StringVar(&rawLevels, "levels", "100,500,1000,2000,4000", "ascending client counts")
	flag.DurationVar(&opts.duration, "duration", 10*time.Second, "measurement duration per level")
	flag.DurationVar(&opts.interval, "interval", 120*time.Millisecond, "voice packet interval")
	flag.StringVar(&rawIntervals, "intervals", "", "comma-separated voice intervals scanned from slower to faster; overrides -interval")
	flag.IntVar(&opts.payload, "payload", 320, "voice payload bytes")
	flag.DurationVar(&opts.settle, "settle", 3*time.Second, "settling time after adding clients")
	flag.IntVar(&opts.groups, "groups", 1, "independent groups and simultaneous speakers")
	flag.StringVar(&opts.placement, "placement", "local", "client placement across -servers: local, distributed, or cross")
	flag.BoolVar(&opts.churn, "churn", false, "run mixed route/control churn soak instead of a static fan-out stage")
	flag.DurationVar(&opts.churnEvery, "churn-interval", 5*time.Second, "interval between route/control churn operations")
	flag.DurationVar(&opts.edgeResetEvery, "edge-reset-interval", 2*time.Minute, "interval between edge control reconnect tests; 0 disables")
	flag.BoolVar(&opts.confirm, "confirm-test-data", false, "confirm temporary MySQL rows may be created and deleted")
	flag.BoolVar(&opts.cleanupOnly, "cleanup-only", false, "remove stale benchmark rows/files without running a benchmark")
	flag.Parse()

	if !opts.confirm {
		return opts, errors.New("-confirm-test-data is required because this tool writes temporary MySQL rows")
	}
	if !opts.cleanupOnly && opts.serverPID <= 0 && strings.TrimSpace(opts.processPIDs) == "" {
		return opts, fmt.Errorf("-server-pid or -process-pids is required")
	}
	for _, part := range strings.Split(rawLevels, ",") {
		level, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || level < 2 {
			return opts, fmt.Errorf("invalid client level %q", part)
		}
		if len(opts.levels) > 0 && level <= opts.levels[len(opts.levels)-1] {
			return opts, fmt.Errorf("client levels must be strictly ascending")
		}
		opts.levels = append(opts.levels, level)
	}
	if len(opts.levels) == 0 || opts.levels[len(opts.levels)-1] > maxBenchClients {
		return opts, fmt.Errorf("client levels must end between 2 and %d", maxBenchClients)
	}
	if opts.churn {
		if len(opts.levels) != 1 {
			return opts, errors.New("-churn requires exactly one client level")
		}
		if strings.TrimSpace(opts.apiBase) == "" {
			return opts, errors.New("-api-base is required by -churn")
		}
		if opts.levels[0] < opts.groups+2 {
			return opts, errors.New("-churn requires at least two non-speaker clients")
		}
		if opts.groups < 2 {
			return opts, errors.New("-churn requires at least two groups")
		}
		if opts.churnEvery < time.Second {
			return opts, errors.New("-churn-interval must be at least 1s")
		}
		if opts.edgeResetEvery < 0 || opts.edgeResetEvery > 0 && opts.edgeResetEvery < 5*time.Second {
			return opts, errors.New("-edge-reset-interval must be 0 or at least 5s")
		}
		if opts.duration < 8*opts.churnEvery {
			return opts, errors.New("-duration is too short to cover one complete churn cycle")
		}
		if opts.edgeResetEvery > 0 && opts.duration < opts.edgeResetEvery+35*time.Second {
			return opts, errors.New("-duration must leave at least 35s to verify an edge reconnect")
		}
	}
	if opts.groups < 1 || opts.groups > 1024 {
		return opts, fmt.Errorf("groups must be between 1 and 1024")
	}
	opts.placement = strings.ToLower(strings.TrimSpace(opts.placement))
	if opts.placement != "local" && opts.placement != "distributed" && opts.placement != "cross" {
		return opts, fmt.Errorf("placement must be local, distributed, or cross")
	}
	for _, level := range opts.levels {
		if level < opts.groups {
			return opts, fmt.Errorf("client level %d is smaller than group count %d", level, opts.groups)
		}
	}
	if opts.duration < time.Second || opts.interval < 10*time.Millisecond {
		return opts, fmt.Errorf("duration must be >=1s and interval >=10ms")
	}
	if strings.TrimSpace(rawIntervals) == "" {
		opts.intervals = []time.Duration{opts.interval}
	} else {
		var previous time.Duration
		for _, part := range strings.Split(rawIntervals, ",") {
			interval, err := time.ParseDuration(strings.TrimSpace(part))
			if err != nil || interval < 10*time.Millisecond {
				return opts, fmt.Errorf("invalid voice interval %q", part)
			}
			if previous > 0 && interval >= previous {
				return opts, errors.New("voice intervals must be ordered from slower to faster")
			}
			opts.intervals = append(opts.intervals, interval)
			previous = interval
		}
	}
	if opts.payload < 32 || opts.payload+protocol.DraARLv1HeaderSize > protocol.DraARLv1MaxPacketSize {
		return opts, fmt.Errorf("payload must be between 32 and %d bytes", protocol.DraARLv1MaxPacketSize-protocol.DraARLv1HeaderSize)
	}
	return opts, nil
}

func resolveServerAddrs(single, multiple string) ([]*net.UDPAddr, error) {
	raw := strings.TrimSpace(multiple)
	if raw == "" {
		raw = strings.TrimSpace(single)
	}
	seen := make(map[string]struct{})
	var result []*net.UDPAddr
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			return nil, errors.New("UDP server address is empty")
		}
		addr, err := net.ResolveUDPAddr("udp4", item)
		if err != nil {
			return nil, fmt.Errorf("resolve UDP server %q: %w", item, err)
		}
		key := addr.String()
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("duplicate UDP server address %q", item)
		}
		seen[key] = struct{}{}
		result = append(result, addr)
	}
	return result, nil
}

func parseProcessTargets(serverPID int, raw string) ([]processTarget, error) {
	if strings.TrimSpace(raw) == "" {
		if serverPID <= 0 {
			return nil, errors.New("measured process PID is required")
		}
		return []processTarget{{name: "server", pid: serverPID}}, nil
	}
	seen := make(map[string]struct{})
	var result []processTarget
	for _, item := range strings.Split(raw, ",") {
		parts := strings.SplitN(strings.TrimSpace(item), "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid measured process %q; want name=pid", item)
		}
		name := sanitizeMetricName(parts[0])
		pid, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if name == "" || err != nil || pid <= 0 {
			return nil, fmt.Errorf("invalid measured process %q", item)
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("duplicate measured process name %q", name)
		}
		seen[name] = struct{}{}
		result = append(result, processTarget{name: name, pid: pid})
	}
	return result, nil
}

func sanitizeMetricName(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	for _, r := range raw {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			if r == '-' {
				r = '_'
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

func setupBenchData(db *sql.DB, aesKey string, clients, groupCount int, adminOwner bool) (*benchData, error) {
	cipher, err := draarlcrypto.NewAESCrypto(aesKey)
	if err != nil {
		return nil, err
	}
	encryptedPassword, err := cipher.Encrypt(benchPassword)
	if err != nil {
		return nil, err
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	userIDs := make([]int64, usersForClients(clients))
	for i := range userIDs {
		role := "user"
		if adminOwner && i == 0 {
			role = "admin"
		}
		result, err := tx.Exec(`INSERT INTO users
			(name,email,email_verified,callsign,roles,status,approval_status,dmrid,device_password,create_time,update_time)
			VALUES (?,?,?,?,?,1,1,?,?,NOW(3),NOW(3))`,
			usernameForUser(i), fmt.Sprintf("%s%04d@example.invalid", benchPrefix, i), 1,
			callsignForUser(i), role, 9000000+i, encryptedPassword)
		if err != nil {
			return nil, fmt.Errorf("insert user %d: %w", i, err)
		}
		userIDs[i], err = result.LastInsertId()
		if err != nil {
			return nil, err
		}
	}

	groupIDs := make([]int64, groupCount)
	for i := range groupIDs {
		groupResult, err := tx.Exec(`INSERT INTO public_groups
			(name,type,ower_id,master_server,slave_server,status,is_virtual,create_time,update_time,note)
			VALUES (?,1,?,0,0,1,0,NOW(3),NOW(3),?)`, fmt.Sprintf("%s%02d", benchGroupName, i+1), userIDs[0], benchPrefix)
		if err != nil {
			return nil, fmt.Errorf("insert group %d: %w", i, err)
		}
		groupIDs[i], err = groupResult.LastInsertId()
		if err != nil {
			return nil, err
		}
	}

	deviceIDs := make([]int64, clients)
	stmt, err := tx.Prepare(`INSERT INTO devices
		(name,dmrid,ssid,owner_id,qth,last_online_ip,dev_model,group_id,status,is_certed,priority,
		 disable_send,disable_recv,is_online,online_time,note,create_time,update_time)
		VALUES (?,?,?,?, '',?,2,?,1,1,100,0,0,1,NOW(),?,NOW(3),NOW(3))`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	for i := 0; i < clients; i++ {
		identity := identityForIndex(i)
		result, err := stmt.Exec(fmt.Sprintf("bench-device-%05d", i), identity.dmrid, identity.ssid,
			userIDs[i/devicesPerUser], identity.ip.String(), groupIDs[i%groupCount], benchPrefix)
		if err != nil {
			return nil, fmt.Errorf("insert device %d: %w", i, err)
		}
		deviceIDs[i], err = result.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("read device %d id: %w", i, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &benchData{groupIDs: groupIDs, deviceIDs: deviceIDs}, nil
}

func cleanupBenchData(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []struct {
		query string
		args  []any
	}{
		{`DELETE cr FROM comm_records cr JOIN devices d ON d.id=cr.device_id JOIN users u ON u.id=d.owner_id WHERE LEFT(u.name, ?) = ?`, []any{len(benchPrefix), benchPrefix}},
		{`DELETE FROM public_groups WHERE name=? OR note=?`, []any{benchGroupName, benchPrefix}},
		{`DELETE FROM users WHERE LEFT(name, ?) = ?`, []any{len(benchPrefix), benchPrefix}},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement.query, statement.args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func usersForClients(clients int) int {
	return (clients + devicesPerUser - 1) / devicesPerUser
}

func usernameForUser(index int) string {
	return fmt.Sprintf("%su%04d", benchPrefix, index)
}

func callsignForUser(index int) string {
	return fmt.Sprintf("BT%05d", index)
}

func identityForIndex(index int) benchIdentity {
	withinUser := index % devicesPerUser
	ssid := withinUser + 1
	if ssid >= 100 {
		ssid += 6
	}
	n := index + 1
	return benchIdentity{
		username: usernameForUser(index / devicesPerUser),
		ssid:     byte(ssid),
		dmrid:    uint32(9100000 + index),
		mac:      fmt.Sprintf("02:42:%02X:%02X:%02X:%02X", byte(n>>24), byte(n>>16), byte(n>>8), byte(n)),
		ip:       net.IPv4(127, 64+byte(n/(254*254)), 1+byte((n/254)%254), 1+byte(n%254)),
	}
}

func connectRange(servers []*net.UDPAddr, placement string, groups, start, end int) ([]*benchClient, error) {
	count := end - start
	clients := make([]*benchClient, count)
	type connectResult struct {
		index  int
		client *benchClient
		err    error
	}
	jobs := make(chan int)
	results := make(chan connectResult, count)
	workers := 32
	if count < workers {
		workers = count
	}
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for globalIndex := range jobs {
				server := servers[serverIndexForClient(globalIndex, groups, len(servers), placement)]
				client, err := connectClient(server, identityForIndex(globalIndex))
				results <- connectResult{index: globalIndex - start, client: client, err: err}
			}
		}()
	}
	go func() {
		for i := start; i < end; i++ {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	var firstErr error
	for result := range results {
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
			}
			continue
		}
		clients[result.index] = result.client
	}
	if firstErr != nil {
		closeClients(clients)
		return nil, firstErr
	}
	return clients, nil
}

func serverIndexForClient(index, groups, serverCount int, placement string) int {
	if serverCount <= 1 || placement == "local" {
		return 0
	}
	positionInGroup := index / groups
	if placement == "cross" {
		if positionInGroup == 0 {
			return 0
		}
		return 1 + (positionInGroup-1)%(serverCount-1)
	}
	return positionInGroup % serverCount
}

func connectClient(server *net.UDPAddr, identity benchIdentity) (*benchClient, error) {
	client := &benchClient{
		identity:      identity,
		authCh:        make(chan struct{}),
		heartbeatAcks: make(chan string, 16),
		stopCh:        make(chan struct{}),
	}
	client.server.Store(server)
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: identity.ip, Port: 0})
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", identity.ip, err)
	}
	_ = conn.SetReadBuffer(256 * 1024)
	client.conn = conn
	client.wg.Add(1)
	go client.readLoop()

	for attempt := 0; attempt < 4; attempt++ {
		if err := client.sendHeartbeat(); err != nil {
			client.close()
			return nil, err
		}
		select {
		case <-client.authCh:
			client.wg.Add(1)
			go client.heartbeatLoop()
			return client, nil
		case <-time.After(2500 * time.Millisecond):
		}
	}
	client.close()
	return nil, fmt.Errorf("authenticate %s-%d from %s: timeout", identity.username, identity.ssid, identity.ip)
}

func (c *benchClient) sendHeartbeat() error {
	return c.sendHeartbeatTo(c.serverAddr())
}

func (c *benchClient) sendHeartbeatTo(server *net.UDPAddr) error {
	if server == nil {
		return errors.New("UDP server is not set")
	}
	payload := make([]byte, protocol.HeartbeatGPSPayloadSize, protocol.HeartbeatGPSPayloadSize+17)
	payload = append(payload, c.identity.mac...)
	packet := protocol.EncodeDraARLv1(c.identity.username, benchPassword, c.identity.ssid,
		protocol.DraARLTypeHeartbeat, protocol.DraARLDevModelESP32NoRadio, c.identity.dmrid, "", payload)
	_, err := c.conn.WriteToUDP(packet, server)
	return err
}

func (c *benchClient) serverAddr() *net.UDPAddr { return c.server.Load() }

func (c *benchClient) authenticateAt(server *net.UDPAddr, timeout time.Duration) error {
	if server == nil {
		return errors.New("UDP server is not set")
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := c.sendHeartbeatTo(server); err != nil {
			return err
		}
		attemptDeadline := time.NewTimer(time.Second)
		for {
			select {
			case from := <-c.heartbeatAcks:
				if from == server.String() {
					attemptDeadline.Stop()
					c.server.Store(server)
					return nil
				}
			case <-attemptDeadline.C:
				goto retry
			case <-c.stopCh:
				attemptDeadline.Stop()
				return errors.New("client stopped")
			}
		}
	retry:
	}
	return fmt.Errorf("authenticate %s-%d at %s: timeout", c.identity.username, c.identity.ssid, server)
}

func (c *benchClient) heartbeatLoop() {
	defer c.wg.Done()
	jitter := time.Duration(int(c.identity.ssid)%200) * time.Millisecond
	ticker := time.NewTicker(heartbeatEvery + jitter)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			_ = c.sendHeartbeat()
		}
	}
}

func (c *benchClient) readLoop() {
	defer c.wg.Done()
	buf := make([]byte, 2048)
	for {
		n, from, err := c.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if n < protocol.DraARLv1HeaderSize || !bytes.Equal(buf[:4], []byte(protocol.DraARLVersion)) {
			continue
		}
		packetType := buf[48]
		if packetType == protocol.DraARLTypeHeartbeat && len(bytes.TrimRight(buf[54:86], "\x00")) > 0 {
			c.authOnce.Do(func() { close(c.authCh) })
			select {
			case c.heartbeatAcks <- from.String():
			default:
			}
			continue
		}
		if int(packetType) < len(c.receivedTypes) {
			c.receivedTypes[packetType].Add(1)
		}
		metrics := activeStage.Load()
		if metrics == nil || packetType != protocol.DraARLTypeOpus16K || n < protocol.DraARLv1HeaderSize+32 {
			continue
		}
		payload := buf[protocol.DraARLv1HeaderSize:n]
		if !bytes.Equal(payload[:8], packetMarker[:]) || binary.BigEndian.Uint64(payload[8:16]) != metrics.id {
			continue
		}
		sentAt := int64(binary.BigEndian.Uint64(payload[24:32]))
		latencyUS := uint64(0)
		if elapsed := time.Now().UnixNano() - sentAt; elapsed > 0 {
			latencyUS = uint64(elapsed / 1000)
		}
		metrics.observe(latencyUS)
	}
}

func (c *benchClient) receivedType(packetType byte) uint64 {
	if int(packetType) >= len(c.receivedTypes) {
		return 0
	}
	return c.receivedTypes[packetType].Load()
}

func (c *benchClient) close() {
	if c == nil {
		return
	}
	c.stopOnce.Do(func() {
		close(c.stopCh)
		if c.conn != nil {
			_ = c.conn.Close()
		}
	})
	c.wg.Wait()
}

func closeClients(clients []*benchClient) {
	var wg sync.WaitGroup
	for _, client := range clients {
		if client == nil {
			continue
		}
		wg.Add(1)
		go func(c *benchClient) {
			defer wg.Done()
			c.close()
		}(client)
	}
	wg.Wait()
}

func warmUp(sources []*benchClient, payloadSize int) {
	for i := 0; i < 8; i++ {
		for sourceIndex, source := range sources {
			packet := buildVoicePacket(source.identity, payloadSize, 0, uint64(i*len(sources)+sourceIndex))
			_, _ = source.conn.WriteToUDP(packet, source.serverAddr())
		}
		time.Sleep(120 * time.Millisecond)
	}
	time.Sleep(time.Second)
}

func runStage(clients []*benchClient, opts options, serverMeters []*processMeter, loadMeter *processMeter, stageID uint64) (string, float64) {
	metrics := &stageMetrics{id: stageID}
	activeStage.Store(metrics)
	time.Sleep(200 * time.Millisecond)

	serverBefore := sampleProcesses(serverMeters)
	loadBefore, _ := loadMeter.sample()
	stopSampling := make(chan struct{})
	serverPeaks := make([]*atomic.Uint64, len(serverMeters))
	loadPeak := atomic.Uint64{}
	for i := range serverMeters {
		serverPeaks[i] = &atomic.Uint64{}
		serverPeaks[i].Store(serverBefore[i].rss)
	}
	loadPeak.Store(loadBefore.rss)
	go sampleMemoryPeaks(stopSampling, serverMeters, loadMeter, serverPeaks, &loadPeak)

	start := time.Now()
	deadline := start.Add(opts.duration)
	var sent uint64
	var expected uint64
	groupSizes := make([]int, opts.groups)
	for i := range clients {
		groupSizes[i%opts.groups]++
	}
	var ticks uint64
	for now := start; now.Before(deadline); now = time.Now() {
		for groupIndex := 0; groupIndex < opts.groups; groupIndex++ {
			source := clients[groupIndex]
			packet := buildVoicePacket(source.identity, opts.payload, stageID, sent)
			if _, err := source.conn.WriteToUDP(packet, source.serverAddr()); err == nil {
				sent++
				expected += uint64(groupSizes[groupIndex] - 1)
			}
		}
		ticks++
		next := start.Add(time.Duration(ticks) * opts.interval)
		if sleep := time.Until(next); sleep > 0 {
			time.Sleep(sleep)
		}
	}
	// Loopback delivery should be nearly immediate, but include a bounded drain
	// period so the final server fan-out batch is counted.
	time.Sleep(time.Second)
	elapsed := time.Since(start)
	close(stopSampling)
	serverAfter := sampleProcesses(serverMeters)
	loadAfter, _ := loadMeter.sample()
	activeStage.Store(nil)

	received := metrics.received.Load()
	loss := 0.0
	if expected > 0 && received < expected {
		loss = 100 * float64(expected-received) / float64(expected)
	}
	packetBytes := opts.payload + protocol.DraARLv1HeaderSize
	serverResults := make([]processResult, len(serverMeters))
	serverResult := processResult{}
	for i := range serverMeters {
		serverResults[i] = processDelta(serverBefore[i], serverAfter[i], serverPeaks[i].Load(), elapsed)
		serverResult.corePercent += serverResults[i].corePercent
		serverResult.machinePct += serverResults[i].machinePct
		serverResult.rssMB += serverResults[i].rssMB
		serverResult.peakRSSMB += serverResults[i].peakRSSMB
	}
	loadResult := processDelta(loadBefore, loadAfter, loadPeak.Load(), elapsed)
	avgLatency := 0.0
	if received > 0 {
		avgLatency = float64(metrics.latencyUS.Load()) / float64(received) / 1000
	}
	maxLatency := float64(metrics.maxUS.Load()) / 1000
	p95 := float64(metrics.percentileBucket(0.95)) / 1000
	outputPPS := float64(received) / opts.duration.Seconds()
	inputPPS := float64(sent) / opts.duration.Seconds()
	outputMbps := outputPPS * float64(packetBytes) * 8 / 1_000_000
	result := fmt.Sprintf("RESULT clients=%d groups=%d senders=%d servers=%d placement=%s type=%d packet_bytes=%d interval=%s sent=%d input_pps=%.1f expected=%d received=%d loss_pct=%.4f output_pps=%.0f output_mbps=%.2f latency_avg_ms=%.3f latency_p95_le_ms=%.3f latency_max_ms=%.3f server_cpu_cores=%.2f server_machine_cpu_pct=%.2f server_rss_mb=%.1f server_peak_rss_mb=%.1f loadgen_cpu_cores=%.2f loadgen_rss_mb=%.1f wall=%s",
		len(clients), opts.groups, opts.groups, countDistinctServers(clients), opts.placement, protocol.DraARLTypeOpus16K, packetBytes, opts.interval, sent, inputPPS, expected, received, loss,
		outputPPS, outputMbps, avgLatency, p95, maxLatency,
		serverResult.corePercent/100, serverResult.machinePct, serverResult.rssMB, serverResult.peakRSSMB,
		loadResult.corePercent/100, loadResult.rssMB, elapsed.Round(time.Millisecond))
	for i, meter := range serverMeters {
		result += fmt.Sprintf(" proc_%s_cpu_cores=%.2f proc_%s_rss_mb=%.1f proc_%s_peak_rss_mb=%.1f",
			meter.name, serverResults[i].corePercent/100, meter.name, serverResults[i].rssMB, meter.name, serverResults[i].peakRSSMB)
	}
	return result, loss
}

func countDistinctServers(clients []*benchClient) int {
	seen := make(map[string]struct{})
	for _, client := range clients {
		if client != nil && client.serverAddr() != nil {
			seen[client.serverAddr().String()] = struct{}{}
		}
	}
	return len(seen)
}

func buildVoicePacket(identity benchIdentity, payloadSize int, stageID, sequence uint64) []byte {
	payload := make([]byte, payloadSize)
	copy(payload[:8], packetMarker[:])
	binary.BigEndian.PutUint64(payload[8:16], stageID)
	binary.BigEndian.PutUint64(payload[16:24], sequence)
	binary.BigEndian.PutUint64(payload[24:32], uint64(time.Now().UnixNano()))
	for i := 32; i < len(payload); i++ {
		payload[i] = byte(i + int(sequence))
	}
	return protocol.EncodeDraARLv1(identity.username, benchPassword, identity.ssid, protocol.DraARLTypeOpus16K,
		protocol.DraARLDevModelESP32NoRadio, identity.dmrid, "", payload)
}

func (m *stageMetrics) observe(latencyUS uint64) {
	m.received.Add(1)
	m.latencyUS.Add(latencyUS)
	for {
		old := m.maxUS.Load()
		if latencyUS <= old || m.maxUS.CompareAndSwap(old, latencyUS) {
			break
		}
	}
	limits := [...]uint64{250, 500, 1000, 2000, 5000, 10000, 20000, 50000, 100000, 250000, 500000, math.MaxUint64}
	for i, limit := range limits {
		if latencyUS <= limit {
			m.buckets[i].Add(1)
			break
		}
	}
}

func (m *stageMetrics) percentileBucket(percentile float64) uint64 {
	total := m.received.Load()
	if total == 0 {
		return 0
	}
	target := uint64(math.Ceil(float64(total) * percentile))
	limits := [...]uint64{250, 500, 1000, 2000, 5000, 10000, 20000, 50000, 100000, 250000, 500000, 1_000_000}
	var cumulative uint64
	for i := range m.buckets {
		cumulative += m.buckets[i].Load()
		if cumulative >= target {
			return limits[i]
		}
	}
	return limits[len(limits)-1]
}

func sampleProcesses(meters []*processMeter) []processSample {
	result := make([]processSample, len(meters))
	for i, meter := range meters {
		result[i], _ = meter.sample()
	}
	return result
}

func aggregateCPUCorePercent(before, after []processSample, elapsed time.Duration) float64 {
	var total float64
	for i := range before {
		if i < len(after) {
			total += cpuCorePercent(before[i], after[i], elapsed)
		}
	}
	return total
}

func sampleMemoryPeaks(stop <-chan struct{}, servers []*processMeter, load *processMeter, serverPeaks []*atomic.Uint64, loadPeak *atomic.Uint64) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			for i, server := range servers {
				if sample, err := server.sample(); err == nil {
					storeMax(serverPeaks[i], sample.rss)
				}
			}
			if sample, err := load.sample(); err == nil {
				storeMax(loadPeak, sample.rss)
			}
		}
	}
}

func storeMax(target *atomic.Uint64, value uint64) {
	for {
		old := target.Load()
		if value <= old || target.CompareAndSwap(old, value) {
			return
		}
	}
}

func cpuCorePercent(before, after processSample, elapsed time.Duration) float64 {
	if elapsed <= 0 || after.cpu < before.cpu {
		return 0
	}
	return 100 * float64(after.cpu-before.cpu) / float64(elapsed)
}

func processDelta(before, after processSample, peak uint64, elapsed time.Duration) processResult {
	corePercent := cpuCorePercent(before, after, elapsed)
	return processResult{
		corePercent: corePercent,
		machinePct:  corePercent / float64(runtime.NumCPU()),
		rssMB:       float64(after.rss) / 1024 / 1024,
		peakRSSMB:   float64(peak) / 1024 / 1024,
	}
}
