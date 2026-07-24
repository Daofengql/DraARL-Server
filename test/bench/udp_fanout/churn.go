package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"draarl/internal/interconnect"
	"draarl/internal/protocol"
	appjwt "draarl/pkg/jwt"
)

const churnMarker = "__draarl_churn_probe__"

type churnAPI struct {
	baseURL string
	token   string
	client  *http.Client
}

type edgeNodeAPIItem struct {
	ID      int                     `json:"id"`
	NodeID  string                  `json:"node_id"`
	Runtime interconnect.NodeStatus `json:"runtime"`
}

type churnDeviceState struct {
	index         int
	originalGroup int64
	currentGroup  int64
	disableSend   bool
	disableRecv   bool
}

type churnCounters struct {
	operations   atomic.Uint64
	groupChanges atomic.Uint64
	commChanges  atomic.Uint64
	configSyncs  atomic.Uint64
	roams        atomic.Uint64
	edgeResets   atomic.Uint64
	textSent     atomic.Uint64
	voiceSent    atomic.Uint64
	writeErrors  atomic.Uint64
}

type edgeSafetySnapshot struct {
	metricsDrops    uint64
	metricsErrors   uint64
	queueDrops      uint64
	staleDrops      uint64
	hardLimitDrops  uint64
	queuedData      int64
	pendingControl  int
	goroutines      int
	receiverHits    uint64
	receiverMisses  uint64
	receiverRebuild uint64
}

func newChurnAPI(baseURL, secret, username string) (*churnAPI, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid API base %q", baseURL)
	}
	if err := appjwt.SetSecret(secret); err != nil {
		return nil, fmt.Errorf("set benchmark JWT secret: %w", err)
	}
	token, err := appjwt.GenerateToken(username, []string{"admin"})
	if err != nil {
		return nil, fmt.Errorf("generate benchmark admin token: %w", err)
	}
	return &churnAPI{baseURL: baseURL, token: token, client: &http.Client{Timeout: 10 * time.Second}}, nil
}

func (a *churnAPI) request(ctx context.Context, method, path string, body any, result any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("%s %s returned HTTP %d with invalid JSON: %w", method, path, resp.StatusCode, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || envelope.Code != http.StatusOK {
		return fmt.Errorf("%s %s failed: HTTP %d code=%d message=%q", method, path, resp.StatusCode, envelope.Code, envelope.Message)
	}
	if result != nil && len(envelope.Data) > 0 && string(envelope.Data) != "null" {
		if err := json.Unmarshal(envelope.Data, result); err != nil {
			return fmt.Errorf("decode %s %s response: %w", method, path, err)
		}
	}
	return nil
}

func (a *churnAPI) changeGroup(ctx context.Context, deviceID int64, groupID int64) error {
	return a.request(ctx, http.MethodPut, "/devices/"+strconv.FormatInt(deviceID, 10)+"/group", map[string]any{"group_id": groupID}, nil)
}

func (a *churnAPI) setCommControl(ctx context.Context, groupID, deviceID int64, disableSend, disableRecv bool) error {
	body := map[string]bool{"disable_send": disableSend, "disable_recv": disableRecv}
	path := fmt.Sprintf("/groups/%d/devices/%d/comm-control", groupID, deviceID)
	return a.request(ctx, http.MethodPut, path, body, nil)
}

func (a *churnAPI) syncConfig(ctx context.Context, deviceID int64, value int) error {
	path := "/admin/devices/" + strconv.FormatInt(deviceID, 10) + "/config"
	if err := a.request(ctx, http.MethodPut, path, map[string]string{"adc_volume": strconv.Itoa(value)}, nil); err != nil {
		return err
	}
	return a.request(ctx, http.MethodPost, path+"/sync", nil, nil)
}

func (a *churnAPI) edgeNodes(ctx context.Context) ([]edgeNodeAPIItem, error) {
	var data struct {
		Items []edgeNodeAPIItem `json:"items"`
	}
	if err := a.request(ctx, http.MethodGet, "/edge-nodes", nil, &data); err != nil {
		return nil, err
	}
	return data.Items, nil
}

func (a *churnAPI) resetEdge(ctx context.Context, node edgeNodeAPIItem) error {
	return a.request(ctx, http.MethodPost, fmt.Sprintf("/edge-nodes/%d/disconnect", node.ID), nil, nil)
}

func runChurnSoak(clients []*benchClient, data *benchData, opts options, jwtSecret string, serverMeters []*processMeter, loadMeter *processMeter) (string, error) {
	if data == nil || len(data.groupIDs) != opts.groups || len(data.deviceIDs) < len(clients) {
		return "", errors.New("benchmark row metadata is incomplete")
	}
	api, err := newChurnAPI(opts.apiBase, jwtSecret, usernameForUser(0))
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startNodes, err := api.edgeNodes(ctx)
	if err != nil {
		return "", fmt.Errorf("read initial edge status: %w", err)
	}
	if len(optsServerAddrs(opts)) > 1 && onlineNodeCount(startNodes) < 2 {
		return "", fmt.Errorf("multi-server churn requires at least two online edge nodes; got %d", onlineNodeCount(startNodes))
	}

	candidateCount := len(clients) - opts.groups
	if candidateCount > 16 {
		candidateCount = 16
	}
	states := make([]*churnDeviceState, candidateCount)
	for i := range states {
		index := opts.groups + i
		groupID := data.groupIDs[index%opts.groups]
		states[i] = &churnDeviceState{index: index, originalGroup: groupID, currentGroup: groupID}
	}
	defer restoreChurnDevices(api, data, states)

	stageID := uint64(time.Now().UnixNano())
	metrics := &stageMetrics{id: stageID}
	activeStage.Store(metrics)
	defer activeStage.Store(nil)
	type3Before := totalReceivedType(clients, protocol.DraARLTypeConfig)
	type4Before := totalReceivedType(clients, protocol.DraARLTypeTextMessage)

	serverBefore := sampleProcesses(serverMeters)
	loadBefore, _ := loadMeter.sample()
	serverPeaks := make([]*atomic.Uint64, len(serverMeters))
	for i := range serverPeaks {
		serverPeaks[i] = &atomic.Uint64{}
		serverPeaks[i].Store(serverBefore[i].rss)
	}
	loadPeak := atomic.Uint64{}
	loadPeak.Store(loadBefore.rss)
	stopSampling := make(chan struct{})
	go sampleMemoryPeaks(stopSampling, serverMeters, loadMeter, serverPeaks, &loadPeak)
	defer close(stopSampling)

	counters := &churnCounters{}
	errCh := make(chan error, 1)
	var workers sync.WaitGroup
	workers.Add(3)
	go churnVoiceLoop(ctx, clients[:opts.groups], opts, stageID, counters, &workers)
	go churnTextLoop(ctx, clients[:opts.groups], opts, counters, &workers)
	go churnOperationLoop(ctx, api, clients, data, states, opts, counters, errCh, &workers)

	start := time.Now()
	deadline := time.NewTimer(opts.duration)
	var runErr error
	select {
	case runErr = <-errCh:
	case <-deadline.C:
	}
	if !deadline.Stop() {
		select {
		case <-deadline.C:
		default:
		}
	}
	cancel()
	workers.Wait()
	// Edge counters are reported every five seconds. Wait through one complete
	// heartbeat before taking the final safety snapshot.
	time.Sleep(6 * time.Second)
	elapsed := time.Since(start)

	endNodes, statusErr := api.edgeNodes(context.Background())
	if runErr == nil && statusErr != nil {
		runErr = fmt.Errorf("read final edge status: %w", statusErr)
	}
	if runErr == nil {
		runErr = validateChurnOutcome(clients, metrics, type3Before, type4Before, startNodes, endNodes, counters, opts)
	}

	serverAfter := sampleProcesses(serverMeters)
	loadAfter, _ := loadMeter.sample()
	serverResults := make([]processResult, len(serverMeters))
	totalServer := processResult{}
	for i := range serverMeters {
		serverResults[i] = processDelta(serverBefore[i], serverAfter[i], serverPeaks[i].Load(), elapsed)
		totalServer.corePercent += serverResults[i].corePercent
		totalServer.machinePct += serverResults[i].machinePct
		totalServer.rssMB += serverResults[i].rssMB
		totalServer.peakRSSMB += serverResults[i].peakRSSMB
	}
	loadResult := processDelta(loadBefore, loadAfter, loadPeak.Load(), elapsed)
	startSafety := summarizeEdgeSafety(startNodes)
	endSafety := summarizeEdgeSafety(endNodes)
	result := fmt.Sprintf("CHURN_RESULT clients=%d groups=%d servers=%d duration=%s operations=%d group_changes=%d comm_changes=%d config_syncs=%d roams=%d edge_resets=%d voice_sent=%d voice_received=%d text_sent=%d text_received=%d config_received=%d write_errors=%d edge_metric_drops_delta=%d edge_metric_errors_delta=%d queue_drops_delta=%d stale_drops_delta=%d hard_limit_drops_delta=%d queued_data_end=%d pending_control_end=%d edge_goroutines_start=%d edge_goroutines_end=%d receiver_hits_delta=%d receiver_misses_delta=%d receiver_rebuilds_delta=%d server_cpu_cores=%.2f server_rss_mb=%.1f server_peak_rss_mb=%.1f loadgen_cpu_cores=%.2f loadgen_rss_mb=%.1f wall=%s",
		len(clients), opts.groups, countDistinctServers(clients), opts.duration.Round(time.Second), counters.operations.Load(), counters.groupChanges.Load(), counters.commChanges.Load(), counters.configSyncs.Load(), counters.roams.Load(), counters.edgeResets.Load(), counters.voiceSent.Load(), metrics.received.Load(), counters.textSent.Load(), totalReceivedType(clients, protocol.DraARLTypeTextMessage)-type4Before, totalReceivedType(clients, protocol.DraARLTypeConfig)-type3Before, counters.writeErrors.Load(), subtractFloor(endSafety.metricsDrops, startSafety.metricsDrops), subtractFloor(endSafety.metricsErrors, startSafety.metricsErrors), subtractFloor(endSafety.queueDrops, startSafety.queueDrops), subtractFloor(endSafety.staleDrops, startSafety.staleDrops), subtractFloor(endSafety.hardLimitDrops, startSafety.hardLimitDrops), endSafety.queuedData, endSafety.pendingControl, startSafety.goroutines, endSafety.goroutines, subtractFloor(endSafety.receiverHits, startSafety.receiverHits), subtractFloor(endSafety.receiverMisses, startSafety.receiverMisses), subtractFloor(endSafety.receiverRebuild, startSafety.receiverRebuild), totalServer.corePercent/100, totalServer.rssMB, totalServer.peakRSSMB, loadResult.corePercent/100, loadResult.rssMB, elapsed.Round(time.Millisecond))
	for i, meter := range serverMeters {
		result += fmt.Sprintf(" proc_%s_cpu_cores=%.2f proc_%s_rss_mb=%.1f proc_%s_peak_rss_mb=%.1f", meter.name, serverResults[i].corePercent/100, meter.name, serverResults[i].rssMB, meter.name, serverResults[i].peakRSSMB)
	}
	return result, runErr
}

func churnVoiceLoop(ctx context.Context, sources []*benchClient, opts options, stageID uint64, counters *churnCounters, wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(opts.interval)
	defer ticker.Stop()
	var sequence uint64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, source := range sources {
				packet := buildVoicePacket(source.identity, opts.payload, stageID, sequence)
				sequence++
				if _, err := source.conn.WriteToUDP(packet, source.serverAddr()); err != nil {
					counters.writeErrors.Add(1)
				} else {
					counters.voiceSent.Add(1)
				}
			}
		}
	}
}

func churnTextLoop(ctx context.Context, sources []*benchClient, opts options, counters *churnCounters, wg *sync.WaitGroup) {
	defer wg.Done()
	every := 10 * time.Second
	if candidate := 3 * opts.churnEvery; candidate > every {
		every = candidate
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	var sequence uint64
	send := func() {
		for _, source := range sources {
			payload := []byte(fmt.Sprintf("%s:%d:%d", churnMarker, time.Now().UnixMilli(), sequence))
			sequence++
			packet := protocol.EncodeDraARLv1(source.identity.username, benchPassword, source.identity.ssid, protocol.DraARLTypeTextMessage, protocol.DraARLDevModelESP32NoRadio, source.identity.dmrid, "", payload)
			if _, err := source.conn.WriteToUDP(packet, source.serverAddr()); err != nil {
				counters.writeErrors.Add(1)
			} else {
				counters.textSent.Add(1)
			}
		}
	}
	send()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			send()
		}
	}
}

func churnOperationLoop(ctx context.Context, api *churnAPI, clients []*benchClient, data *benchData, states []*churnDeviceState, opts options, counters *churnCounters, errCh chan<- error, wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(opts.churnEvery)
	defer ticker.Stop()
	var resetTicker *time.Ticker
	var resetC <-chan time.Time
	if opts.edgeResetEvery > 0 {
		resetTicker = time.NewTicker(opts.edgeResetEvery)
		resetC = resetTicker.C
		defer resetTicker.Stop()
	}
	servers := optsServerAddrs(opts)
	opIndex := 0
	resetIndex := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			state := states[(opIndex/7)%len(states)]
			if err := runChurnOperation(ctx, api, clients, data, state, servers, opIndex, counters); err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
			opIndex++
		case <-resetC:
			if err := resetOneEdge(ctx, api, resetIndex); err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
			resetIndex++
			counters.edgeResets.Add(1)
			counters.operations.Add(1)
		}
	}
}

func runChurnOperation(ctx context.Context, api *churnAPI, clients []*benchClient, data *benchData, state *churnDeviceState, servers []*net.UDPAddr, operation int, counters *churnCounters) error {
	deviceID := data.deviceIDs[state.index]
	var err error
	switch operation % 7 {
	case 0:
		target := data.groupIDs[(indexOfGroup(data.groupIDs, state.currentGroup)+1)%len(data.groupIDs)]
		err = api.changeGroup(ctx, deviceID, target)
		if err == nil {
			state.currentGroup = target
			counters.groupChanges.Add(1)
		}
	case 1:
		state.disableRecv = true
		err = api.setCommControl(ctx, state.currentGroup, deviceID, state.disableSend, state.disableRecv)
		if err == nil {
			counters.commChanges.Add(1)
		}
	case 2:
		state.disableRecv = false
		err = api.setCommControl(ctx, state.currentGroup, deviceID, state.disableSend, state.disableRecv)
		if err == nil {
			counters.commChanges.Add(1)
		}
	case 3:
		state.disableSend = true
		err = api.setCommControl(ctx, state.currentGroup, deviceID, state.disableSend, state.disableRecv)
		if err == nil {
			counters.commChanges.Add(1)
		}
	case 4:
		state.disableSend = false
		err = api.setCommControl(ctx, state.currentGroup, deviceID, state.disableSend, state.disableRecv)
		if err == nil {
			counters.commChanges.Add(1)
		}
	case 5:
		before := clients[state.index].receivedType(protocol.DraARLTypeConfig)
		err = api.syncConfig(ctx, deviceID, 70+operation%10)
		if err == nil {
			err = waitForCounter(ctx, func() uint64 { return clients[state.index].receivedType(protocol.DraARLTypeConfig) }, before, 5*time.Second)
		}
		if err == nil {
			counters.configSyncs.Add(1)
		}
	case 6:
		if len(servers) > 1 {
			current := clients[state.index].serverAddr()
			targetIndex := 0
			for i, server := range servers {
				if current != nil && server.String() == current.String() {
					targetIndex = (i + 1) % len(servers)
					break
				}
			}
			err = clients[state.index].authenticateAt(servers[targetIndex], 8*time.Second)
			if err == nil {
				counters.roams.Add(1)
			}
		}
	}
	if err != nil {
		return fmt.Errorf("churn operation %d device=%d: %w", operation%7, deviceID, err)
	}
	counters.operations.Add(1)
	return nil
}

func resetOneEdge(ctx context.Context, api *churnAPI, offset int) error {
	nodes, err := api.edgeNodes(ctx)
	if err != nil {
		return fmt.Errorf("list edges before reset: %w", err)
	}
	online := make([]edgeNodeAPIItem, 0, len(nodes))
	for _, node := range nodes {
		if node.Runtime.Online {
			online = append(online, node)
		}
	}
	if len(online) == 0 {
		return errors.New("no online edge is available for reconnect test")
	}
	node := online[offset%len(online)]
	connectedAt := node.Runtime.ConnectedAt
	if err := api.resetEdge(ctx, node); err != nil {
		return fmt.Errorf("reset edge %s: %w", node.NodeID, err)
	}
	deadline := time.NewTimer(30 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("edge %s did not reconnect within 30s", node.NodeID)
		case <-ticker.C:
			current, listErr := api.edgeNodes(ctx)
			if listErr != nil {
				continue
			}
			for _, item := range current {
				if item.ID != node.ID || !item.Runtime.Online || item.Runtime.ConnectedAt == nil {
					continue
				}
				if connectedAt == nil || item.Runtime.ConnectedAt.After(*connectedAt) {
					return nil
				}
			}
		}
	}
}

func validateChurnOutcome(clients []*benchClient, metrics *stageMetrics, type3Before, type4Before uint64, startNodes, endNodes []edgeNodeAPIItem, counters *churnCounters, opts options) error {
	var failures []error
	if counters.operations.Load() == 0 || counters.groupChanges.Load() == 0 || counters.commChanges.Load() == 0 || counters.configSyncs.Load() == 0 {
		failures = append(failures, fmt.Errorf("required churn coverage missing: operations=%d groups=%d comm=%d config=%d", counters.operations.Load(), counters.groupChanges.Load(), counters.commChanges.Load(), counters.configSyncs.Load()))
	}
	if counters.roams.Load() == 0 {
		failures = append(failures, errors.New("no device completed an edge roam"))
	}
	if opts.edgeResetEvery > 0 && counters.edgeResets.Load() == 0 {
		failures = append(failures, errors.New("no edge control connection completed a reset and reconnect"))
	}
	if metrics.received.Load() == 0 || counters.voiceSent.Load() == 0 {
		failures = append(failures, errors.New("Type 5 traffic was not delivered during churn"))
	}
	if counters.textSent.Load() == 0 || totalReceivedType(clients, protocol.DraARLTypeTextMessage) <= type4Before {
		failures = append(failures, errors.New("Type 4 traffic was not delivered during churn"))
	}
	if totalReceivedType(clients, protocol.DraARLTypeConfig) <= type3Before {
		failures = append(failures, errors.New("Type 3 configuration was not delivered during churn"))
	}
	if counters.writeErrors.Load() > 0 {
		failures = append(failures, fmt.Errorf("UDP writes failed %d times", counters.writeErrors.Load()))
	}
	start := summarizeEdgeSafety(startNodes)
	end := summarizeEdgeSafety(endNodes)
	if end.queuedData != 0 || end.pendingControl != 0 {
		failures = append(failures, fmt.Errorf("edge queues did not drain: queued_data=%d pending_control=%d", end.queuedData, end.pendingControl))
	}
	if subtractFloor(end.queueDrops, start.queueDrops) > 0 || subtractFloor(end.staleDrops, start.staleDrops) > 0 || subtractFloor(end.hardLimitDrops, start.hardLimitDrops) > 0 {
		failures = append(failures, fmt.Errorf("edge protection dropped queued data: queue=%d stale=%d hard=%d", subtractFloor(end.queueDrops, start.queueDrops), subtractFloor(end.staleDrops, start.staleDrops), subtractFloor(end.hardLimitDrops, start.hardLimitDrops)))
	}
	for _, node := range endNodes {
		if !node.Runtime.Online || node.Runtime.SyncError != "" {
			failures = append(failures, fmt.Errorf("edge %s unhealthy at completion: online=%t sync_error=%q", node.NodeID, node.Runtime.Online, node.Runtime.SyncError))
		}
	}
	return errors.Join(failures...)
}

func restoreChurnDevices(api *churnAPI, data *benchData, states []*churnDeviceState) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, state := range states {
		deviceID := data.deviceIDs[state.index]
		if state.currentGroup != state.originalGroup {
			_ = api.changeGroup(ctx, deviceID, state.originalGroup)
			state.currentGroup = state.originalGroup
		}
		if state.disableSend || state.disableRecv {
			_ = api.setCommControl(ctx, state.currentGroup, deviceID, false, false)
			state.disableSend, state.disableRecv = false, false
		}
	}
}

func optsServerAddrs(opts options) []*net.UDPAddr {
	servers, _ := resolveServerAddrs(opts.serverAddr, opts.serverAddrs)
	return servers
}

func totalReceivedType(clients []*benchClient, packetType byte) uint64 {
	var total uint64
	for _, client := range clients {
		if client != nil {
			total += client.receivedType(packetType)
		}
	}
	return total
}

func waitForCounter(ctx context.Context, current func() uint64, before uint64, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		if current() > before {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("expected packet was not received before timeout")
		case <-ticker.C:
		}
	}
}

func onlineNodeCount(nodes []edgeNodeAPIItem) int {
	count := 0
	for _, node := range nodes {
		if node.Runtime.Online {
			count++
		}
	}
	return count
}

func summarizeEdgeSafety(nodes []edgeNodeAPIItem) edgeSafetySnapshot {
	var result edgeSafetySnapshot
	for _, node := range nodes {
		hb := node.Runtime.Heartbeat
		result.metricsDrops += hb.Device.Drops + hb.Interconnect.Drops + node.Runtime.CenterData.Drops
		result.metricsErrors += hb.Device.Errors + hb.Interconnect.Errors + node.Runtime.CenterData.Errors
		result.queueDrops += hb.Protection.DataQueueDrops + node.Runtime.CenterProtection.DataQueueDrops
		result.staleDrops += hb.Protection.DataStaleDrops + node.Runtime.CenterProtection.DataStaleDrops
		result.hardLimitDrops += hb.Protection.DataHardLimitDrops + node.Runtime.CenterProtection.DataHardLimitDrops
		result.queuedData += hb.Protection.QueuedData + node.Runtime.CenterProtection.QueuedData
		result.pendingControl += node.Runtime.PendingControl
		result.goroutines += hb.Goroutines
		result.receiverHits += hb.ReceiverCache.Hits
		result.receiverMisses += hb.ReceiverCache.Misses
		result.receiverRebuild += hb.ReceiverCache.Rebuilds
	}
	return result
}

func subtractFloor(after, before uint64) uint64 {
	if after < before {
		return 0
	}
	return after - before
}

func indexOfGroup(groups []int64, target int64) int {
	for i, group := range groups {
		if group == target {
			return i
		}
	}
	return 0
}
