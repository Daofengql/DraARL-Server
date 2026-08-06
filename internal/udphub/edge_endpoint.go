package udphub

import (
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"draarl/internal/protocol"
)

// EdgeFanoutTarget is the minimal receiver identity required by the existing
// parallel fan-out engine. It contains no database or authentication state.
type EdgeFanoutTarget struct {
	Addr          *net.UDPAddr
	DeviceID      int
	Username      string
	SSID          byte
	SessionID     uint64
	SourceGroupV1 bool
	addrPort      netip.AddrPort
}

func NewEdgeSessionFanoutTarget(addr *net.UDPAddr, sessionID uint64, deviceID int, username string, ssid byte, sourceGroupV1 bool) (EdgeFanoutTarget, bool) {
	target, ok := NewEdgeFanoutTarget(addr, deviceID, username, ssid)
	if !ok || sessionID == 0 {
		return EdgeFanoutTarget{}, false
	}
	target.SessionID = sessionID
	target.SourceGroupV1 = sourceGroupV1
	return target, true
}

func NewEdgeFanoutTarget(addr *net.UDPAddr, deviceID int, username string, ssid byte) (EdgeFanoutTarget, bool) {
	addrPort, ok := udpAddrPort(addr)
	if !ok {
		return EdgeFanoutTarget{}, false
	}
	return EdgeFanoutTarget{DeviceID: deviceID, Username: username, SSID: ssid, addrPort: addrPort}, true
}

type EdgeFanoutResult struct {
	Attempted int64
	Sent      int64
	Dropped   int64
	Errors    int64
}

// EdgeFanoutPlan is an immutable receiver snapshot already partitioned for
// this endpoint's parallel writers. It is safe to reuse across frames until
// InvalidateFanoutPlans is called.
type EdgeFanoutPlan struct {
	endpoint   *EdgeEndpoint
	entries    []domainReceiverEntry
	partitions [][]domainReceiverEntry
	workers    int
	generation uint64
}

func (p *EdgeFanoutPlan) Len() int {
	if p == nil {
		return 0
	}
	return len(p.entries)
}

// EdgeEndpoint is the database-free form of the udphub UDP ingress. One
// socket carries both ordinary device packets and authenticated Type 0 node
// packets; callers distinguish them in handler. It intentionally reuses the
// same single-reader, sharded-worker and parallel fan-out design as the
// centre's udphub.
type EdgeEndpoint struct {
	conn            *net.UDPConn
	handler         func([]byte, *net.UDPAddr, *net.UDPAddr)
	proxyProtocolV2 bool
	queues          []chan udpDatagramJob
	readerWG        sync.WaitGroup
	workerWG        sync.WaitGroup
	closeOnce       sync.Once
	closed          chan struct{}
	sender          *FanoutSender
	planGeneration  atomic.Uint64
}

func NewEdgeEndpoint(listenAddr, proxyProtocol string, handler func([]byte, *net.UDPAddr, *net.UDPAddr)) (*EdgeEndpoint, error) {
	if handler == nil {
		return nil, errors.New("edge UDP handler is required")
	}
	proxyProtocol = strings.ToLower(strings.TrimSpace(proxyProtocol))
	if proxyProtocol != "" && proxyProtocol != "v2" {
		return nil, errors.New("edge UDP proxy protocol must be empty or v2")
	}
	if listenAddr == "" {
		listenAddr = ":60050"
	}
	addr, err := net.ResolveUDPAddr("udp", listenAddr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}
	configureUDPSocketBuffers(conn)

	workers := udpWorkerCount()
	perQueue := udpJobQueueSize / workers
	if perQueue < 64 {
		perQueue = 64
	}
	e := &EdgeEndpoint{conn: conn, handler: handler, proxyProtocolV2: proxyProtocol == "v2", queues: make([]chan udpDatagramJob, workers), closed: make(chan struct{})}
	for i := range e.queues {
		e.queues[i] = make(chan udpDatagramJob, perQueue)
		e.workerWG.Add(1)
		go e.worker(e.queues[i])
	}
	queueSize, maxAge := fanoutRuntimeSettings()
	e.sender = newFanoutSenderWithMaxAge(conn, fanoutWorkerCount(), queueSize, maxAge)
	e.readerWG.Add(1)
	go e.readLoop()
	return e, nil
}

func (e *EdgeEndpoint) Addr() net.Addr {
	if e == nil || e.conn == nil {
		return nil
	}
	return e.conn.LocalAddr()
}

func (e *EdgeEndpoint) readLoop() {
	defer e.readerWG.Done()
	for {
		select {
		case <-e.closed:
			return
		default:
		}
		base := packetPool.Get().([]byte)
		n, addr, err := e.conn.ReadFromUDP(base)
		if err != nil {
			packetPool.Put(base)
			return
		}
		packetData := base[:n]
		realAddr := addr
		if e.proxyProtocolV2 {
			proxyInfo, payload, parsed := ParseProxyProtocolV2(packetData)
			if parsed {
				packetData = payload
				if proxyInfo != nil && proxyInfo.IsProxy {
					realAddr = GetRealAddr(addr, proxyInfo)
				}
			}
		}
		job := udpDatagramJob{data: packetData, baseBuffer: base, remoteAddr: addr, realAddr: realAddr, receivedAt: time.Now()}
		queue := e.queues[udpDatagramShard(job.data, realAddr, len(e.queues))]
		select {
		case queue <- job:
		default:
			packetPool.Put(base)
		}
	}
}

func (e *EdgeEndpoint) worker(queue <-chan udpDatagramJob) {
	defer e.workerWG.Done()
	for job := range queue {
		func() {
			defer func() { _ = recover() }()
			e.handler(job.data, job.remoteAddr, job.realAddr)
		}()
		packetPool.Put(job.baseBuffer)
	}
}

func (e *EdgeEndpoint) SendTo(data []byte, addr *net.UDPAddr) error {
	if e == nil || e.conn == nil || addr == nil {
		return errors.New("edge UDP endpoint is not ready")
	}
	_, err := e.conn.WriteToUDP(data, addr)
	return err
}

func (e *EdgeEndpoint) PrepareFanout(targets []EdgeFanoutTarget) *EdgeFanoutPlan {
	if e == nil || len(targets) == 0 || e.sender == nil || len(e.sender.writers) == 0 {
		return nil
	}
	generation := e.planGeneration.Load()
	entries := make([]domainReceiverEntry, 0, len(targets))
	seen := make(map[netip.AddrPort]struct{}, len(targets))
	for _, target := range targets {
		addr := target.addrPort
		if !addr.IsValid() {
			var ok bool
			addr, ok = udpAddrPort(target.Addr)
			if !ok {
				continue
			}
		}
		if _, exists := seen[addr]; exists {
			continue
		}
		seen[addr] = struct{}{}
		sessionID := ""
		if target.SessionID != 0 {
			sessionID = "edge:" + strconv.FormatUint(target.SessionID, 10)
		}
		entries = append(entries, domainReceiverEntry{addr: addr, deviceID: target.DeviceID, username: target.Username, ssid: target.SSID, sessionID: sessionID, sourceGroupV1: target.SourceGroupV1})
	}
	if len(entries) == 0 {
		return nil
	}
	partitions := make([][]domainReceiverEntry, len(e.sender.writers))
	for i := range entries {
		index := addrPortShard(entries[i].addr, len(partitions))
		partitions[index] = append(partitions[index], entries[i])
	}
	return &EdgeFanoutPlan{endpoint: e, entries: entries, partitions: partitions, workers: len(partitions), generation: generation}
}

func (e *EdgeEndpoint) InvalidateFanoutPlans() {
	if e != nil {
		e.planGeneration.Add(1)
	}
}

func (e *EdgeEndpoint) FanoutPlan(data []byte, plan *EdgeFanoutPlan, sourceID int, sourceUser string, sourceSSID byte, onComplete func(EdgeFanoutResult)) bool {
	return e.fanoutPlan(data, plan, sourceID, sourceUser, sourceSSID, "", 0, onComplete)
}

func (e *EdgeEndpoint) FanoutSessionPlan(data []byte, plan *EdgeFanoutPlan, sourceSessionID uint64, sourceGroupID int, onComplete func(EdgeFanoutResult)) bool {
	identity := ""
	if sourceSessionID != 0 {
		identity = "edge:" + strconv.FormatUint(sourceSessionID, 10)
	}
	return e.fanoutPlan(data, plan, 0, "", 0, identity, sourceGroupID, onComplete)
}

func (e *EdgeEndpoint) fanoutPlan(data []byte, plan *EdgeFanoutPlan, sourceID int, sourceUser string, sourceSSID byte, sourceSessionID string, sourceGroupID int, onComplete func(EdgeFanoutResult)) bool {
	if e == nil || len(data) == 0 || plan == nil || plan.endpoint != e || plan.workers == 0 || plan.workers != len(e.sender.writers) || plan.generation != e.planGeneration.Load() {
		return false
	}
	complete := func(result fanoutWriteResult) {
		if onComplete != nil {
			onComplete(EdgeFanoutResult{Attempted: result.attempted, Sent: result.sent, Dropped: result.dropped, Errors: result.errors})
		}
	}
	var sourceGroupData []byte
	if sourceGroupID > 0 {
		for i := range plan.entries {
			if plan.entries[i].sourceGroupV1 {
				sourceGroupData, _ = protocol.WithSourceGroupID(data, sourceGroupID)
				break
			}
		}
	}
	if e.sender.enqueue(fanoutFrameJob{
		data: append([]byte(nil), data...), sourceGroupData: sourceGroupData, partitions: plan.partitions,
		sourceID: sourceID, sourceUser: sourceUser, sourceSSID: sourceSSID, sourceSessionID: sourceSessionID,
		enqueuedAt: time.Now(), snapshotGen: plan.generation, generation: &e.planGeneration,
		onComplete: complete,
	}) {
		return true
	}
	if plan.generation != e.planGeneration.Load() {
		return false
	}
	result := fanoutWriteResult{}
	for i := range plan.entries {
		target := &plan.entries[i]
		if isSourceTarget(target, sourceID, sourceUser, sourceSSID, sourceSessionID) {
			continue
		}
		result.attempted++
		payload := data
		if target.sourceGroupV1 && len(sourceGroupData) > 0 {
			payload = sourceGroupData
		}
		if _, err := e.conn.WriteToUDPAddrPort(payload, target.addr); err == nil {
			result.sent++
		} else {
			result.errors++
		}
	}
	complete(result)
	return true
}

func (e *EdgeEndpoint) Fanout(data []byte, targets []EdgeFanoutTarget, sourceID int, sourceUser string, sourceSSID byte, onComplete func(EdgeFanoutResult)) bool {
	return e.FanoutPlan(data, e.PrepareFanout(targets), sourceID, sourceUser, sourceSSID, onComplete)
}

func (e *EdgeEndpoint) Close() error {
	if e == nil {
		return nil
	}
	var err error
	e.closeOnce.Do(func() {
		close(e.closed)
		e.InvalidateFanoutPlans()
		if e.conn != nil {
			// Wake the single reader before closing duplicated Windows socket
			// views. A direct Close can wait for an outstanding IOCP read.
			wakeAddr := cloneEndpointAddr(e.conn.LocalAddr())
			if wakeAddr != nil {
				if wake, wakeErr := net.ListenUDP("udp", nil); wakeErr == nil {
					_, _ = wake.WriteToUDP([]byte{0}, wakeAddr)
					_ = wake.Close()
				}
			}
		}
		e.readerWG.Wait()
		for _, queue := range e.queues {
			close(queue)
		}
		e.workerWG.Wait()
		if e.sender != nil {
			e.sender.stop()
		}
		if e.conn != nil {
			err = e.conn.Close()
		}
	})
	return err
}

func cloneEndpointAddr(addr net.Addr) *net.UDPAddr {
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok || udpAddr == nil {
		return nil
	}
	copyAddr := *udpAddr
	copyAddr.IP = append(net.IP(nil), udpAddr.IP...)
	if copyAddr.IP == nil || copyAddr.IP.IsUnspecified() {
		if udpAddr.IP != nil && udpAddr.IP.To4() == nil {
			copyAddr.IP = net.IPv6loopback
		} else {
			copyAddr.IP = net.IPv4(127, 0, 0, 1)
		}
	}
	return &copyAddr
}
